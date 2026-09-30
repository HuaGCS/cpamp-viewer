package httpapi

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

var codexQuotaTestTime = time.Date(2030, 9, 9, 12, 0, 0, 0, time.UTC)

func codexQuotaTestFile() map[string]any {
	return map[string]any{
		"provider": "codex", "type": "codex", "name": "private-credential.json",
		"auth_index": "private-auth-index", "email": "shared-account@example.test",
		"id_token": map[string]any{"plan_type": "pro"},
	}
}

func codexQuotaTestObservation(at time.Time, values map[string]string) map[string]any {
	signals := make(map[string]any, len(values))
	for key, value := range values {
		signals[key] = value
	}
	return map[string]any{"observed_at": at.Format(time.RFC3339), "signals": signals}
}

func codexQuotaTestSnapshot(at time.Time, primary map[string]any) map[string]any {
	return map[string]any{
		"event_hash": "private-event-id", "timestamp_ms": at.UnixMilli(),
		"auth_file_snapshot": "private-credential.json", "auth_index": "private-auth-index",
		"account_snapshot": "shared-account@example.test", "auth_provider_snapshot": "codex",
		"model": "gpt-5.3-codex", "resolved_model": "gpt-5.3-codex",
		"response_metadata": map[string]any{"quota": map[string]any{
			"plan_type": "pro", "primary": primary,
		}},
	}
}

func codexQuotaTestFind(t *testing.T, windows []quotaWindow, pool string, minutes float64) quotaWindow {
	t.Helper()
	var match quotaWindow
	found := 0
	for _, window := range windows {
		if window.Pool == pool && window.WindowMins == minutes {
			match = window
			found++
		}
	}
	if found != 1 {
		t.Fatalf("pool=%s duration=%v has %d windows, want exactly one: %#v", pool, minutes, found, windows)
	}
	return match
}

func codexQuotaTestRemaining(t *testing.T, window quotaWindow, remaining float64) {
	t.Helper()
	if math.Abs(window.Remaining-remaining) > 1e-9 || math.Abs(window.Used-(100-remaining)) > 1e-9 {
		t.Fatalf("remaining/used=%v/%v, want %v/%v", window.Remaining, window.Used, remaining, 100-remaining)
	}
}

func codexQuotaTestFullSignals() map[string]string {
	return map[string]string{
		"X-Codex-Plan-Type":            "pro",
		"X-Codex-Primary-Used-Percent": "48", "X-Codex-Primary-Window-Minutes": "10080",
		"X-Codex-Primary-Reset-After-Seconds":                                  "3600",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Limit-Name":                    "GPT-5.3-Codex-Spark",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Primary-Used-Percent":          "3",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Primary-Window-Minutes":        "300",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Primary-Reset-After-Seconds":   "1200",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Secondary-Used-Percent":        "63",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Secondary-Window-Minutes":      "10080",
		"X-Codex-Additional-GPT-5.3-Codex-Spark-Secondary-Reset-After-Seconds": "7200",
	}
}

func TestCodexQuotaKeepsMainWeeklyAndSparkWindowsSeparate(t *testing.T) {
	file := codexQuotaTestFile()
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, codexQuotaTestFullSignals())
	windows, plan, observedAt := projectCodexQuota(file, nil)
	if len(windows) != 3 || plan != "pro" || observedAt != codexQuotaTestTime.UnixMilli() {
		t.Fatalf("unexpected inventory: windows=%#v plan=%q observed=%d", windows, plan, observedAt)
	}
	main := codexQuotaTestFind(t, windows, "codex_main", 10080)
	sparkFiveHour := codexQuotaTestFind(t, windows, "codex_spark", 300)
	sparkWeekly := codexQuotaTestFind(t, windows, "codex_spark", 10080)
	codexQuotaTestRemaining(t, main, 52)
	codexQuotaTestRemaining(t, sparkFiveHour, 97)
	codexQuotaTestRemaining(t, sparkWeekly, 37)
	if main.ResetAtMS != codexQuotaTestTime.Add(time.Hour).UnixMilli() || sparkFiveHour.ResetAtMS != codexQuotaTestTime.Add(20*time.Minute).UnixMilli() || sparkWeekly.ResetAtMS != codexQuotaTestTime.Add(2*time.Hour).UnixMilli() {
		t.Fatalf("window reset times were mixed: %#v", windows)
	}
	ids := map[string]bool{}
	for _, window := range windows {
		if window.ID == "" || ids[window.ID] {
			t.Fatalf("distinct pools/windows share an ID: %#v", windows)
		}
		if !strings.HasPrefix(window.ID, window.Pool+"-") {
			t.Fatalf("window ID does not include its confirmed pool identity: %#v", window)
		}
		ids[window.ID] = true
	}
}

