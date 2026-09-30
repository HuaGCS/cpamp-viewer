package httpapi

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

var codexUsageTestObserved = time.Date(2030, 9, 9, 12, 0, 0, 0, time.UTC).UnixMilli()

func codexUsageTestWindow(used, seconds float64) map[string]any {
	return map[string]any{
		"used_percent": used, "limit_window_seconds": seconds,
		"reset_after_seconds": float64(3600),
	}
}

func codexUsageTestPayload() map[string]any {
	return map[string]any{
		"plan_type": "pro",
		"rate_limit": map[string]any{
			"primary_window":   codexUsageTestWindow(48, 604800),
			"secondary_window": nil,
		},
		"additional_rate_limits": []any{
			map[string]any{
				"metered_feature": "codex_spark", "limit_name": "GPT-5.3-Codex-Spark",
				"rate_limit": map[string]any{
					"primary_window":   codexUsageTestWindow(3, 18000),
					"secondary_window": codexUsageTestWindow(63, 604800),
				},
			},
		},
		"code_review_rate_limit": nil,
	}
}

func codexUsageTestFind(t *testing.T, windows []quotaWindow, pool string, minutes float64) quotaWindow {
	t.Helper()
	var result quotaWindow
	count := 0
	for _, window := range windows {
		if window.Pool == pool && window.WindowMins == minutes {
			result = window
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one %s/%v window, found %d: %#v", pool, minutes, count, windows)
	}
	return result
}

func TestCodexUsageFullInventoryIgnoresGenericSparkHeaderMirror(t *testing.T) {
	payload := codexUsageTestPayload()
	// These lossy diagnostics mirror Spark. Only the wham rate-limit containers
	// establish quota inventory, even when the request itself used Spark.
	payload["model"] = "gpt-5.3-codex-spark"
	payload["active_limit"] = "codex_bengalfox"
	payload["primary"] = map[string]any{"used_percent": float64(3), "window_minutes": float64(300)}
	payload["secondary"] = map[string]any{"used_percent": float64(63), "window_minutes": float64(10080)}
	payload["signals"] = map[string]any{
		"x-codex-primary-used-percent": "3", "x-codex-primary-window-minutes": "300",
		"x-codex-secondary-used-percent": "63", "x-codex-secondary-window-minutes": "10080",
	}
	windows, plan, valid := projectCodexUsage(payload, codexUsageTestObserved)
	if !valid || plan != "pro" || len(windows) != 3 {
		t.Fatalf("unexpected complete inventory: %t %q %#v", valid, plan, windows)
	}
	if codexUsageTestFind(t, windows, "codex_main", 10080).Remaining != 52 ||
		codexUsageTestFind(t, windows, "codex_spark", 300).Remaining != 97 ||
		codexUsageTestFind(t, windows, "codex_spark", 10080).Remaining != 37 {
		t.Fatalf("main/Spark values were mixed: %#v", windows)
	}
	ids := make(map[string]bool)
	for _, window := range windows {
		if window.Pool == "unknown" || (window.Pool == "codex_main" && window.WindowMins == 300) || ids[window.ID] {
			t.Fatalf("a mirrored/duplicate/invented window survived: %#v", windows)
		}
		ids[window.ID] = true
		if window.ObservedAt != codexUsageTestObserved {
			t.Fatalf("window reused a historical timestamp: %#v", window)
		}
	}
}

func TestCodexUsageKeepsRealProFiveHourAndClassifiesSwappedSlotsByDuration(t *testing.T) {
	payload := map[string]any{
		"plan_type": "pro",
		"rate_limit": map[string]any{
			"primary_window":   codexUsageTestWindow(10, 604800),
			"secondary_window": codexUsageTestWindow(30, 18000),
		},
	}
	windows, _, valid := projectCodexUsage(payload, codexUsageTestObserved)
	if !valid || len(windows) != 2 {
		t.Fatalf("valid Pro windows were hidden: %#v", windows)
	}
	if codexUsageTestFind(t, windows, "codex_main", 300).Remaining != 70 || codexUsageTestFind(t, windows, "codex_main", 10080).Remaining != 90 {
		t.Fatalf("window positions overrode real durations: %#v", windows)
	}
}

func TestCodexUsageValidEmptyInventoryClearsHistoricalWindows(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"null main":               {"rate_limit": nil},
		"empty main":              {"rate_limit": map[string]any{}},
		"null slots":              {"rate_limit": map[string]any{"primary_window": nil, "secondary_window": nil}},
		"empty additional array":  {"additional_rate_limits": []any{}},
		"empty additional object": {"additional_rate_limits": map[string]any{}},
		"all null":                {"rate_limit": nil, "code_review_rate_limit": nil, "additional_rate_limits": nil},
	} {
		t.Run(name, func(t *testing.T) {
			windows, _, valid := projectCodexUsage(payload, codexUsageTestObserved)
			if !valid || windows == nil || len(windows) != 0 {
				t.Fatalf("valid empty inventory was treated as unavailable: %t %#v", valid, windows)
			}
		})
	}
}

