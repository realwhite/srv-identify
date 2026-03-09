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
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"

	"github.com/olekukonko/errors"
)

const RedfishCollectorType CollectorType = "redfish"

type RedfishResult struct {
	Status CollectStatus
	Data   RedfishData
}

type RedfishData struct {
	Version string

	Manufacturer string
	Model        string
	SerialNumber string
	PartNumber   string
}

type systemsCollection struct {
	Members json.RawMessage `json:"Members"`
}

type odataID struct {
	ID string `json:"@odata.id"`
}

type minimalRootResponse struct {
	RedfishVersion string `json:"RedfishVersion"`
}

type minimalSystemResponse struct {
	Manufacturer string `json:"Manufacturer"`
	Model        string `json:"Model"`
	SerialNumber string `json:"SerialNumber"`
	PartNumber   string `json:"PartNumber"`
}

type AuthTransport struct {
	Base       http.RoundTripper
	AuthHeader string
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", t.AuthHeader)

	return t.Base.RoundTrip(clone) //nolint: wrapcheck
}

func buildUniversalTransport(insecure bool) *http.Transport {
	allCiphers := make([]uint16, 0)
	for _, c := range tls.CipherSuites() {
		allCiphers = append(allCiphers, c.ID)
	}
	for _, c := range tls.InsecureCipherSuites() {
		allCiphers = append(allCiphers, c.ID)
	}

	return &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecure, //nolint: gosec
			MinVersion:         tls.VersionTLS10,
			CipherSuites:       allCiphers,
		},
		ForceAttemptHTTP2: false,
	}
}

func basicAuth(username, password string) string {
	encoded := base64.StdEncoding.EncodeToString(
		[]byte(username + ":" + password),
	)

	return "Basic " + encoded
}

func DefaultHTTPClient(username, password string, insecure bool) *http.Client {
	baseTransport := buildUniversalTransport(insecure)

	return &http.Client{
		Transport: &AuthTransport{
			Base:       baseTransport,
			AuthHeader: basicAuth(username, password),
		},
	}
}

type RedfishCollector struct {
	l *slog.Logger
}

func NewRedfishCollector(l *slog.Logger) *RedfishCollector {
	return &RedfishCollector{
		l: l,
	}
}

func (c *RedfishCollector) Type() CollectorType {
	return RedfishCollectorType
}

func (c *RedfishCollector) CollectRemote(ctx context.Context, result *UnionCollectResult, ip net.IP, opts *IdentifyOptions) error {
	result.Redfish = &RedfishResult{
		Status: CollectStatusError,
	}
	httpClient := DefaultHTTPClient(opts.Credentials.Username, opts.Credentials.Password, opts.RedfishConfig.Insecure)

	host := ip.String()
	if opts.RedfishConfig.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, opts.RedfishConfig.Port)
	}

	urlPath := fmt.Sprintf("%s/redfish/v1/", opts.RedfishConfig.Prefix)

	baseUrl := url.URL{
		Scheme: "https",
		Host:   host,
		Path:   urlPath,
	}

	redfishVersion, err := c.getRedfishVersion(ctx, httpClient, baseUrl.String())
	if err != nil {
		return fmt.Errorf("failed to check redfish availability: %w", err)
	}

	result.Redfish.Data.Version = normalizeString(redfishVersion)
	result.Redfish.Status = CollectStatusPartialError

	systemId, err := c.getSystemId(ctx, httpClient, baseUrl.String())
	if err != nil {
		return fmt.Errorf("failed to get system id: %w", err)
	}

	err = c.collectSystemData(ctx, httpClient, baseUrl.String(), systemId, result)
	if err != nil {
		return fmt.Errorf("failed to collect system data: %w", err)
	}
	result.Redfish.Status = CollectStatusSuccess

	return nil
}

func (c *RedfishCollector) CollectLocal(ctx context.Context, result *UnionCollectResult, opts *IdentifyOptions) error {
	return errCollectNotImplemented
}

func (c *RedfishCollector) getRedfishVersion(ctx context.Context, client *http.Client, baseUrl string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseUrl, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to execute request, status code: %d, body: %s", resp.StatusCode, body)
	}

	jsonResp := minimalRootResponse{}
	err = json.Unmarshal(body, &jsonResp)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return jsonResp.RedfishVersion, nil
}

func (c *RedfishCollector) getSystemId(ctx context.Context, client *http.Client, baseUrl string) (string, error) {
	systemsUrl, err := url.JoinPath(baseUrl, "Systems")
	if err != nil {
		return "", fmt.Errorf("failed to create url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, systemsUrl, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to execute request, status code: %d, body: %s", resp.StatusCode, body)
	}

	return c.safeParseSystemId(body)
}

func (c *RedfishCollector) safeParseSystemId(data []byte) (string, error) {
	var collection systemsCollection
	if err := json.Unmarshal(data, &collection); err != nil {
		return "", fmt.Errorf("unmarshal collection: %w", err)
	}

	if len(collection.Members) == 0 {
		return "", errors.New("Members field is missing")
	}

	var odataPath string

	switch collection.Members[0] {
	case '[':
		// Стандартный вариант: Members — массив
		var members []odataID
		if err := json.Unmarshal(collection.Members, &members); err != nil {
			return "", fmt.Errorf("unmarshal Members array: %w", err)
		}
		if len(members) == 0 {
			return "", errors.New("Members array is empty")
		}
		odataPath = members[0].ID

	case '{':
		// Нестандартный вариант: Members — объект
		var member odataID
		if err := json.Unmarshal(collection.Members, &member); err != nil {
			return "", fmt.Errorf("unmarshal Members object: %w", err)
		}
		odataPath = member.ID

	default:
		return "", fmt.Errorf("unexpected Members format: %s", string(collection.Members))
	}

	if odataPath == "" {
		return "", errors.New("@odata.id is empty")
	}

	// Из "/redfish/v1/Systems/Self" вытаскиваем "Self"
	return path.Base(odataPath), nil
}

func (c *RedfishCollector) collectSystemData(ctx context.Context, client *http.Client, baseUrl, systemID string, result *UnionCollectResult) error {
	systemUrl, err := url.JoinPath(baseUrl, "Systems", systemID)
	if err != nil {
		return fmt.Errorf("failed to create url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, systemUrl, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to execute request, status code: %d, body: %s", resp.StatusCode, body)
	}

	jsonResp := minimalSystemResponse{}
	err = json.Unmarshal(body, &jsonResp)
	if err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	result.Redfish.Data.Manufacturer = normalizeString(jsonResp.Manufacturer)
	result.Redfish.Data.Model = normalizeString(jsonResp.Model)
	result.Redfish.Data.SerialNumber = normalizeString(jsonResp.SerialNumber)
	result.Redfish.Data.PartNumber = normalizeString(jsonResp.PartNumber)

	return nil
}