func TestCodexQuotaWindowDurationOverridesPrimarySecondaryPosition(t *testing.T) {
	file := codexQuotaTestFile()
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
		"x-codex-primary-used-percent": "10", "x-codex-primary-window-minutes": "10080",
		"x-codex-secondary-used-percent": "30", "x-codex-secondary-window-minutes": "300",
	})
	windows, _, _ := projectCodexQuota(file, nil)
	if len(windows) != 2 {
		t.Fatalf("unexpected windows: %#v", windows)
	}
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "codex_main", 10080), 90)
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "codex_main", 300), 70)
}

func TestCodexQuotaFlattenedActiveSparkDoesNotProveRootMainPool(t *testing.T) {
	file := codexQuotaTestFile()
	signals := codexQuotaTestFullSignals()
	signals["x-codex-active-limit"] = "codex_bengalfox"
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, signals)
	windows, _, _ := projectCodexQuota(file, nil)
	if len(windows) != 3 {
		t.Fatalf("active Spark changed the WS pool inventory: %#v", windows)
	}
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "unknown", 10080), 52)
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "codex_spark", 300), 97)
}

func TestCodexQuotaModelObservationKeyIsNotAPoolIdentity(t *testing.T) {
	file := codexQuotaTestFile()
	file["model_quotas"] = map[string]any{
		"gpt-5.3-codex-spark": codexQuotaTestObservation(codexQuotaTestTime, codexQuotaTestFullSignals()),
	}
	windows, _, _ := projectCodexQuota(file, nil)
	if len(windows) != 3 {
		t.Fatalf("model-scoped copy changed the underlying header namespaces: %#v", windows)
	}
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "codex_main", 10080), 52)
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "codex_spark", 10080), 37)
}

func TestCodexQuotaNewestPoolObservationReplacesItsWholeWindowInventory(t *testing.T) {
	older := codexQuotaTestTime.Add(-2 * time.Hour)
	oldSignals := codexQuotaTestFullSignals()
	oldSignals["X-Codex-Primary-Window-Minutes"] = "300"
	oldSignals["X-Codex-Primary-Used-Percent"] = "70"
	oldSignals["X-Codex-Secondary-Window-Minutes"] = "10080"
	oldSignals["X-Codex-Secondary-Used-Percent"] = "80"
	file := codexQuotaTestFile()
	file["model_quotas"] = map[string]any{
		"gpt-5.3-codex-spark": codexQuotaTestObservation(older, oldSignals),
	}
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
		"x-codex-primary-used-percent": "40", "x-codex-primary-window-minutes": "10080",
		"x-codex-primary-reset-after-seconds": "1800",
	})
	windows, _, observedAt := projectCodexQuota(file, nil)
	if len(windows) != 3 || observedAt != codexQuotaTestTime.UnixMilli() {
		t.Fatalf("older main window was revived or independent Spark evidence lost: %#v", windows)
	}
	main := codexQuotaTestFind(t, windows, "codex_main", 10080)
	codexQuotaTestRemaining(t, main, 60)
	if main.ObservedAt != codexQuotaTestTime.UnixMilli() || main.ResetAtMS != codexQuotaTestTime.Add(30*time.Minute).UnixMilli() {
		t.Fatalf("latest main observation mixed with old fields: %#v", main)
	}
	for _, window := range windows {
		if window.Pool == "codex_main" && window.WindowMins == 300 {
			t.Fatalf("older 5H window was spliced into newer weekly-only main observation: %#v", windows)
		}
	}
	spark := codexQuotaTestFind(t, windows, "codex_spark", 300)
	codexQuotaTestRemaining(t, spark, 97)
	if spark.ObservedAt != older.UnixMilli() || spark.ResetAtMS != older.Add(20*time.Minute).UnixMilli() {
		t.Fatalf("independent older Spark timestamp was replaced with latest account time: %#v", spark)
	}
}