func TestCodexUsageRejectsNonInventoryAndMalformedContainers(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"missing":                 {},
		"plan only":               {"plan_type": "pro"},
		"error":                   {"error": "private-error"},
		"header only":             {"primary": map[string]any{"used_percent": 0}},
		"main string":             {"rate_limit": "private-invalid"},
		"main array":              {"rate_limit": []any{}},
		"review boolean":          {"rate_limit": nil, "code_review_rate_limit": false},
		"window string":           {"rate_limit": map[string]any{"primary_window": "private-invalid"}},
		"additional number":       {"additional_rate_limits": float64(1)},
		"additional bad entry":    {"additional_rate_limits": []any{"private-invalid"}},
		"additional missing rate": {"additional_rate_limits": []any{map[string]any{"limit_name": "Spark"}}},
		"additional rate string":  {"additional_rate_limits": []any{map[string]any{"rate_limit": "invalid"}}},
	} {
		t.Run(name, func(t *testing.T) {
			windows, _, valid := projectCodexUsage(payload, codexUsageTestObserved)
			if valid || len(windows) != 0 {
				t.Fatalf("invalid structure was accepted as a complete inventory: %#v", windows)
			}
		})
	}
}

func TestCodexUsageSnakeAndCamelCaseNumbersAndTimes(t *testing.T) {
	absolute := codexUsageTestObserved + int64(2*time.Hour/time.Millisecond)
	payload := map[string]any{
		"planType": "pro",
		"rateLimit": map[string]any{
			"primaryWindow": map[string]any{
				"usedPercent": "12.5", "limitWindowSeconds": json.Number("18000"),
				"resetAt": float64(absolute / 1000), "resetAfterSeconds": float64(1),
			},
		},
		"codeReviewRateLimit": map[string]any{
			"secondaryWindow": map[string]any{"usedPercent": float64(20), "limitWindowSeconds": float64(604800), "resetAfterSeconds": float64(120)},
		},
		"additionalRateLimits": []any{map[string]any{
			"meteredFeature": "codex_spark", "limitName": "Spark",
			"rateLimit": map[string]any{"primaryWindow": map[string]any{"usedPercent": float64(2), "limitWindowSeconds": float64(18000)}},
		}},
	}
	windows, plan, valid := projectCodexUsage(payload, codexUsageTestObserved)
	if !valid || plan != "pro" || len(windows) != 3 {
		t.Fatalf("camelCase inventory was not decoded: %#v", windows)
	}
	main := codexUsageTestFind(t, windows, "codex_main", 300)
	review := codexUsageTestFind(t, windows, "codex_review", 10080)
	spark := codexUsageTestFind(t, windows, "codex_spark", 300)
	if main.Remaining != 87.5 || main.ResetAtMS != absolute || review.ResetAtMS != codexUsageTestObserved+120000 || spark.ResetAtMS != 0 {
		t.Fatalf("numbers, absolute/relative reset precedence or missing reset were wrong: %#v", windows)
	}
}

