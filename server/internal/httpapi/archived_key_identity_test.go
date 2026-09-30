package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArchivedKeylessAggregateCannotBecomeSelectableKey(t *testing.T) {
	for _, rawHash := range []any{nil, "", "unusable-group-key"} {
		row := map[string]any{"id": "archived-private-aggregate-group", "calls": json.Number("1"), "total_tokens": json.Number("50"), "cost": 0.001}
		if rawHash != nil {
			row["api_key_hash"] = rawHash
		}
		response := map[string]any{"api_key_stats": []any{row}, "filter_options": map[string]any{"api_key_stats": []any{row}}}
		projectAnalytics(response)
		for _, stats := range []any{response["api_key_stats"], response["filter_options"].(map[string]any)["api_key_stats"]} {
			got := stats.([]any)[0].(map[string]any)
			if got["api_key_selectable"] != false || got["api_key_id"] == "" || got["api_key_id"] == nil {
				t.Fatalf("archived group became filterable: %#v", got)
			}
			if got["calls"] != json.Number("1") || got["total_tokens"] != json.Number("50") || got["cost"] != 0.001 {
				t.Fatalf("keyless historical totals changed: %#v", got)
			}
		}
		encoded, _ := json.Marshal(response)
		if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "unusable-group-key") {
			t.Fatal("internal grouping identity leaked")
		}
	}
}

func TestArchivedRealKeyStillUsesAuthenticatedHashIdentity(t *testing.T) {
	hash := strings.Repeat("a", 64)
	response := map[string]any{"api_key_stats": []any{map[string]any{"id": "aggregate-group", "api_key_hash": hash, "calls": json.Number("2")}}}
	projectAnalytics(response)
	row := response["api_key_stats"].([]any)[0].(map[string]any)
	if row["api_key_id"] != pseudonym(hash) || row["api_key_selectable"] == false {
		t.Fatalf("known key became unavailable: %#v", row)
	}
}