func TestCodexQuotaHTTPNamespaceAndExplicitLimitNamePrecedence(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		limitName string
		pool      string
	}{
		{"named HTTP Spark", "bengalfox", "GPT-5.3-Codex-Spark", "codex_spark"},
		{"known namespace without name", "bengalfox", "", "codex_spark"},
		{"known alias without name", "codex-bengalfox", "", "codex_spark"},
		{"unknown namespace with exact Spark name", "some-other-namespace", "GPT-5.3-Codex-Spark", "codex_spark"},
		{"explicit conflicting name", "bengalfox", "Other feature", "unknown"},
		{"Spark substring is insufficient", "some-other-namespace", "Not really Spark usage", "unknown"},
		{"code review remains separate", "code-review", "", "codex_review"},
		{"explicit name conflicts with code review namespace", "code-review", "Other feature", "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prefix := "x-codex-" + test.namespace + "-"
			signals := map[string]string{prefix + "primary-used-percent": "20", prefix + "primary-window-minutes": "300"}
			if test.limitName != "" {
				signals[prefix+"limit-name"] = test.limitName
			}
			file := codexQuotaTestFile()
			file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, signals)
			windows, _, _ := projectCodexQuota(file, nil)
			if len(windows) != 1 {
				t.Fatalf("unexpected inventory for namespace: %#v", windows)
			}
			codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, test.pool, 300), 80)
		})
	}
}

func TestCodexQuotaNonWebsocketActiveAdditionalLimitLeavesRootUnconfirmed(t *testing.T) {
	file := codexQuotaTestFile()
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
		"x-codex-active-limit":         "codex_bengalfox",
		"x-codex-primary-used-percent": "40", "x-codex-primary-window-minutes": "10080",
		"x-codex-bengalfox-limit-name":           "GPT-5.3-Codex-Spark",
		"x-codex-bengalfox-primary-used-percent": "3", "x-codex-bengalfox-primary-window-minutes": "300",
	})
	windows, _, _ := projectCodexQuota(file, nil)
	if len(windows) != 2 {
		t.Fatalf("unexpected non-WS inventory: %#v", windows)
	}
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "unknown", 10080), 60)
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "codex_spark", 300), 97)
}

func TestCodexQuotaMissingOrMalformedUsageNeverBecomesFullQuota(t *testing.T) {
	for _, value := range []string{"", "not-a-number", "NaN", "+Inf", "-1", "101"} {
		t.Run("used="+value, func(t *testing.T) {
			signals := map[string]string{"x-codex-primary-window-minutes": "300", "x-codex-primary-reset-after-seconds": "3600"}
			if value != "" {
				signals["x-codex-primary-used-percent"] = value
			}
			file := codexQuotaTestFile()
			file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, signals)
			windows, _, _ := projectCodexQuota(file, nil)
			if len(windows) != 0 {
				t.Fatalf("absent/invalid usage became a public measured window: %#v", windows)
			}
		})
	}
	file := codexQuotaTestFile()
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
		"x-codex-primary-used-percent": "0", "x-codex-primary-window-minutes": "300",
	})
	windows, _, _ := projectCodexQuota(file, nil)
	if len(windows) != 1 {
		t.Fatalf("an explicitly measured zero percent was lost: %#v", windows)
	}
	window := codexQuotaTestFind(t, windows, "codex_main", 300)
	codexQuotaTestRemaining(t, window, 100)
	if window.ResetAtMS != 0 {
		t.Fatalf("missing reset manufactured an immediate reset timestamp: %#v", window)
	}
}

func TestCodexQuotaMissingDurationDoesNotInventFiveHourOrWeekly(t *testing.T) {
	for _, position := range []string{"primary", "secondary"} {
		t.Run(position, func(t *testing.T) {
			file := codexQuotaTestFile()
			file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
				"x-codex-" + position + "-used-percent": "25",
			})
			windows, _, _ := projectCodexQuota(file, nil)
			for _, window := range windows {
				if window.WindowMins != 0 || strings.Contains(window.Label, "5 小时") || strings.Contains(window.Label, "7 天") || strings.Contains(window.Label, "5H") || strings.Contains(window.Label, "7D") || window.ResetAtMS != 0 {
					t.Fatalf("source position invented a duration/reset: %#v", window)
				}
			}
		})
	}
}

