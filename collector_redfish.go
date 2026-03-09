package identify

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"
)

const (
	RedfishCollectorType  CollectorType = "redfish"
	maxResponseBodyBytes  int64         = 1 << 20 // 1 MiB
	redfishRequestTimeout               = 30 * time.Second
)

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

type authTransport struct {
	base       http.RoundTripper
	authHeader string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", t.authHeader)

	return t.base.RoundTrip(clone) //nolint: wrapcheck
}

// buildTransport builds a TLS transport compatible with legacy hardware.
// When insecure=false — only secure ciphers are used.
// When insecure=true  — deprecated ciphers are added for compatibility
// and certificate verification is disabled.
func buildTransport(insecure bool) *http.Transport {
	ciphers := make([]uint16, 0, len(tls.CipherSuites()))
	for _, c := range tls.CipherSuites() {
		ciphers = append(ciphers, c.ID)
	}

	if insecure {
		for _, c := range tls.InsecureCipherSuites() {
			ciphers = append(ciphers, c.ID)
		}
	}

	return &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: insecure, //nolint: gosec
			MinVersion:         tls.VersionTLS10,
			CipherSuites:       ciphers,
		},
		ForceAttemptHTTP2: false,
	}
}

func basicAuth(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func newRedfishHTTPClient(username, password string, insecure bool) *http.Client {
	return &http.Client{
		Transport: &authTransport{
			base:       buildTransport(insecure),
			authHeader: basicAuth(username, password),
		},
		Timeout: redfishRequestTimeout,
	}
}

type RedfishCollector struct {
	l *slog.Logger
}

func NewRedfishCollector(l *slog.Logger) *RedfishCollector {
	return &RedfishCollector{l: l}
}

func (c *RedfishCollector) Type() CollectorType {
	return RedfishCollectorType
}

func (c *RedfishCollector) CollectRemote(ctx context.Context, result *UnionCollectResult, ip net.IP, opts *IdentifyOptions) error {
	result.Redfish = &RedfishResult{Status: CollectStatusError}

	client := newRedfishHTTPClient(opts.Credentials.Username, opts.Credentials.Password, opts.RedfishConfig.Insecure)

	host := ip.String()
	if opts.RedfishConfig.Port > 0 {
		host = fmt.Sprintf("%s:%d", host, opts.RedfishConfig.Port)
	}

	baseURL := url.URL{
		Scheme: "https",
		Host:   host,
		Path:   fmt.Sprintf("%s/redfish/v1/", opts.RedfishConfig.Prefix),
	}

	redfishVersion, err := c.getRedfishVersion(ctx, client, baseURL.String())
	if err != nil {
		return fmt.Errorf("failed to check redfish availability: %w", err)
	}

	result.Redfish.Data.Version = normalizeString(redfishVersion)
	result.Redfish.Status = CollectStatusPartialError

	systemID, err := c.getSystemID(ctx, client, baseURL.String())
	if err != nil {
		return fmt.Errorf("failed to get system id: %w", err)
	}

	if err = c.collectSystemData(ctx, client, baseURL.String(), systemID, result); err != nil {
		return fmt.Errorf("failed to collect system data: %w", err)
	}

	result.Redfish.Status = CollectStatusSuccess

	return nil
}

func (c *RedfishCollector) CollectLocal(_ context.Context, _ *UnionCollectResult, _ *IdentifyOptions) error {
	return errCollectNotImplemented
}

func (c *RedfishCollector) doGet(ctx context.Context, client *http.Client, rawURL string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, body)
	}

	if err = json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return nil
}

func (c *RedfishCollector) getRedfishVersion(ctx context.Context, client *http.Client, baseURL string) (string, error) {
	var resp minimalRootResponse

	if err := c.doGet(ctx, client, baseURL, &resp); err != nil {
		return "", err
	}

	return resp.RedfishVersion, nil
}

func (c *RedfishCollector) getSystemID(ctx context.Context, client *http.Client, baseURL string) (string, error) {
	systemsURL, err := url.JoinPath(baseURL, "Systems")
	if err != nil {
		return "", fmt.Errorf("failed to build systems url: %w", err)
	}

	var collection systemsCollection

	if err := c.doGet(ctx, client, systemsURL, &collection); err != nil {
		return "", err
	}

	return parseSystemID(collection)
}

func parseSystemID(collection systemsCollection) (string, error) {
	if len(collection.Members) == 0 {
		return "", errors.New("members field is missing")
	}

	var odataPath string

	switch collection.Members[0] {
	case '[':
		// Standard format: Members is an array.
		var members []odataID
		if err := json.Unmarshal(collection.Members, &members); err != nil {
			return "", fmt.Errorf("failed to unmarshal Members array: %w", err)
		}

		if len(members) == 0 {
			return "", errors.New("members array is empty")
		}

		odataPath = members[0].ID

	case '{':
		// Non-standard format: Members is an object (seen on Quanta and others).
		var member odataID
		if err := json.Unmarshal(collection.Members, &member); err != nil {
			return "", fmt.Errorf("failed to unmarshal Members object: %w", err)
		}

		odataPath = member.ID

	default:
		return "", fmt.Errorf("unexpected Members format: %s", string(collection.Members))
	}

	if odataPath == "" {
		return "", errors.New("@odata.id is empty")
	}

	// Extract the last path segment: "/redfish/v1/Systems/Self" → "Self".
	return path.Base(odataPath), nil
}

func (c *RedfishCollector) collectSystemData(ctx context.Context, client *http.Client, baseURL, systemID string, result *UnionCollectResult) error {
	systemURL, err := url.JoinPath(baseURL, "Systems", systemID)
	if err != nil {
		return fmt.Errorf("failed to build system url: %w", err)
	}

	var resp minimalSystemResponse

	if err := c.doGet(ctx, client, systemURL, &resp); err != nil {
		return err
	}

	result.Redfish.Data.Manufacturer = normalizeString(resp.Manufacturer)
	result.Redfish.Data.Model = normalizeString(resp.Model)
	result.Redfish.Data.SerialNumber = normalizeString(resp.SerialNumber)
	result.Redfish.Data.PartNumber = normalizeString(resp.PartNumber)

	return nil
}