func TestCodexUsageInvalidOrMissingUsedNeverBecomesFullQuota(t *testing.T) {
	for _, used := range []any{nil, "", "bad", "NaN", "+Inf", float64(-1), float64(101), true} {
		payload := map[string]any{"rate_limit": map[string]any{
			"allowed": false, "limit_reached": true,
			"primary_window": map[string]any{"used_percent": used, "limit_window_seconds": float64(18000), "reset_after_seconds": float64(120)},
		}}
		windows, _, valid := projectCodexUsage(payload, codexUsageTestObserved)
		if !valid || len(windows) != 0 {
			t.Fatalf("missing/invalid used value manufactured quota: %#v", windows)
		}
	}
}

func TestCodexUsageSuppressesZeroSlotsButPreservesRealZeroUsage(t *testing.T) {
	for name, slot := range map[string]map[string]any{
		"explicit zero":                 {"used_percent": float64(0), "limit_window_seconds": float64(0), "reset_after_seconds": float64(0)},
		"zero slot with unused percent": {"used_percent": float64(10), "limit_window_seconds": float64(0)},
		"empty slot":                    {},
		"zero without window":           {"used_percent": float64(0)},
	} {
		t.Run(name, func(t *testing.T) {
			payload := map[string]any{"rate_limit": map[string]any{"primary_window": slot}}
			windows, _, valid := projectCodexUsage(payload, codexUsageTestObserved)
			if !valid || len(windows) != 0 {
				t.Fatalf("an absent/zero slot became another quota window: %#v", windows)
			}
		})
	}
	payload := map[string]any{"rate_limit": map[string]any{
		"primary_window":   map[string]any{"used_percent": float64(0), "limit_window_seconds": float64(18000)},
		"secondary_window": map[string]any{"used_percent": float64(5)},
	}}
	windows, _, valid := projectCodexUsage(payload, codexUsageTestObserved)
	if !valid || len(windows) != 2 || codexUsageTestFind(t, windows, "codex_main", 300).Remaining != 100 {
		t.Fatalf("genuine zero usage or unknown-duration positive usage was lost: %#v", windows)
	}
	unknownDuration := codexUsageTestFind(t, windows, "codex_main", 0)
	if unknownDuration.ResetAtMS != 0 || unknownDuration.Label != "额度窗口" {
		t.Fatalf("missing duration/reset was invented: %#v", unknownDuration)
	}
}

func TestCodexUsageAdditionalFeaturePrecedesDisplayNameAndMapKey(t *testing.T) {
	for _, test := range []struct {
		feature any
		name    string
		pool    string
	}{
		{"codex_spark", "Anything", "codex_spark"},
		{"codex_bengalfox", "Anything", "codex_spark"},
		{nil, "GPT-5.3-Codex-Spark", "codex_spark"},
		{"", "Spark", "codex_spark"},
		{"future_feature", "Spark", "unknown"},
		{float64(1), "Spark", "unknown"},
		{nil, "Not really Spark", "unknown"},
		{nil, "", "unknown"},
	} {
		entry := map[string]any{"metered_feature": test.feature, "limit_name": test.name, "rate_limit": map[string]any{"primary_window": codexUsageTestWindow(20, 18000)}}
		payload := map[string]any{"additional_rate_limits": map[string]any{"codex_spark": entry}}
		windows, _, valid := projectCodexUsage(payload, codexUsageTestObserved)
		if !valid || len(windows) != 1 || windows[0].Pool != test.pool {
			t.Fatalf("feature precedence or unknown map scope was wrong: feature=%v name=%q windows=%#v", test.feature, test.name, windows)
		}
	}
}