func TestCodexQuotaMalformedDurationRemainsSerializableAndUnconfirmed(t *testing.T) {
	for _, duration := range []string{"NaN", "+Inf", "-Inf", "not-a-number", "-300"} {
		t.Run(duration, func(t *testing.T) {
			file := codexQuotaTestFile()
			file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
				"x-codex-active-limit":         "main",
				"x-codex-primary-used-percent": "25", "x-codex-primary-window-minutes": duration,
			})
			windows, _, _ := projectCodexQuota(file, nil)
			if len(windows) != 1 || windows[0].Pool != "codex_main" || windows[0].WindowMins != 0 {
				t.Fatalf("invalid duration manufactured a known window: %#v", windows)
			}
			if _, err := json.Marshal(windows); err != nil {
				t.Fatalf("invalid source duration broke the quota response: %v", err)
			}
		})
	}
}

func TestCodexQuotaAbsoluteResetWinsOverRelativeAndKeepsObservationTime(t *testing.T) {
	reset := codexQuotaTestTime.Add(3 * time.Hour)
	file := codexQuotaTestFile()
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
		"x-codex-primary-used-percent": "25", "x-codex-primary-window-minutes": "300",
		"x-codex-primary-reset-at":            strconv.FormatInt(reset.Unix(), 10),
		"x-codex-primary-reset-after-seconds": "120",
	})
	windows, _, _ := projectCodexQuota(file, nil)
	window := codexQuotaTestFind(t, windows, "codex_main", 300)
	if window.ResetAtMS != reset.UnixMilli() || window.ObservedAt != codexQuotaTestTime.UnixMilli() {
		t.Fatalf("absolute reset was replaced by relative/current clock: %#v", window)
	}
}

func TestCodexQuotaRuntimeObservationPreventsHistoricalHeaderMixing(t *testing.T) {
	file := codexQuotaTestFile()
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
		"x-codex-primary-used-percent": "40", "x-codex-primary-window-minutes": "10080",
	})
	header := codexQuotaTestSnapshot(codexQuotaTestTime.Add(time.Hour), map[string]any{
		"used_percent": float64(90), "window_minutes": float64(300),
	})
	windows, _, _ := projectCodexQuota(file, []map[string]any{header})
	if len(windows) != 1 {
		t.Fatalf("header fallback was merged into existing runtime inventory: %#v", windows)
	}
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "codex_main", 10080), 60)
}

func TestCodexQuotaHeaderFallbackRejectsCredentialAndProviderConflicts(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"same email different credential", func(snapshot map[string]any) {
			snapshot["auth_file_snapshot"] = "different.json"
			snapshot["auth_index"] = "different-auth"
		}},
		{"same file conflicting auth index", func(snapshot map[string]any) { snapshot["auth_index"] = "different-auth" }},
		{"same auth index different provider", func(snapshot map[string]any) { snapshot["auth_provider_snapshot"] = "claude" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := codexQuotaTestFile()
			conflict := codexQuotaTestSnapshot(codexQuotaTestTime.Add(time.Hour), map[string]any{
				"used_percent": float64(1), "window_minutes": float64(300),
			})
			test.edit(conflict)
			windows, _, _ := projectCodexQuota(file, []map[string]any{conflict})
			if len(windows) != 0 {
				t.Fatalf("another credential/provider supplied the quota: %#v", windows)
			}
			matching := codexQuotaTestSnapshot(codexQuotaTestTime, map[string]any{
				"used_percent": float64(60), "window_minutes": float64(10080),
				"reset_after_seconds": float64(3600),
			})
			windows, _, observed := projectCodexQuota(file, []map[string]any{conflict, matching})
			if len(windows) != 1 || observed != codexQuotaTestTime.UnixMilli() {
				t.Fatalf("newer conflicting snapshot obscured the matching credential: %#v", windows)
			}
			window := codexQuotaTestFind(t, windows, "codex_main", 10080)
			codexQuotaTestRemaining(t, window, 40)
			if window.ResetAtMS != codexQuotaTestTime.Add(time.Hour).UnixMilli() {
				t.Fatalf("matching snapshot did not anchor its own reset: %#v", window)
			}
		})
	}
}

