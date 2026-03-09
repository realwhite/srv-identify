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
	"encoding/hex"
	"errors"
	_ "fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
)

var errCollectNotImplemented = errors.New("collect not implemented")

type CollectStatus string

const (
	CollectStatusSuccess      CollectStatus = "success"
	CollectStatusError        CollectStatus = "error"
	CollectStatusPartialError CollectStatus = "partial_error"
)

type CollectorType string

var (
	DefaultCollectorsRemote = []CollectorType{IPMICollectorType, RedfishCollectorType}
	DefaultCollectorsLocal  = []CollectorType{IPMICollectorType}
)

type UnionCollectResult struct {
	IPMI    *IPMIResult
	Redfish *RedfishResult
}

type ResultVendor struct {
	Name string
	Raw  []string

	// https://www.iana.org/assignments/enterprise-numbers.txt
	IANAEnterpriseID uint32

	ShortConstant string

	MAC net.HardwareAddr

	// https://standards-oui.ieee.org/oui/oui.csv
	OUI string
}

type ResultModel struct {
	Name         string
	SerialNumber string
}

type IdentifyResult struct {
	Vendor ResultVendor
	Model  ResultModel
}

// Options
type ServerCredentials struct {
	Username string
	Password string
}

type RedfishConfig struct {
	Port     int
	Prefix   string
	Insecure bool
}

type IPMIConfig struct {
	Port int
}

type IdentifyOptions struct {
	Credentials ServerCredentials

	RedfishConfig RedfishConfig
	IPMIConfig    IPMIConfig

	Collectors []CollectorType
}

type Collector interface {
	Type() CollectorType
	CollectRemote(context.Context, *UnionCollectResult, net.IP, *IdentifyOptions) error
	CollectLocal(context.Context, *UnionCollectResult, *IdentifyOptions) error
}

type ServerIdentifier struct {
	l *slog.Logger
}

func NewServerIdentifier(l *slog.Logger) *ServerIdentifier {
	return &ServerIdentifier{
		l: l,
	}
}

func (id *ServerIdentifier) IdentifyRemote(ctx context.Context, ip net.IP, opts IdentifyOptions) (*IdentifyResult, error) {
	collectResult := &UnionCollectResult{}

	ipmiCollector := NewIPMICollector(id.l)
	redfishCollector := NewRedfishCollector(id.l)

	targetCollectorTypes := DefaultCollectorsRemote
	if len(opts.Collectors) > 0 {
		targetCollectorTypes = opts.Collectors
	}

	for _, collector := range []Collector{ipmiCollector, redfishCollector} {
		if !slices.Contains(targetCollectorTypes, collector.Type()) {
			id.l.Debug("collector skipped", "collector", collector.Type())

			continue
		}

		err := collector.CollectRemote(ctx, collectResult, ip, &opts)
		if err != nil {
			id.l.Error("Failed to collect data", "error", err, "collector", collector.Type())
		}
	}

	return id.processResult(ctx, collectResult)
}

func (id *ServerIdentifier) IdentifyLocal(ctx context.Context, opts IdentifyOptions) (*IdentifyResult, error) {
	collectResult := &UnionCollectResult{}

	ipmiCollector := NewIPMICollector(id.l)
	redfishCollector := NewRedfishCollector(id.l)

	targetCollectorTypes := DefaultCollectorsLocal
	if len(opts.Collectors) > 0 {
		targetCollectorTypes = opts.Collectors
	}

	for _, collector := range []Collector{ipmiCollector, redfishCollector} {
		if !slices.Contains(targetCollectorTypes, collector.Type()) {
			id.l.Debug("collector skipped", "collector", collector.Type())

			continue
		}

		err := collector.CollectLocal(ctx, collectResult, &opts)
		if err != nil {
			id.l.Error("Failed to collect data", "error", err, "collector", collector.Type())
		}
	}

	return id.processResult(ctx, collectResult)
}

func (id *ServerIdentifier) makeShortConstant(vendorName string) string {
	return strings.SplitN(strings.ToUpper(vendorName), " ", 2)[0]
}

