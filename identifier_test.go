package identify

import (
	"log/slog"
	"net"
	"os"
	"sort"
	"testing"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func newID() *ServerIdentifier {
	return &ServerIdentifier{l: testLogger}
}

// ──────────────────────────────────────────────
// mergeVendorSection
// ──────────────────────────────────────────────

func TestMergeVendorSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		collect         UnionCollectResult
		wantVendorName  string
		wantShort       string
		wantRawContains []string // all of these must be present in Raw
	}{
		{
			name: "only_redfish",
			collect: UnionCollectResult{
				Redfish: &RedfishResult{
					Status: CollectStatusSuccess,
					Data:   RedfishData{Manufacturer: "Dell Inc."},
				},
			},
			wantVendorName:  "Dell Inc.",
			wantShort:       "DELLINC",
			wantRawContains: []string{"Dell Inc."},
		},
		{
			name: "only_ipmi_fru_product",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data:   IPMIData{FRUProductMfg: "Supermicro"},
				},
			},
			wantVendorName:  "Supermicro",
			wantShort:       "SUPERMICRO",
			wantRawContains: []string{"Supermicro"},
		},
		{
			// Priority: IPMI FRU Product → IPMI FRU Board → Redfish → IANA → OUI
			name: "ipmi_priority_over_redfish",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data:   IPMIData{FRUProductMfg: "Supermicro"},
				},
				Redfish: &RedfishResult{
					Status: CollectStatusSuccess,
					Data:   RedfishData{Manufacturer: "Super Micro Computer Inc."},
				},
			},
			wantVendorName:  "Supermicro",
			wantShort:       "SUPERMICRO",
			wantRawContains: []string{"Supermicro", "Super Micro Computer Inc."},
		},
		{
			name: "ipmi_board_when_product_empty",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data: IPMIData{
						FRUProductMfg: "",
						FRUBoardMfg:   "Quanta Computer",
					},
				},
			},
			wantVendorName:  "Quanta Computer",
			wantShort:       "QUANTACOMPUTER",
			wantRawContains: []string{"Quanta Computer"},
		},
		{
			name: "all_blank_values_skipped",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data: IPMIData{
						FRUProductMfg: "null",
						FRUBoardMfg:   "unknown",
					},
				},
				Redfish: &RedfishResult{
					Status: CollectStatusSuccess,
					Data:   RedfishData{Manufacturer: ""},
				},
			},
			wantVendorName: "",
			wantShort:      "",
		},
		{
			name: "ipmi_error_status_skipped",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusError,
					Data:   IPMIData{FRUProductMfg: "ShouldBeIgnored"},
				},
				Redfish: &RedfishResult{
					Status: CollectStatusSuccess,
					Data:   RedfishData{Manufacturer: "HPE"},
				},
			},
			wantVendorName:  "HPE",
			wantShort:       "HPE",
			wantRawContains: []string{"HPE"},
		},
		{
			name: "redfish_error_status_skipped",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data:   IPMIData{FRUProductMfg: "Lenovo"},
				},
				Redfish: &RedfishResult{
					Status: CollectStatusError,
					Data:   RedfishData{Manufacturer: "ShouldBeIgnored"},
				},
			},
			wantVendorName:  "Lenovo",
			wantRawContains: []string{"Lenovo"},
		},
		{
			name:           "no_sources",
			collect:        UnionCollectResult{},
			wantVendorName: "",
			wantShort:      "",
		},
		{
			// When FRU is empty, vendor name falls back to the OUI database lookup by MAC.
			// 0C:C4:7A is registered to Supermicro.
			name: "oui_used_as_fallback_vendor_name",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data: IPMIData{
						FRUProductMfg: "",
						FRUBoardMfg:   "",
						MAC:           net.HardwareAddr{0x0C, 0xC4, 0x7A, 0x00, 0x00, 0x01},
					},
				},
			},
			wantVendorName:  "Super Micro Computer, Inc.",
			wantRawContains: []string{"Super Micro Computer, Inc."},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := &IdentifyResult{}
			newID().mergeVendorSection(&tc.collect, result)

			if result.Vendor.Name != tc.wantVendorName {
				t.Errorf("Vendor.Name: got %q, want %q", result.Vendor.Name, tc.wantVendorName)
			}

			if tc.wantShort != "" && result.Vendor.ShortConstant != tc.wantShort {
				t.Errorf("Vendor.ShortConstant: got %q, want %q", result.Vendor.ShortConstant, tc.wantShort)
			}

			for _, want := range tc.wantRawContains {
				found := false
				for _, raw := range result.Vendor.Raw {
					if raw == want {
						found = true

						break
					}
				}

				if !found {
					t.Errorf("Vendor.Raw missing %q, got %v", want, result.Vendor.Raw)
				}
			}
		})
	}
}