func TestCodexQuotaSafeWindowDTONeverContainsRawCredentialOrHeaderData(t *testing.T) {
	file := codexQuotaTestFile()
	signals := codexQuotaTestFullSignals()
	signals["Authorization"] = "Bearer PRIVATE_TOKEN_SENTINEL"
	signals["X-Private-Account"] = "PRIVATE_ACCOUNT_SENTINEL"
	signals["X-Codex-Unrecognized-Limit-Name"] = "PRIVATE_LIMIT_NAME_SENTINEL"
	signals["X-Codex-Unrecognized-Primary-Used-Percent"] = "5"
	signals["X-Codex-Unrecognized-Primary-Window-Minutes"] = "300"
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, signals)
	windows, _, _ := projectCodexQuota(file, nil)
	encoded, err := json.Marshal(windows)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE_", "private-credential", "private-auth-index", "shared-account@example.test", "Authorization", "observed_at\"", "signals", "model_quotas", "x-codex-"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("public quota DTO contains forbidden credential/header data %q: %s", secret, encoded)
		}
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &items); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"id": true, "label": true, "pool": true, "remaining_percent": true, "used_percent": true, "reset_at_ms": true, "window_minutes": true, "observed_at_ms": true}
	for _, item := range items {
		for field := range item {
			if !allowed[field] {
				t.Fatalf("unexpected public window field %q", field)
			}
		}
	}
}

func TestCodexQuotaPremiumFallbackKeepsWeeklyAndDropsZeroSlot(t *testing.T) {
	file := codexQuotaTestFile()
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
		"x-codex-active-limit":         "premium",
		"x-codex-primary-used-percent": "43", "x-codex-primary-window-minutes": "10080",
		"x-codex-secondary-used-percent": "0", "x-codex-secondary-window-minutes": "0", "x-codex-secondary-reset-after-seconds": "0",
		"x-codex-bengalfox-primary-used-percent": "0", "x-codex-bengalfox-primary-window-minutes": "300",
		"x-codex-bengalfox-secondary-used-percent": "0", "x-codex-bengalfox-secondary-window-minutes": "10080",
	})
	windows, _, _ := projectCodexQuota(file, nil)
	if len(windows) != 3 {
		t.Fatalf("empty secondary slot or unknown duplicate survived: %#v", windows)
	}
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "codex_main", 10080), 57)
	codexQuotaTestRemaining(t, codexQuotaTestFind(t, windows, "codex_spark", 300), 100)
}

func TestCodexQuotaAmbiguousSparkMirrorDoesNotDuplicateNamedPool(t *testing.T) {
	file := codexQuotaTestFile()
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
		"x-codex-active-limit":         "codex_bengalfox",
		"x-codex-primary-used-percent": "30", "x-codex-primary-window-minutes": "300",
		"x-codex-secondary-used-percent": "19", "x-codex-secondary-window-minutes": "10080",
		"x-codex-bengalfox-primary-used-percent": "30", "x-codex-bengalfox-primary-window-minutes": "300",
		"x-codex-bengalfox-secondary-used-percent": "19", "x-codex-bengalfox-secondary-window-minutes": "10080",
	})
	windows, _, _ := projectCodexQuota(file, nil)
	if len(windows) != 2 {
		t.Fatalf("ambiguous mirror was counted twice: %#v", windows)
	}
	for _, w := range windows {
		if w.Pool != "codex_spark" {
			t.Fatalf("mirror invented a main pool: %#v", windows)
		}
	}
}

func TestCodexQuotaEqualUnknownFeatureIsNotAnActivePoolMirror(t *testing.T) {
	file := codexQuotaTestFile()
	file["quota"] = codexQuotaTestObservation(codexQuotaTestTime, map[string]string{
		"x-codex-bengalfox-primary-used-percent": "0", "x-codex-bengalfox-primary-window-minutes": "300",
		"x-codex-future-primary-used-percent": "0", "x-codex-future-primary-window-minutes": "300",
	})
	windows, _, _ := projectCodexQuota(file, nil)
	if len(windows) != 2 {
		t.Fatalf("coincidentally equal unknown feature was deleted: %#v", windows)
	}
	codexQuotaTestFind(t, windows, "unknown", 300)
	codexQuotaTestFind(t, windows, "codex_spark", 300)
}