func (id *ServerIdentifier) mergeVendorSection(collectResult *UnionCollectResult, identifyResult *IdentifyResult) {
	identifyResult.Vendor = ResultVendor{}
	vendorCandidates := make(map[string]struct{})

	if collectResult.IPMI != nil && collectResult.IPMI.Status != CollectStatusError {
		vendorCandidates[collectResult.IPMI.Data.FRUBoardMfg] = struct{}{}
		vendorCandidates[collectResult.IPMI.Data.FRUProductMfg] = struct{}{}

		identifyResult.Vendor.IANAEnterpriseID = collectResult.IPMI.Data.ManufacturerID
		vendorByIANA, ok := LookupIANANumber(collectResult.IPMI.Data.ManufacturerID)
		if ok {
			vendorCandidates[vendorByIANA] = struct{}{}
		}

		identifyResult.Vendor.MAC = collectResult.IPMI.Data.Mac
		vendorByMac, ok := LookupOUI(collectResult.IPMI.Data.Mac)
		if ok {
			vendorCandidates[vendorByMac] = struct{}{}
			identifyResult.Vendor.OUI = vendorByMac
		}
	}

	if collectResult.Redfish != nil && collectResult.Redfish.Status != CollectStatusError {
		vendorCandidates[collectResult.Redfish.Data.Manufacturer] = struct{}{}
	}

	var vendorName string
	for name := range vendorCandidates {
		if strings.EqualFold(name, "null") || strings.EqualFold(name, "") || strings.EqualFold(name, "unknown") {
			continue
		}

		vendorName = name

		break
	}

	identifyResult.Vendor.Name = vendorName
	identifyResult.Vendor.ShortConstant = id.makeShortConstant(vendorName)

	var rawNames []string

	for name := range vendorCandidates {
		rawNames = append(rawNames, name)
	}

	identifyResult.Vendor.Raw = rawNames
}

func (id *ServerIdentifier) mergeModelSection(collectResult *UnionCollectResult, identifyResult *IdentifyResult) {
	identifyResult.Model = ResultModel{}
	var modelCandidates []string
	var serialCandidates []string

	if collectResult.IPMI != nil && collectResult.IPMI.Status != CollectStatusError {
		modelCandidates = append(modelCandidates, collectResult.IPMI.Data.FRUProductName, collectResult.IPMI.Data.FRUProductMfg)
		serialCandidates = append(serialCandidates, collectResult.IPMI.Data.FRUProductSerial)
	}

	if collectResult.Redfish != nil && collectResult.Redfish.Status != CollectStatusError {
		modelCandidates = append(modelCandidates, collectResult.Redfish.Data.Model)
		serialCandidates = append(serialCandidates, collectResult.Redfish.Data.SerialNumber)
	}

	for _, name := range modelCandidates {
		if strings.EqualFold(name, "null") || strings.EqualFold(name, "") || strings.EqualFold(name, "unknown") {
			continue
		}
		identifyResult.Model.Name = name

		break
	}

	for _, serial := range serialCandidates {
		if strings.EqualFold(serial, "null") || strings.EqualFold(serial, "") || strings.EqualFold(serial, "unknown") {
			continue
		}

		identifyResult.Model.SerialNumber = serial
	}
}

func (id *ServerIdentifier) processResult(ctx context.Context, collectResult *UnionCollectResult) (*IdentifyResult, error) {
	identifyResult := &IdentifyResult{}

	// Vendor section
	id.mergeVendorSection(collectResult, identifyResult)

	// Model section
	id.mergeModelSection(collectResult, identifyResult)

	return identifyResult, nil
}

func LookupOUI(mac net.HardwareAddr) (string, bool) {
	if len(mac) < 3 {
		return "", false
	}

	key := hex.EncodeToString(mac[:3])
	key = strings.ToUpper(key)

	v, ok := OUIDB[key]

	return v, ok
}

func LookupIANANumber(vendorId uint32) (string, bool) {
	v, ok := IANADB[vendorId]

	return v, ok
}
