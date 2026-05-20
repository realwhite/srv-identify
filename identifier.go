package identify

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"slices"
	"sort"
	"strings"
	"unicode"
)

var errCollectNotImplemented = errors.New("collect not implemented")

const logAttrCollector = "collector"

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
	l          *slog.Logger
	collectors []Collector
}

func NewServerIdentifier(l *slog.Logger) *ServerIdentifier {
	return &ServerIdentifier{
		l: l,
		collectors: []Collector{
			NewIPMICollector(l),
			NewRedfishCollector(l),
		},
	}
}

func (id *ServerIdentifier) IdentifyRemote(ctx context.Context, ip net.IP, opts IdentifyOptions) (*IdentifyResult, error) {
	targetCollectors := DefaultCollectorsRemote
	if len(opts.Collectors) > 0 {
		targetCollectors = opts.Collectors
	}

	collectResult := &UnionCollectResult{}

	for _, collector := range id.collectors {
		if !slices.Contains(targetCollectors, collector.Type()) {
			id.l.Debug("collector skipped", logAttrCollector, collector.Type())

			continue
		}

		if err := collector.CollectRemote(ctx, collectResult, ip, &opts); err != nil {
			id.l.Error("failed to collect data", "error", err, logAttrCollector, collector.Type())
		}
	}

	return id.processResult(collectResult)
}

func (id *ServerIdentifier) IdentifyLocal(ctx context.Context, opts IdentifyOptions) (*IdentifyResult, error) {
	targetCollectors := DefaultCollectorsLocal
	if len(opts.Collectors) > 0 {
		targetCollectors = opts.Collectors
	}

	collectResult := &UnionCollectResult{}

	for _, collector := range id.collectors {
		if !slices.Contains(targetCollectors, collector.Type()) {
			id.l.Debug("collector skipped", logAttrCollector, collector.Type())

			continue
		}

		if err := collector.CollectLocal(ctx, collectResult, &opts); err != nil {
			if errors.Is(err, errCollectNotImplemented) {
				id.l.Debug("collector not implemented for local", logAttrCollector, collector.Type())

				continue
			}

			id.l.Error("failed to collect data", "error", err, logAttrCollector, collector.Type())
		}
	}

	return id.processResult(collectResult)
}

func isBlankValue(s string) bool {
	return strings.EqualFold(s, "") ||
		strings.EqualFold(s, "null") ||
		strings.EqualFold(s, "unknown")
}

func makeShortConstant(vendorName string) string {
	vendorName = strings.TrimSpace(vendorName)
	if isBlankValue(vendorName) {
		return ""
	}

	var b strings.Builder
	b.Grow(len(vendorName))

	for _, r := range vendorName {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToUpper(r))
		}
	}

	return b.String()
}

func (id *ServerIdentifier) mergeVendorSection(collectResult *UnionCollectResult, identifyResult *IdentifyResult) {
	identifyResult.Vendor = ResultVendor{}

	// Ordered candidates: the first non-blank value becomes Name.
	// Priority: IPMI FRU Product -> IPMI FRU Board -> Redfish -> IANA -> OUI.
	var orderedCandidates []string
	allCandidates := make(map[string]struct{})

	addCandidate := func(name string) {
		orderedCandidates = append(orderedCandidates, name)
		name = normalizeString(name)
		if !isBlankValue(name) {
			allCandidates[name] = struct{}{}
		}
	}

	if collectResult.IPMI != nil && collectResult.IPMI.Status != CollectStatusError {
		addCandidate(collectResult.IPMI.Data.FRUProductMfg)
		addCandidate(collectResult.IPMI.Data.FRUBoardMfg)

		identifyResult.Vendor.IANAEnterpriseID = collectResult.IPMI.Data.ManufacturerID
		// ManufacturerID=0 means "not set" (Reserved in IANA) — skip it.
		if id := collectResult.IPMI.Data.ManufacturerID; id != 0 {
			if vendorByIANA, ok := LookupIANANumber(id); ok {
				addCandidate(vendorByIANA)
			}
		}

		identifyResult.Vendor.MAC = collectResult.IPMI.Data.MAC
		if vendorByMac, ok := LookupOUI(collectResult.IPMI.Data.MAC); ok {
			addCandidate(vendorByMac)
			identifyResult.Vendor.OUI = vendorByMac
		}
	}

	if collectResult.Redfish != nil && collectResult.Redfish.Status != CollectStatusError {
		addCandidate(collectResult.Redfish.Data.Manufacturer)
	}

	for _, name := range orderedCandidates {
		if isBlankValue(name) {
			continue
		}

		identifyResult.Vendor.Name = name

		break
	}

	identifyResult.Vendor.ShortConstant = makeShortConstant(identifyResult.Vendor.Name)

	rawNames := make([]string, 0, len(allCandidates))
	for name := range allCandidates {
		rawNames = append(rawNames, name)
	}

	sort.Strings(rawNames)

	identifyResult.Vendor.Raw = rawNames
}

func (id *ServerIdentifier) mergeModelSection(collectResult *UnionCollectResult, identifyResult *IdentifyResult) {
	identifyResult.Model = ResultModel{}

	var modelCandidates []string
	var serialCandidates []string

	if collectResult.IPMI != nil && collectResult.IPMI.Status != CollectStatusError {
		modelCandidates = append(modelCandidates, collectResult.IPMI.Data.FRUProductName, collectResult.IPMI.Data.FRUBoardProduct)
		serialCandidates = append(serialCandidates, collectResult.IPMI.Data.FRUProductSerial)
	}

	if collectResult.Redfish != nil && collectResult.Redfish.Status != CollectStatusError {
		modelCandidates = append(modelCandidates, collectResult.Redfish.Data.Model)
		serialCandidates = append(serialCandidates, collectResult.Redfish.Data.SerialNumber)
	}

	for _, name := range modelCandidates {
		if isBlankValue(name) {
			continue
		}
		identifyResult.Model.Name = name

		break
	}

	for _, serial := range serialCandidates {
		if isBlankValue(serial) {
			continue
		}
		identifyResult.Model.SerialNumber = serial

		break
	}
}

func (id *ServerIdentifier) processResult(collectResult *UnionCollectResult) (*IdentifyResult, error) {
	identifyResult := &IdentifyResult{}

	id.mergeVendorSection(collectResult, identifyResult)
	id.mergeModelSection(collectResult, identifyResult)

	return identifyResult, nil
}

func LookupOUI(mac net.HardwareAddr) (string, bool) {
	if len(mac) < 3 {
		return "", false
	}

	key := strings.ToUpper(hex.EncodeToString(mac[:3]))
	v, ok := OUIDB[key]

	return v, ok
}

func LookupIANANumber(vendorID uint32) (string, bool) {
	v, ok := IANADB[vendorID]

	return v, ok
}
