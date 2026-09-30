package httpapi

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestUsageCoveragePreservesHistoricalFidelityWithoutPrivateFields(t *testing.T) {
	response := map[string]any{"summary": map[string]any{"total_calls": float64(7)}, "coverage": map[string]any{
		"scope": "requested_ranges", "mode": "mixed", "raw_complete": false, "core_aggregate_used": true,
		"raw_deleted_event_count": 4, "raw_event_count": 3, "min_deleted_timestamp_ms": 100, "max_deleted_timestamp_ms": 500,
		"comparison_raw_deleted_event_count": 2,
		"fidelity_limitations":               []any{"event_details_require_raw_events", "private-diagnostic", "private-diagnostic", false},
		"archive_path":                       "/private-archive", "run_id": "private-run", "token": "private-token",
		"auxiliary_ranges": []any{map[string]any{"scope": "rolling_30m", "from_ms": 100, "to_ms": 600, "raw_deleted_event_count": 2, "raw_event_count": 1, "path": "private-path"}},
	}}
	projectAnalytics(response)
	coverage := response["coverage"].(map[string]any)
	if coverage["raw_complete"] != false || coverage["core_aggregate_used"] != true || coverage["raw_deleted_event_count"] != int64(4) || coverage["comparison_raw_deleted_event_count"] != int64(2) {
		t.Fatalf("coverage lost: %#v", coverage)
	}
	if len(coverage["auxiliary_ranges"].([]any)) != 1 || len(coverage["fidelity_limitations"].([]string)) != 2 {
		t.Fatalf("fidelity evidence lost: %#v", coverage)
	}
	b, _ := json.Marshal(response)
	if strings.Contains(string(b), "private") || strings.Contains(string(b), "run_id") {
		t.Fatalf("private coverage data leaked: %s", b)
	}
	if response["summary"].(map[string]any)["total_calls"] != float64(7) {
		t.Fatal("projected core totals changed")
	}
}

func TestUsageCoverageRejectsFalseSchemaAndInvalidCounters(t *testing.T) {
	for _, bad := range []any{nil, false, "0", map[string]any{"raw_deleted_event_count": 0}, map[string]any{"scope": "time_range", "mode": "mixed", "raw_complete": "false", "core_aggregate_used": true, "raw_deleted_event_count": 4}} {
		if projectUsageCoverage(bad) != nil {
			t.Fatalf("invalid schema accepted: %#v", bad)
		}
	}
	for _, bad := range []any{nil, true, "2", -1, 0.5, math.Inf(1), math.NaN(), float64(9007199254740992)} {
		if _, ok := publicUsageInteger(bad); ok {
			t.Fatalf("invalid number accepted: %#v", bad)
		}
	}
	response := map[string]any{"summary": map[string]any{"total_calls": 3}}
	projectAnalytics(response)
	if _, ok := response["coverage"]; ok {
		t.Fatal("coverage invented for legacy server")
	}
}

func TestUsageCoverageMalformedLimitationsNeverClaimCompleteDerivedMetrics(t *testing.T) {
	for _, value := range []any{nil, true, "diagnostic", map[string]any{"secret": "private"}, []any{false, "private-diagnostic"}} {
		coverage := projectUsageCoverage(map[string]any{"scope": "time_range", "mode": "aggregate_only", "raw_complete": false, "core_aggregate_used": true, "raw_deleted_event_count": 4, "fidelity_limitations": value})
		limits := coverage["fidelity_limitations"].([]string)
		if len(limits) != 1 || limits[0] != "other_metrics_require_raw_events" {
			t.Fatalf("invalid limitations claimed complete fidelity: %#v", coverage)
		}
	}
	coverage := projectUsageCoverage(map[string]any{"scope": "time_range", "mode": "aggregate_only", "raw_complete": false, "core_aggregate_used": true, "raw_deleted_event_count": 4, "fidelity_limitations": []any{}})
	if len(coverage["fidelity_limitations"].([]string)) != 0 {
		t.Fatal("explicit valid empty evidence changed")
	}
}
