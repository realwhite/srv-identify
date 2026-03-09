// Copyright \d{4} VK Cloud.
//
// All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/bougou/go-ipmi"
)

const (
	IPMICollectorType CollectorType = "ipmi"
	bitInByte         uint16        = 8
)

type IPMIResult struct {
	Status CollectStatus
	Data   IPMIData
}
type IPMIData struct {
	DeviceID       uint8
	ManufacturerID uint32
	ProductID      uint32

	// Из FRU (Board Info Area)
	FRUBoardMfg     string
	FRUBoardProduct string
	FRUBoardSerial  string
	FRUBoardPN      string
	FRUBoardMfgDate string

	// Из FRU (Product Info Area)
	FRUProductMfg    string
	FRUProductName   string
	FRUProductPN     string
	FRUProductSerial string
	FRUProductAsset  string
	FRUProductVer    string

	// Из FRU (Chassis Info Area)
	FRUChassisType   string
	FRUChassisPN     string
	FRUChassisSerial string

	// Lan config
	Mac net.HardwareAddr
}

type IPMICollector struct {
	l *slog.Logger
}

func NewIPMICollector(l *slog.Logger) *IPMICollector {
	return &IPMICollector{
		l: l,
	}
}

func (c *IPMICollector) Type() CollectorType {
	return IPMICollectorType
}

func (c *IPMICollector) CollectRemote(ctx context.Context, result *UnionCollectResult, ip net.IP, opts *IdentifyOptions) error {
	result.IPMI = &IPMIResult{
		Status: CollectStatusError,
	}

	client, err := ipmi.NewClient(ip.String(), opts.IPMIConfig.Port, opts.Credentials.Username, opts.Credentials.Password)
	if err != nil {
		return fmt.Errorf("failed to create ipmi client: %w", err)
	}

	client.WithInterface(ipmi.InterfaceLanplus)

	err = client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to ipmi: %w", err)
	}

	return c.collect(ctx, client, result)
}

func (c *IPMICollector) CollectLocal(ctx context.Context, result *UnionCollectResult, opts *IdentifyOptions) error {
	client, err := ipmi.NewOpenClient()
	if err != nil {
		return fmt.Errorf("failed to create ipmi client: %w", err)
	}

	client.WithInterface(ipmi.InterfaceLanplus)
	err = client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to ipmi: %w", err)
	}

	return c.collect(ctx, client, result)
}

func (c *IPMICollector) collect(ctx context.Context, client *ipmi.Client, result *UnionCollectResult) error {
	collectStatus := CollectStatusSuccess

	collectors := []func(context.Context, *ipmi.Client, *UnionCollectResult) error{
		c.collectDeviceID,
		c.collectFRU,
		c.collectMAC,
	}

	successCount, failedCount := 0, 0

	for _, collector := range collectors {
		if err := collector(ctx, client, result); err != nil {
			c.l.Error("Failed to collect data", "err", err)
			failedCount++
		} else {
			successCount++
		}
	}

	if failedCount > 0 {
		if successCount > 0 {
			collectStatus = CollectStatusPartialError
		} else {
			collectStatus = CollectStatusError
		}
	}

	result.IPMI.Status = collectStatus

	return nil
}

func (c *IPMICollector) collectDeviceID(ctx context.Context, client *ipmi.Client, result *UnionCollectResult) error {
	res, err := client.GetDeviceID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get device ID: %w", err)
	}

	result.IPMI.Data.DeviceID = res.DeviceID
	result.IPMI.Data.ManufacturerID = res.ManufacturerID

	return nil
}

