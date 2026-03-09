package identify

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────────────────────────
// parseSystemID
// ──────────────────────────────────────────────

func TestParseSystemID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload string
		want    string
		wantErr bool
	}{
		{
			// Standard format: Members is an array of objects.
			name:    "array_standard",
			payload: `{"Members": [{"@odata.id": "/redfish/v1/Systems/Self"}]}`,
			want:    "Self",
		},
		{
			// Multiple members — take the first one.
			name:    "array_multiple_members",
			payload: `{"Members": [{"@odata.id": "/redfish/v1/Systems/1"}, {"@odata.id": "/redfish/v1/Systems/2"}]}`,
			want:    "1",
		},
		{
			// Non-standard format: Members is an object (seen on Quanta and others).
			name:    "object_nonstandard",
			payload: `{"Members": {"@odata.id": "/redfish/v1/Systems/System.Embedded.1"}}`,
			want:    "System.Embedded.1",
		},
		{
			// Members field is absent from JSON.
			name:    "missing_members_field",
			payload: `{}`,
			wantErr: true,
		},
		{
			// Members is an empty array.
			name:    "empty_array",
			payload: `{"Members": []}`,
			wantErr: true,
		},
		{
			// @odata.id is empty inside the array.
			name:    "empty_odata_id_in_array",
			payload: `{"Members": [{"@odata.id": ""}]}`,
			wantErr: true,
		},
		{
			// @odata.id is empty inside the object.
			name:    "empty_odata_id_in_object",
			payload: `{"Members": {"@odata.id": ""}}`,
			wantErr: true,
		},
		{
			// Unknown Members format (a number).
			name:    "unknown_format",
			payload: `{"Members": 42}`,
			wantErr: true,
		},
		{
			// Only a single name segment in the path.
			name:    "single_path_segment",
			payload: `{"Members": [{"@odata.id": "/System"}]}`,
			want:    "System",
		},
		{
			// Deeply nested path — last segment is returned.
			name:    "deep_path",
			payload: `{"Members": [{"@odata.id": "/redfish/v1/Systems/Chassis/Node/Self"}]}`,
			want:    "Self",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var collection systemsCollection
			if err := json.Unmarshal([]byte(tc.payload), &collection); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}

			got, err := parseSystemID(collection)

			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got %q", got)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
