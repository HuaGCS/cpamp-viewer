package httpapi

import (
	"encoding/json"
	"math"
)

// Counts and timestamps must be exactly representable by the browser. A string,
// negative, fraction or missing value must never become evidence of zero usage.
func publicUsageInteger(value any) (int64, bool) {
	var number float64
	switch v := value.(type) {
	case float64:
		number = v
	case int64:
		number = float64(v)
	case int:
		number = float64(v)
	case json.Number:
		var err error
		number, err = v.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 9007199254740991 || math.Trunc(number) != number {
		return 0, false
	}
	return int64(number), true
}

func usageEnum(value any, allowed ...string) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	for _, candidate := range allowed {
		if text == candidate {
			return text
		}
	}
	return ""
}

func projectUsageCoverage(value any) map[string]any {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	mode := usageEnum(raw["mode"], "raw", "mixed", "aggregate_only")
	scope := usageEnum(raw["scope"], "time_range", "requested_ranges")
	complete, completeOK := raw["raw_complete"].(bool)
	aggregate, aggregateOK := raw["core_aggregate_used"].(bool)
	deleted, deletedOK := publicUsageInteger(raw["raw_deleted_event_count"])
	if mode == "" || scope == "" || !completeOK || !aggregateOK || !deletedOK {
		return nil
	}
	result := map[string]any{"scope": scope, "mode": mode, "raw_complete": complete, "core_aggregate_used": aggregate, "raw_deleted_event_count": deleted}
	for _, key := range []string{"raw_event_count", "min_deleted_timestamp_ms", "max_deleted_timestamp_ms", "comparison_raw_event_count", "comparison_raw_deleted_event_count", "comparison_min_deleted_timestamp_ms", "comparison_max_deleted_timestamp_ms"} {
		if n, ok := publicUsageInteger(raw[key]); ok {
			result[key] = n
		}
	}
	limitations := []string{}
	seen := map[string]bool{}
	if list, ok := raw["fidelity_limitations"].([]any); ok {
		for _, v := range list {
			if _, ok := v.(string); !ok {
				if !seen["other_metrics_require_raw_events"] {
					limitations = append(limitations, "other_metrics_require_raw_events")
					seen["other_metrics_require_raw_events"] = true
				}
				continue
			}
			name := usageEnum(v, "core_metrics_require_raw_events", "extended_summary_metrics_require_raw_events", "timeline_metrics_require_raw_events", "timeline_latency_percentiles_require_raw_events", "distribution_metrics_require_raw_events", "failure_metrics_require_raw_events", "credential_metrics_require_raw_events", "identity_metrics_require_raw_events", "filter_options_require_raw_events", "event_details_require_raw_events", "summary_comparison_requires_raw_events", "rolling_window_metrics_require_raw_events")
			// Future limitation messages may contain diagnostics. Retain the fact
			// that fidelity is limited, without disclosing their untrusted text.
			if name == "" {
				name = "other_metrics_require_raw_events"
			}
			if !seen[name] {
				limitations = append(limitations, name)
				seen[name] = true
			}
		}
	} else {
		// Only an explicitly reported empty list proves there are no further
		// limitations. Invalid/new schemas must not claim full fidelity.
		limitations = append(limitations, "other_metrics_require_raw_events")
	}
	result["fidelity_limitations"] = limitations
	ranges := []any{}
	if list, ok := raw["auxiliary_ranges"].([]any); ok {
		for _, v := range list {
			r, ok := v.(map[string]any)
			if !ok {
				continue
			}
			scope := usageEnum(r["scope"], "rolling_30m", "drilldown_preview")
			from, fromOK := publicUsageInteger(r["from_ms"])
			to, toOK := publicUsageInteger(r["to_ms"])
			count, countOK := publicUsageInteger(r["raw_deleted_event_count"])
			if scope == "" || !fromOK || !toOK || to < from || !countOK {
				continue
			}
			safe := map[string]any{"scope": scope, "from_ms": from, "to_ms": to, "raw_deleted_event_count": count}
			for _, key := range []string{"raw_event_count", "min_deleted_timestamp_ms", "max_deleted_timestamp_ms"} {
				if n, ok := publicUsageInteger(r[key]); ok {
					safe[key] = n
				}
			}
			ranges = append(ranges, safe)
			if len(ranges) == 8 {
				break
			}
		}
	}
	result["auxiliary_ranges"] = ranges
	return result
}