func (c *IPMICollector) collectFRU(ctx context.Context, client *ipmi.Client, result *UnionCollectResult) error {
	resFru, err := c.getFRUSafe(ctx, client, 0)
	if err != nil {
		return fmt.Errorf("failed to read FRUSafe: %w", err)
	}

	if resFru.BoardInfoArea != nil {
		result.IPMI.Data.FRUBoardMfg = normalizeString(string(resFru.BoardInfoArea.Manufacturer))
		result.IPMI.Data.FRUBoardProduct = normalizeString(string(resFru.BoardInfoArea.ProductName))
		result.IPMI.Data.FRUBoardSerial = normalizeString(string(resFru.BoardInfoArea.SerialNumber))
		result.IPMI.Data.FRUBoardMfgDate = resFru.BoardInfoArea.MfgDateTime.Format(time.RFC3339)
		result.IPMI.Data.FRUBoardPN = normalizeString(string(resFru.BoardInfoArea.PartNumber))
	}

	if resFru.ProductInfoArea != nil {
		result.IPMI.Data.FRUProductMfg = normalizeString(string(resFru.ProductInfoArea.Manufacturer))
		result.IPMI.Data.FRUProductName = normalizeString(string(resFru.ProductInfoArea.Name))
		result.IPMI.Data.FRUProductPN = normalizeString(string(resFru.ProductInfoArea.PartModel))
		result.IPMI.Data.FRUProductSerial = normalizeString(string(resFru.ProductInfoArea.SerialNumber))
		result.IPMI.Data.FRUProductAsset = normalizeString(string(resFru.ProductInfoArea.AssetTag))
		result.IPMI.Data.FRUProductVer = normalizeString(string(resFru.ProductInfoArea.Version))
	}

	if resFru.ChassisInfoArea != nil {
		result.IPMI.Data.FRUChassisType = normalizeString(resFru.ChassisInfoArea.ChassisType.String())
		result.IPMI.Data.FRUChassisPN = normalizeString(string(resFru.ChassisInfoArea.PartNumber))
		result.IPMI.Data.FRUChassisSerial = normalizeString(string(resFru.ChassisInfoArea.SerialNumber))
	}

	return nil
}

// это копия ipmi.GetFRU, но без обработки multirecords area
func (c *IPMICollector) getFRUSafe(ctx context.Context, client *ipmi.Client, deviceID uint8) (*ipmi.FRU, error) {
	fru := &ipmi.FRU{}

	fruAreaInfoRes, err := client.GetFRUInventoryAreaInfo(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("GetFRUInventoryAreaInfo failed, err: %w", err)
	}

	if fruAreaInfoRes.AreaSizeBytes < 1 {
		return nil, fmt.Errorf("invalid FRU size %d", fruAreaInfoRes.AreaSizeBytes)
	}

	// retrieve the FRU header, just fetch FRUCommonHeaderSize bytes to construct a FRU Header
	readFRURes, err := client.ReadFRUData(ctx, 0, 0, ipmi.FRUCommonHeaderSize)
	if err != nil {
		return nil, fmt.Errorf("ReadFRUData failed, err: %w", err)
	}

	fruHeader := &ipmi.FRUCommonHeader{}
	if err := fruHeader.Unpack(readFRURes.Data); err != nil {
		return nil, fmt.Errorf("unpack fru header data failed, err: %w", err)
	}
	if fruHeader.FormatVersion != ipmi.FRUFormatVersion {
		return nil, fmt.Errorf("unknown FRU header version %#02x", fruHeader.FormatVersion)
	}
	fru.CommonHeader = fruHeader

	if offset := uint16(fruHeader.ChassisOffset8B) * bitInByte; offset > 0 && offset < fruAreaInfoRes.AreaSizeBytes {
		fruChassis, err := client.GetFRUAreaChassis(ctx, deviceID, offset)
		if err != nil {
			return nil, fmt.Errorf("GetFRUAreaChassis failed, err: %w", err)
		}

		fru.ChassisInfoArea = fruChassis
	}

	if offset := uint16(fruHeader.BoardOffset8B) * bitInByte; offset > 0 && offset < fruAreaInfoRes.AreaSizeBytes {
		fruBoard, err := client.GetFRUAreaBoard(ctx, deviceID, offset)
		if err != nil {
			return nil, fmt.Errorf("GetFRUAreaBoard failed, err: %w", err)
		}
		fru.BoardInfoArea = fruBoard
	}

	if offset := uint16(fruHeader.ProductOffset8B) * bitInByte; offset > 0 && offset < fruAreaInfoRes.AreaSizeBytes {
		fruProduct, err := client.GetFRUAreaProduct(ctx, deviceID, offset)
		if err != nil {
			return nil, fmt.Errorf("GetFRUAreaProduct failed, err: %w", err)
		}
		fru.ProductInfoArea = fruProduct
	}

	return fru, nil
}

func (c *IPMICollector) collectMAC(ctx context.Context, client *ipmi.Client, result *UnionCollectResult) error {
	resp, err := client.GetLanConfigParams(ctx, 1)
	if err != nil {
		return fmt.Errorf("GetLanConfigParams failed, err: %w", err)
	}

	if resp == nil {
		return fmt.Errorf("LanConfigResponse is nil")
	}

	if resp.MAC == nil {
		return fmt.Errorf("MAC is nil")
	}

	result.IPMI.Data.Mac = resp.MAC.MAC

	return nil
}