func TestCodexUsageAdditionalSupportsSingleObjectAndDirectRateMapping(t *testing.T) {
	for name, additional := range map[string]any{
		"single entry":       map[string]any{"metered_feature": "codex_spark", "rate_limit": map[string]any{"primary_window": codexUsageTestWindow(25, 18000)}},
		"direct mapped rate": map[string]any{"private-feature": map[string]any{"primary_window": codexUsageTestWindow(25, 18000)}},
		"null entries":       []any{nil, map[string]any{"limit_name": "Spark", "rate_limit": map[string]any{"primary_window": codexUsageTestWindow(25, 18000)}}},
	} {
		t.Run(name, func(t *testing.T) {
			windows, _, valid := projectCodexUsage(map[string]any{"additional_rate_limits": additional}, codexUsageTestObserved)
			if !valid || len(windows) != 1 || windows[0].Remaining != 75 {
				t.Fatalf("supported additional object was not parsed: %#v", windows)
			}
		})
	}
}

func TestCodexUsageDuplicatesAndConflictsDoNotReuseWindowIDs(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		first := map[string]any{"metered_feature": "codex_spark", "rate_limit": map[string]any{"primary_window": codexUsageTestWindow(3, 18000)}}
		used := float64(3)
		if conflict {
			used = 20
		}
		second := map[string]any{"limit_name": "Spark", "rate_limit": map[string]any{"primary_window": codexUsageTestWindow(used, 18000)}}
		windows, _, valid := projectCodexUsage(map[string]any{"additional_rate_limits": []any{first, second}}, codexUsageTestObserved)
		want := 1
		if conflict {
			want = 0
		}
		if !valid || len(windows) != want {
			t.Fatalf("duplicate/conflicting windows were selected by array order: conflict=%v windows=%#v", conflict, windows)
		}
	}
}

func TestCodexUsageMalformedDurationRemainsJSONSafe(t *testing.T) {
	for _, duration := range []any{"NaN", "+Inf", "bad", float64(-1)} {
		payload := map[string]any{"rate_limit": map[string]any{"primary_window": map[string]any{"used_percent": float64(25), "limit_window_seconds": duration}}}
		windows, _, valid := projectCodexUsage(payload, codexUsageTestObserved)
		if !valid || len(windows) != 1 || windows[0].WindowMins != 0 || math.IsNaN(windows[0].WindowMins) {
			t.Fatalf("invalid duration contaminated a window: %#v", windows)
		}
		if _, err := json.Marshal(windows); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCodexUsageOnlyPublishesSafeWindowFields(t *testing.T) {
	payload := codexUsageTestPayload()
	payload["email"] = "PRIVATE_EMAIL_SENTINEL"
	payload["account_id"] = "PRIVATE_ACCOUNT_SENTINEL"
	payload["access_token"] = "PRIVATE_TOKEN_SENTINEL"
	payload["rate_limit_reset_credits"] = map[string]any{"credits": []any{"PRIVATE_CREDIT_ID"}}
	payload["additional_rate_limits"] = append(payload["additional_rate_limits"].([]any), map[string]any{
		"metered_feature": "PRIVATE_FEATURE_SENTINEL", "limit_name": "PRIVATE_NAME_SENTINEL",
		"rate_limit": map[string]any{"primary_window": codexUsageTestWindow(50, 18000)},
	})
	windows, _, valid := projectCodexUsage(payload, codexUsageTestObserved)
	if !valid || len(windows) != 4 {
		t.Fatalf("unexpected inventory: %#v", windows)
	}
	encoded, err := json.Marshal(windows)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "PRIVATE_") {
		t.Fatalf("raw payload identity leaked: %s", encoded)
	}
	var decoded []map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"id": true, "label": true, "pool": true, "remaining_percent": true, "used_percent": true, "reset_at_ms": true, "window_minutes": true, "observed_at_ms": true}
	for _, window := range decoded {
		for field := range window {
			if !allowed[field] {
				t.Fatalf("unexpected public field %q", field)
			}
		}
	}
}