// ──────────────────────────────────────────────
// mergeModelSection
// ──────────────────────────────────────────────

func TestMergeModelSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		collect    UnionCollectResult
		wantModel  string
		wantSerial string
	}{
		{
			name: "ipmi_fru_product_name",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data: IPMIData{
						FRUProductName:   "X11DPH-T",
						FRUProductSerial: "S123456",
					},
				},
			},
			wantModel:  "X11DPH-T",
			wantSerial: "S123456",
		},
		{
			name: "ipmi_board_product_when_product_name_empty",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data: IPMIData{
						FRUProductName:  "",
						FRUBoardProduct: "X11DPH-T (board)",
					},
				},
			},
			wantModel: "X11DPH-T (board)",
		},
		{
			name: "redfish_model",
			collect: UnionCollectResult{
				Redfish: &RedfishResult{
					Status: CollectStatusSuccess,
					Data: RedfishData{
						Model:        "PowerEdge R750",
						SerialNumber: "ABCD123",
					},
				},
			},
			wantModel:  "PowerEdge R750",
			wantSerial: "ABCD123",
		},
		{
			name: "ipmi_wins_when_both_present",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data: IPMIData{
						FRUProductName:   "X11DPH-T",
						FRUProductSerial: "IPMI-SERIAL",
					},
				},
				Redfish: &RedfishResult{
					Status: CollectStatusSuccess,
					Data: RedfishData{
						Model:        "PowerEdge R750",
						SerialNumber: "REDFISH-SERIAL",
					},
				},
			},
			wantModel:  "X11DPH-T",
			wantSerial: "IPMI-SERIAL",
		},
		{
			name: "blank_model_values_skipped",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data: IPMIData{
						FRUProductName:  "null",
						FRUBoardProduct: "unknown",
					},
				},
				Redfish: &RedfishResult{
					Status: CollectStatusSuccess,
					Data:   RedfishData{Model: "PowerEdge R750"},
				},
			},
			wantModel: "PowerEdge R750",
		},
		{
			name: "blank_serial_values_skipped",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusSuccess,
					Data: IPMIData{
						FRUProductName:   "X11DPH-T",
						FRUProductSerial: "null",
					},
				},
				Redfish: &RedfishResult{
					Status: CollectStatusSuccess,
					Data:   RedfishData{SerialNumber: "REAL-SERIAL"},
				},
			},
			wantModel:  "X11DPH-T",
			wantSerial: "REAL-SERIAL",
		},
		{
			name: "ipmi_error_status_skipped",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusError,
					Data:   IPMIData{FRUProductName: "ShouldBeIgnored"},
				},
				Redfish: &RedfishResult{
					Status: CollectStatusSuccess,
					Data:   RedfishData{Model: "PowerEdge R750"},
				},
			},
			wantModel: "PowerEdge R750",
		},
		{
			name: "partial_error_still_used",
			collect: UnionCollectResult{
				IPMI: &IPMIResult{
					Status: CollectStatusPartialError,
					Data:   IPMIData{FRUProductName: "X11DPH-T"},
				},
			},
			wantModel: "X11DPH-T",
		},
		{
			name:       "no_sources",
			collect:    UnionCollectResult{},
			wantModel:  "",
			wantSerial: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := &IdentifyResult{}
			newID().mergeModelSection(&tc.collect, result)

			if result.Model.Name != tc.wantModel {
				t.Errorf("Model.Name: got %q, want %q", result.Model.Name, tc.wantModel)
			}

			if result.Model.SerialNumber != tc.wantSerial {
				t.Errorf("Model.SerialNumber: got %q, want %q", result.Model.SerialNumber, tc.wantSerial)
			}
		})
	}
}

// ──────────────────────────────────────────────
// isBlankValue
// ──────────────────────────────────────────────

func TestIsBlankValue(t *testing.T) {
	t.Parallel()

	blank := []string{"", "null", "NULL", "Null", "unknown", "UNKNOWN", "Unknown"}
	notBlank := []string{"Dell", "0", "HPE", " ", "none", "n/a"}

	for _, s := range blank {
		s := s
		t.Run("blank_"+s, func(t *testing.T) {
			t.Parallel()
			if !isBlankValue(s) {
				t.Errorf("isBlankValue(%q) = false, want true", s)
			}
		})
	}

	for _, s := range notBlank {
		s := s
		t.Run("not_blank_"+s, func(t *testing.T) {
			t.Parallel()
			if isBlankValue(s) {
				t.Errorf("isBlankValue(%q) = true, want false", s)
			}
		})
	}
}

// ──────────────────────────────────────────────
// makeShortConstant
// ──────────────────────────────────────────────

func TestMakeShortConstant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"Dell Inc.", "DELLINC"},
		{"Dell Computer Corporation", "DELLCOMPUTERCORPORATION"},
		{"Hewlett Packard Enterprise", "HEWLETTPACKARDENTERPRISE"},
		{"Hewlett Packard Enterprise Co.", "HEWLETTPACKARDENTERPRISECO"},
		{"HPE", "HPE"},
		{"Super Micro Computer, Inc.", "SUPERMICROCOMPUTERINC"},
		{"Supermicro", "SUPERMICRO"},
		{"", ""},
		{"  Leading", "LEADING"},
		{"Acme Hardware Ltd.", "ACMEHARDWARELTD"},
		{"Acme-Hardware, LLC", "ACMEHARDWARELLC"},
		{" Acme  Hardware ", "ACMEHARDWARE"},
		{"One", "ONE"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := makeShortConstant(tc.input)
			if got != tc.want {
				t.Errorf("makeShortConstant(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ──────────────────────────────────────────────
// LookupOUI / LookupIANANumber — smoke tests
// ──────────────────────────────────────────────

func TestLookupOUI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mac    net.HardwareAddr
		wantOK bool
	}{
		{
			name:   "short_mac_rejected",
			mac:    net.HardwareAddr{0x00, 0x01},
			wantOK: false,
		},
		{
			name:   "nil_mac_rejected",
			mac:    nil,
			wantOK: false,
		},
		{
			// 00:00:00 — XEROX, guaranteed to be in the database.
			name:   "xerox_oui",
			mac:    net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
			wantOK: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ok := LookupOUI(tc.mac)
			if ok != tc.wantOK {
				t.Errorf("LookupOUI ok=%v, want %v", ok, tc.wantOK)
			}
		})
	}
}

func TestLookupIANANumber(t *testing.T) {
	t.Parallel()

	// ID 343 = Dell — a stable entry in the IANA registry.
	name, ok := LookupIANANumber(343)
	if !ok {
		t.Fatal("LookupIANANumber(343) not found, expected Dell entry")
	}

	if name == "" {
		t.Error("LookupIANANumber(343) returned empty string")
	}
}

// ──────────────────────────────────────────────
// Vendor.Raw contains no duplicates
// ──────────────────────────────────────────────

func TestVendorRawNoDuplicates(t *testing.T) {
	t.Parallel()

	collect := UnionCollectResult{
		IPMI: &IPMIResult{
			Status: CollectStatusSuccess,
			Data: IPMIData{
				// Both fields are identical — the duplicate must not appear in Raw.
				FRUProductMfg: "Supermicro",
				FRUBoardMfg:   "Supermicro",
			},
		},
	}

	result := &IdentifyResult{}
	newID().mergeVendorSection(&collect, result)

	seen := make(map[string]int)
	for _, r := range result.Vendor.Raw {
		seen[r]++
	}

	for v, count := range seen {
		if count > 1 {
			t.Errorf("Vendor.Raw contains duplicate %q (%d times)", v, count)
		}
	}
}

// ──────────────────────────────────────────────
// processResult — integration test
// ──────────────────────────────────────────────

func TestProcessResult(t *testing.T) {
	t.Parallel()

	collect := &UnionCollectResult{
		IPMI: &IPMIResult{
			Status: CollectStatusSuccess,
			Data: IPMIData{
				FRUProductMfg:    "Supermicro",
				FRUProductName:   "X11DPH-T",
				FRUProductSerial: "S789",
			},
		},
		Redfish: &RedfishResult{
			Status: CollectStatusSuccess,
			Data: RedfishData{
				Manufacturer: "Super Micro Computer Inc.",
				Model:        "SYS-2049U",
				SerialNumber: "R456",
			},
		},
	}

	result, err := newID().processResult(collect)
	if err != nil {
		t.Fatalf("processResult error: %v", err)
	}

	if result.Vendor.Name == "" {
		t.Error("Vendor.Name is empty")
	}

	if result.Model.Name == "" {
		t.Error("Model.Name is empty")
	}

	if result.Model.SerialNumber == "" {
		t.Error("Model.SerialNumber is empty")
	}

	// Sort Raw for stable comparison.
	raw := make([]string, len(result.Vendor.Raw))
	copy(raw, result.Vendor.Raw)
	sort.Strings(raw)

	if len(raw) < 2 {
		t.Errorf("Vendor.Raw should contain data from multiple sources, got %v", raw)
	}
}
