package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cpamp-viewer/server/internal/auth"
	"cpamp-viewer/server/internal/config"
	"cpamp-viewer/server/internal/cpamp"
)

func testConfig() config.Config {
	return config.Config{
		MaxAnalyticsRange: 365 * 24 * time.Hour,
		MaxEventsPage:     200,
		SessionSecret:     []byte("test-session-secret-at-least-32-bytes"),
	}
}

func floatPointer(value float64) *float64 {
	return &value
}

func TestEventsCursorRoundTripAndTamperRejection(t *testing.T) {
	server := &Server{cfg: testConfig()}
	want := eventsCursor{BeforeMS: 1_700_000_000_000, BeforeID: 99}
	token := server.encodeEventsCursor(want)
	if token == "" {
		t.Fatal("cursor token is empty")
	}
	got, err := server.decodeEventsCursor(token)
	if err != nil || got != want {
		t.Fatalf("decode cursor = %#v, %v", got, err)
	}
	payload, _ := json.Marshal(want)
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(sealed, payload) || bytes.Contains(sealed, []byte(`"i":99`)) {
		t.Fatalf("cursor exposes plaintext payload: %q", sealed)
	}
	if second := server.encodeEventsCursor(want); second == token {
		t.Fatal("cursor encryption reused a nonce")
	}
	tampered := token[:len(token)-1] + "A"
	if tampered == token {
		tampered = token[:len(token)-1] + "B"
	}
	if _, err := server.decodeEventsCursor(tampered); err == nil {
		t.Fatal("tampered cursor was accepted")
	}
}

func TestAttachAnalyticsCursorRejectsUpstreamCursorInjection(t *testing.T) {
	server := &Server{cfg: testConfig()}
	response := map[string]any{
		"events": map[string]any{
			"items":       []any{},
			"has_more":    false,
			"next_cursor": strings.Repeat("A", 64),
		},
		"drilldown_preview": map[string]any{
			"items":       []any{},
			"has_more":    false,
			"next_cursor": strings.Repeat("B", 64),
		},
	}
	server.attachAnalyticsCursor(response)
	projectAnalytics(response)
	for _, key := range []string{"events", "drilldown_preview"} {
		container := response[key].(map[string]any)
		if _, exists := container["next_cursor"]; exists {
			t.Fatalf("untrusted upstream cursor survived in %s: %#v", key, container)
		}
	}
}

func TestSecurityHeadersDisableActiveEmbedding(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://viewer.test/", nil))
	csp := response.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"script-src 'self'", "object-src 'none'", "frame-src 'none'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Fatalf("CSP missing %q: %s", directive, csp)
		}
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("CSP permits inline scripts: %s", csp)
	}
}

func TestPublicAccessSessionNeedsNoPasswordOrCookie(t *testing.T) {
	server := &Server{
		cfg:  config.Config{PublicAccess: true},
		auth: auth.New("", []byte("test-session-secret-at-least-32-bytes"), time.Hour, true),
	}
	response := httptest.NewRecorder()
	server.handleSession(response, httptest.NewRequest(http.MethodGet, "http://viewer.test/viewer/api/v1/session", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("session status = %d", response.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["authenticated"] != true || payload["public_access"] != true {
		t.Fatalf("unexpected public session: %#v", payload)
	}
	if _, exists := payload["csrf_token"]; exists {
		t.Fatalf("public session exposed unnecessary CSRF material: %#v", payload)
	}
}

func TestPublicAccessAllowsReadOnlyAPIWithoutSession(t *testing.T) {
	server := &Server{cfg: config.Config{PublicAccess: true}}
	called := false
	handler := server.withSession(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	response := httptest.NewRecorder()
	handler(response, httptest.NewRequest(http.MethodGet, "http://viewer.test/viewer/api/v1/dashboard", nil))
	if !called || response.Code != http.StatusNoContent {
		t.Fatalf("public GET was not allowed: called=%v status=%d", called, response.Code)
	}
}

func TestPublicAccessRejectsCrossOriginQueryPost(t *testing.T) {
	server := &Server{cfg: config.Config{PublicAccess: true}}
	called := false
	handler := server.withSession(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "http://viewer.test/viewer/api/v1/analytics", strings.NewReader("{}"))
	request.Header.Set("Origin", "https://attacker.test")
	response := httptest.NewRecorder()
	handler(response, request)
	if called || response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST was accepted: called=%v status=%d", called, response.Code)
	}
}

func TestValidateAnalyticsExpandsOpaqueEventsCursor(t *testing.T) {
	server := &Server{cfg: testConfig()}
	token := server.encodeEventsCursor(eventsCursor{BeforeMS: 1_700_000_000_000, BeforeID: 77})
	payload := map[string]any{
		"from_ms": json.Number("1699900000000"),
		"to_ms":   json.Number("1700100000000"),
		"include": map[string]any{"events_page": map[string]any{
			"limit": json.Number("50"), "cursor": token,
		}},
	}
	if err := server.validateAnalytics(payload); err != nil {
		t.Fatal(err)
	}
	page := payload["include"].(map[string]any)["events_page"].(map[string]any)
	if _, exists := page["cursor"]; exists || intValue(page["before_ms"]) != 1_700_000_000_000 || intValue(page["before_id"]) != 77 {
		t.Fatalf("opaque cursor was not safely expanded: %#v", page)
	}
}

func TestValidateAnalyticsRejectsUnsafeShape(t *testing.T) {
	server := &Server{cfg: testConfig()}
	valid := map[string]any{
		"from_ms": json.Number("1700000000000"),
		"to_ms":   json.Number("1700086400000"),
		"filters": map[string]any{"api_key_ids": []any{"view_0123456789ab"}},
		"include": map[string]any{
			"summary": true, "summary_profile": "compact", "summary_percentiles": true,
			"summary_comparison": true, "timeline": true,
			"hourly_distribution": true, "model_share": true, "channel_share": true,
			"failure_sources": true, "credential_stats": true, "credential_timeline": true,
			"api_key_timeline": true,
			"filter_selectors": true, "task_buckets": true,
			"events_page": map[string]any{"limit": json.Number("500")},
			"drilldown_preview": map[string]any{
				"from_ms": json.Number("1700000000000"),
				"to_ms":   json.Number("1700003600000"),
				"limit":   json.Number("12"),
			},
			"granularity": "hour",
		},
	}
	if err := server.validateAnalytics(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	page := valid["include"].(map[string]any)["events_page"].(map[string]any)
	if got := intValue(page["limit"]); got != 200 {
		t.Fatalf("500-row original request was not safely clamped: %d", got)
	}

	cases := []map[string]any{
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "admin": true},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "search_api_key_hash": "short"},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "include": map[string]any{"events_page": map[string]any{"limit": json.Number("501")}}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "include": map[string]any{"events_page": map[string]any{"before_id": json.Number("1")}}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "include": map[string]any{"summary": "yes"}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "include": map[string]any{"summary_profile": "tiny"}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "filters": map[string]any{"api_key_ids": []any{strings.Repeat("a", 64)}}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "filters": map[string]any{"accounts": []any{"private-account"}}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "filters": map[string]any{"auth_files": []any{"credential.json"}}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "filters": map[string]any{"credential_ids": []any{"credential.json"}}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "filters": map[string]any{"project_ids": []any{"private-project"}}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1700086400000"), "filters": map[string]any{"source_hashes": []any{"private-source"}}},
		{"from_ms": json.Number("1700000000000"), "to_ms": json.Number("1800000000000")},
	}
	for index, payload := range cases {
		if err := server.validateAnalytics(payload); err == nil {
			t.Fatalf("case %d was accepted", index)
		}
	}
}

func TestProjectAnalyticsSupportsV112TimelinesAndSafeModelNames(t *testing.T) {
	rawHash := strings.Repeat("e", 64)
	response := map[string]any{
		"api_key_timeline": []any{map[string]any{
			"api_key_hash": rawHash, "bucket_ms": json.Number("1700000000000"),
			"bucket_label": "08:00", "calls": json.Number("2"), "total_tokens": json.Number("14"),
		}},
		"filter_selectors": map[string]any{
			"account_count": json.Number("7"), "api_key_count": json.Number("3"),
		},
		"events": map[string]any{
			"items": []any{map[string]any{
				"event_hash": "event-v112", "timestamp_ms": json.Number("1700000000000"),
				"model": "legacy-model", "analytics_model": "gpt-safe",
				"requested_model": "gpt-safe(high)", "client_ip": "192.0.2.1",
				"x_forwarded_for": "198.51.100.7", "user_agent": "secret-agent",
				"api_key_hash": rawHash,
			}},
			"has_more": false,
		},
	}
	aliases := apiKeyAliasEnvelope{Items: []apiKeyAlias{{APIKeyHash: rawHash, Alias: "鸡哥"}}}
	attachAnalyticsAliases(response, aliases)
	projectAnalytics(response)
	encoded, _ := json.Marshal(response)
	text := string(encoded)
	for _, expected := range []string{"api_key_timeline", "view_", "鸡哥", "gpt-safe", "gpt-safe(high)", "account_count", "api_key_count"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("v1.12 field %q missing: %s", expected, text)
		}
	}
	for _, secret := range []string{rawHash, "192.0.2.1", "198.51.100.7", "secret-agent", "client_ip", "x_forwarded_for", "user_agent"} {
		if strings.Contains(text, secret) {
			t.Fatalf("v1.12 projection leaked %q: %s", secret, text)
		}
	}
}

func TestCredentialIdentityFilterResolvesOnlyKnownViewerID(t *testing.T) {
	index := newAnalyticsIdentityIndex(apiKeyAliasEnvelope{})
	collectAnalyticsCredentialIdentityIndex(index, []any{map[string]any{"id": "credential-a.json"}})
	viewerID := pseudonym("credential-a.json")
	payload := map[string]any{"filters": map[string]any{"credential_ids": []any{viewerID}}}
	if err := resolveAnalyticsIdentityFilters(payload, index); err != nil {
		t.Fatal(err)
	}
	values := payload["filters"].(map[string]any)["credential_ids"].([]any)
	if len(values) != 1 || values[0] != "credential-a.json" {
		t.Fatalf("credential filter was not resolved: %#v", payload)
	}
	unknown := map[string]any{"filters": map[string]any{"credential_ids": []any{"view_000000000000"}}}
	if err := resolveAnalyticsIdentityFilters(unknown, index); err == nil {
		t.Fatal("unknown credential Viewer ID was accepted")
	}
}

func TestProjectAnalyticsRemovesSecretsRecursively(t *testing.T) {
	hash := strings.Repeat("a", 64)
	response := map[string]any{
		"generated_at_ms": json.Number("1800000000000"),
		"filter_options": map[string]any{
			"models":           []any{"safe-model"},
			"api_key_hashes":   []any{hash},
			"auth_files":       []any{"credential-1.json"},
			"header_trace_ids": []any{"trace-secret"},
		},
		"events": map[string]any{
			"items": []any{map[string]any{
				"event_hash":         "event-secret",
				"api_key_hash":       hash,
				"api_key_id":         "attacker-controlled-id",
				"auth_index":         "sensitive-index",
				"auth_file_snapshot": "credential-1.json",
				"auth_file_body":     `{"access_token":"secret-token"}`,
				"source_hash":        "source-secret",
				"header_trace_id":    "trace-secret",
				"response_metadata":  map[string]any{"trace": map[string]any{"request_id": "secret"}},
				"endpoint":           "https://upstream.example/v1/responses?token=secret-query",
				"fail_summary":       "Authorization: Bearer secret-token",
				"unknown_field":      "must-never-pass",
			}},
			"has_more": false,
		},
		"unknown_root": map[string]any{"credential": "secret-root"},
	}
	projectAnalytics(response)
	encoded, _ := json.Marshal(response)
	text := string(encoded)
	for _, secret := range []string{hash, "sensitive-index", "source-secret", "trace-secret", "secret-token", "attacker-controlled-id", "must-never-pass", "secret-root", "secret-query"} {
		if strings.Contains(text, secret) {
			t.Fatalf("projected response leaked %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "view_") || !strings.Contains(text, "敏感错误详情已隐藏") || !strings.Contains(text, `"endpoint":"/v1/responses"`) || !strings.Contains(text, "auth_file_id") || !strings.Contains(text, "auth_file_display") {
		t.Fatalf("expected pseudonym, redaction, and safe path: %s", text)
	}
	if strings.Contains(text, "api_key_hash") || strings.Contains(text, "header_trace") || strings.Contains(text, "auth_file_body") || strings.Contains(text, "auth_file_snapshot") {
		t.Fatalf("sensitive analytics field names survived projection: %s", text)
	}
}

func TestProjectAnalyticsRejectsAPIKeyTypeBypass(t *testing.T) {
	response := map[string]any{
		"api_key_stats": []any{map[string]any{
			"id":           "row-id",
			"api_key_hash": map[string]any{"raw": strings.Repeat("b", 64)},
			"calls":        json.Number("3"),
		}},
		"filter_options": map[string]any{
			"models":     []any{"safe-model"},
			"providers":  []any{"codex"},
			"unexpected": map[string]any{"api_key_hashes": strings.Repeat("c", 64)},
		},
	}
	projectAnalytics(response)
	encoded, _ := json.Marshal(response)
	text := string(encoded)
	if strings.Contains(text, strings.Repeat("b", 64)) || strings.Contains(text, strings.Repeat("c", 64)) || strings.Contains(text, "api_key_hash") {
		t.Fatalf("non-string API key material passed projection: %s", text)
	}
	if !strings.Contains(text, "safe-model") || !strings.Contains(text, "codex") {
		t.Fatalf("safe filter options were removed: %s", text)
	}
}

func TestProjectAnalyticsSupportsOriginalV111ShapeWithoutSensitiveMaterial(t *testing.T) {
	rawHash := strings.Repeat("d", 64)
	response := map[string]any{
		"summary_comparison": map[string]any{
			"from_ms": json.Number("1699913600000"), "to_ms": json.Number("1700000000000"),
			"total_calls": json.Number("9"), "success_calls": json.Number("8"),
			"failure_calls": json.Number("1"), "success_rate": json.Number("0.88"),
			"total_tokens": json.Number("1234"), "total_cost": json.Number("0.12"),
		},
		"timeline": []any{map[string]any{
			"bucket_ms": json.Number("1700000000000"), "bucket_end_ms": json.Number("1700003600000"),
			"calls": json.Number("2"), "input_tokens": json.Number("10"),
			"output_tokens": json.Number("4"), "cache_hit_rate": json.Number("0.5"),
			"p95_latency_ms": json.Number("80"), "failure_rate": json.Number("0.5"),
		}},
		"hourly_distribution": []any{map[string]any{"hour": json.Number("8"), "calls": json.Number("2"), "tokens": json.Number("14")}},
		"model_share":         []any{map[string]any{"model": "gpt-safe", "calls": json.Number("2"), "tokens": json.Number("14"), "cost": json.Number("0.01")}},
		"channel_share": []any{map[string]any{
			"auth_index": "auth-index-secret", "source": "codex", "account_snapshot": "safe@example.test",
			"calls": json.Number("2"), "success": json.Number("1"), "failure": json.Number("1"),
			"tokens": json.Number("14"), "cost": json.Number("0.01"), "average_latency_ms": json.Number("90"),
		}},
		"failure_sources": []any{map[string]any{
			"source": "codex", "source_hash": "source-hash-secret", "auth_index": "auth-index-secret",
			"calls": json.Number("2"), "failure": json.Number("1"), "last_seen_ms": json.Number("1700000000000"),
		}},
		"credential_stats": []any{map[string]any{
			"id": "credential.json", "auth_file_snapshot": "credential.json", "auth_index": "auth-index-secret",
			"source": "codex", "source_hash": "source-hash-secret", "account_snapshot": "safe@example.test",
			"calls": json.Number("2"), "success_calls": json.Number("1"), "failure_calls": json.Number("1"),
			"total_tokens": json.Number("14"), "cost": json.Number("0.01"),
			"models":         []any{map[string]any{"model": "gpt-safe", "calls": json.Number("2"), "cache_read_tokens": json.Number("3")}},
			"auth_file_body": `{"refresh_token":"never-return"}`,
		}},
		"credential_timeline": []any{map[string]any{
			"id": "credential.json", "label": "safe@example.test", "auth_file_snapshot": "credential.json",
			"auth_index": "auth-index-secret", "source_hash": "source-hash-secret",
			"bucket_ms": json.Number("1700000000000"), "bucket_label": "08:00",
			"calls": json.Number("2"), "tokens": json.Number("14"), "success": json.Number("1"),
			"failure": json.Number("1"), "reasoning_tokens": json.Number("2"), "failure_rate": json.Number("0.5"),
		}},
		"filter_selectors": map[string]any{
			"models": []any{"gpt-safe"}, "api_key_hashes": []any{rawHash},
			"auth_files": []any{"credential.json"}, "header_trace_ids": []any{"trace-never-return"},
			"api_key_stats": []any{map[string]any{"id": rawHash, "api_key_hash": rawHash, "calls": json.Number("2")}},
		},
		"task_buckets": []any{map[string]any{
			"bucket_key": "bucket-secret", "source_hash": "source-hash-secret", "auth_index": "auth-index-secret",
			"models": []any{"gpt-safe"}, "endpoints": []any{"https://provider.test/v1/responses?token=never-return"},
			"total": json.Number("2"), "success": json.Number("1"), "failure": json.Number("1"),
		}},
		"heatmap": []any{map[string]any{
			"weekday": json.Number("1"), "hour": json.Number("8"), "calls": json.Number("2"),
			"api_key_contributors": []any{map[string]any{"key": rawHash, "label": rawHash, "calls": json.Number("2"), "share": json.Number("1")}},
			"model_contributors":   []any{map[string]any{"key": "gpt-safe", "calls": json.Number("2"), "share": json.Number("1")}},
		}},
		"events": map[string]any{
			"items": []any{
				analyticsEventFixture(rawHash, "event-1", 1700000003000),
				analyticsEventFixture(rawHash, "event-2", 1700000002000),
				analyticsEventFixture(rawHash, "event-3", 1700000001000),
			},
			"has_more": false, "next_before_ms": json.Number("1700000000000"),
			"next_before_id": json.Number("99"), "next_cursor": strings.Repeat("A", 64),
			"total_count": json.Number("3"),
		},
		"drilldown_preview": map[string]any{
			"items":    []any{analyticsEventFixture(rawHash, "preview-1", 1700000000000)},
			"has_more": false, "next_before_ms": json.Number("0"),
			"next_before_id": json.Number("77"), "total_count": json.Number("1"),
		},
	}
	aliases := apiKeyAliasEnvelope{Items: []apiKeyAlias{{APIKeyHash: rawHash, Alias: "马哥"}}}
	attachAnalyticsAliases(response, aliases)
	projectAnalyticsWithLimit(response, 2)

	encoded, _ := json.Marshal(response)
	text := string(encoded)
	for _, secret := range []string{
		rawHash, "auth-index-secret", "source-hash-secret", "never-return", "trace-never-return",
		"response_metadata", "header_trace_id",
		"auth_file_body", "auth_file_snapshot", "auth_project_id_snapshot", "account_snapshot",
		"source_hash", "api_key_hash",
	} {
		if strings.Contains(text, secret) {
			t.Fatalf("v1.11 analytics projection leaked %q: %s", secret, text)
		}
	}
	for _, required := range []string{
		"summary_comparison", "hourly_distribution", "model_share", "channel_share",
		"failure_sources", "credential_stats", "credential_timeline", "filter_selectors",
		"task_buckets", "drilldown_preview", "api_key_id", "api_key_ids", "auth_file_id", "auth_file_display",
		"account_id", "account_display", "auth_id", "source_id", "safe@example.test", "马哥",
		`"endpoint":"/v1/responses"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("v1.11 analytics field %q was not projected: %s", required, text)
		}
	}
	events := response["events"].(map[string]any)
	items := events["items"].([]any)
	if len(items) != 2 || events["has_more"] != true || intValue(events["next_before_ms"]) != 1700000002000 {
		t.Fatalf("event safety limit was not applied correctly: %#v", events)
	}
	if _, exists := events["next_before_id"]; exists {
		t.Fatalf("unsafe cursor survived local truncation: %#v", events)
	}
	if _, exists := events["next_cursor"]; exists {
		t.Fatalf("stale opaque cursor survived local truncation: %#v", events)
	}
	preview := response["drilldown_preview"].(map[string]any)
	if _, exists := preview["next_before_id"]; exists {
		t.Fatalf("raw upstream cursor leaked through preview projection: %#v", preview)
	}
}

func TestAttachAnalyticsCursorExposesOnlyOpaqueToken(t *testing.T) {
	server := &Server{cfg: testConfig()}
	response := map[string]any{"events": map[string]any{
		"items":          []any{},
		"has_more":       true,
		"next_before_ms": json.Number("1700000000000"),
		"next_before_id": json.Number("42"),
		"total_count":    json.Number("100"),
	}}
	server.attachAnalyticsCursor(response)
	projectAnalytics(response)
	events := response["events"].(map[string]any)
	token, ok := events["next_cursor"].(string)
	if !ok || token == "" {
		t.Fatalf("opaque cursor missing: %#v", events)
	}
	if _, exists := events["next_before_id"]; exists {
		t.Fatalf("raw cursor leaked: %#v", events)
	}
	cursor, err := server.decodeEventsCursor(token)
	if err != nil || cursor.BeforeMS != 1_700_000_000_000 || cursor.BeforeID != 42 {
		t.Fatalf("decoded cursor = %#v, %v", cursor, err)
	}
}

func analyticsEventFixture(rawHash, eventHash string, timestamp int64) map[string]any {
	return map[string]any{
		"request_id": "trace-never-return", "event_hash": eventHash, "timestamp_ms": timestamp,
		"model": "gpt-safe", "resolved_model": "gpt-safe-resolved",
		"endpoint": "https://provider.test/v1/responses?token=never-return", "method": "POST", "path": "/v1/responses?secret=1",
		"auth_index": "auth-index-secret", "source": "codex", "source_hash": "source-hash-secret",
		"api_key_hash": rawHash, "account_snapshot": "safe@example.test", "auth_label_snapshot": "Safe Account",
		"auth_file_snapshot": "credential.json", "auth_provider_snapshot": "codex",
		"input_tokens": json.Number("10"), "output_tokens": json.Number("4"), "total_tokens": json.Number("14"),
		"latency_ms": json.Number("90"), "ttft_ms": json.Number("20"), "failed": false,
		"header_quota_used_percent": json.Number("20"), "header_trace_id": "trace-never-return",
		"response_metadata": map[string]any{"trace": map[string]any{"request_id": "trace-never-return"}},
	}
}

func TestValidateAnalyticsRejectsOverflowRange(t *testing.T) {
	server := &Server{cfg: testConfig()}
	payload := map[string]any{
		"from_ms": json.Number("1"),
		"to_ms":   json.Number("9223372036854775807"),
	}
	if err := server.validateAnalytics(payload); err == nil {
		t.Fatal("overflow-sized range was accepted")
	}
}

func TestCleanTextRejectsCredentialAndStackMarkers(t *testing.T) {
	for _, input := range []string{
		"credential=super-secret",
		"alias " + strings.Repeat("a", 64),
		"account Ab3Def6GhI9JkLmN0PqRsTuVwXyZ_+=/Ab3Def6GhI9JkLmN",
		"panic in C:\\src\\private\\handler.go:42",
		"Traceback (most recent call last): /srv/private/app.py:9",
	} {
		cleaned := cleanText(input, 240)
		if cleaned != "敏感错误详情已隐藏" && cleaned != "内部错误详情已隐藏" {
			t.Fatalf("unsafe text was not hidden: %q => %q", input, cleaned)
		}
	}
}

func TestQuotaSnapshotProjection(t *testing.T) {
	if windows, _, _ := projectQuotaSnapshot(nil); windows == nil || len(windows) != 0 {
		t.Fatalf("missing snapshot must return an empty array, got %#v", windows)
	}
	now := int64(1_800_000_000_000)
	snapshot := map[string]any{
		"timestamp_ms":           json.Number("1800000000000"),
		"header_quota_plan_type": "team",
		"response_metadata": map[string]any{"quota": map[string]any{
			"primary":   map[string]any{"used_percent": json.Number("8"), "window_minutes": json.Number("300"), "reset_at_ms": json.Number("1800010000000")},
			"secondary": map[string]any{"used_percent": json.Number("18"), "window_minutes": json.Number("10080")},
		}},
	}
	windows, plan, observedAt := projectQuotaSnapshot(snapshot)
	if plan != "team" || observedAt != now || len(windows) != 2 {
		t.Fatalf("unexpected projection: plan=%s observed=%d windows=%#v", plan, observedAt, windows)
	}
	if windows[0].Label != "5 小时额度" || windows[0].Remaining != 92 || windows[1].Label != "周额度" || windows[1].Remaining != 82 {
		t.Fatalf("unexpected quota windows: %#v", windows)
	}
}

func TestQuotaAccountFallsBackToCurrentCredentialPlan(t *testing.T) {
	account, ok := projectQuotaAccount(map[string]any{
		"provider":   "codex",
		"name":       "account.json",
		"auth_index": "auth-1",
		"account":    "safe@example.test",
		"status":     "ready",
		"id_token": map[string]any{
			"plan_type":               "plus",
			"chatgpt_account_id":      "must-not-be-projected",
			"chatgpt_subscription":    "private",
			"chatgpt_organization_id": "private-org",
		},
	}, nil)
	if !ok || account.Plan != "plus" {
		t.Fatalf("credential plan fallback = %#v, ok=%v", account, ok)
	}
	encoded, _ := json.Marshal(account)
	for _, secret := range []string{"must-not-be-projected", "private-org", "chatgpt_account_id"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("credential identity leaked through quota projection: %s", encoded)
		}
	}
}

func TestAliasProjectionAndFilterResolution(t *testing.T) {
	rawHash := strings.Repeat("a", 64)
	aliases := apiKeyAliasEnvelope{Items: []apiKeyAlias{{APIKeyHash: rawHash, Alias: "马哥"}}}
	id := pseudonym(rawHash)
	payload := map[string]any{
		"filters": map[string]any{"api_key_ids": []any{id}},
	}
	if err := resolveAliasFilters(payload, aliases); err != nil {
		t.Fatal(err)
	}
	values := payload["filters"].(map[string]any)["api_key_hashes"].([]any)
	if len(values) != 1 || values[0] != rawHash {
		t.Fatalf("alias filter was not resolved: %#v", values)
	}

	response := map[string]any{
		"api_key_stats": []any{map[string]any{"api_key_hash": rawHash, "calls": json.Number("2")}},
		"events":        map[string]any{"items": []any{map[string]any{"api_key_hash": rawHash, "event_hash": "event-1"}}},
	}
	attachAnalyticsAliases(response, aliases)
	projectAnalytics(response)
	encoded, _ := json.Marshal(response)
	text := string(encoded)
	if !strings.Contains(text, "马哥") || !strings.Contains(text, id) {
		t.Fatalf("alias or pseudonym missing: %s", text)
	}
	if strings.Contains(text, rawHash) {
		t.Fatalf("raw hash leaked: %s", text)
	}
	if strings.Contains(text, `"api_key_hash"`) {
		t.Fatalf("legacy api_key_hash response field leaked: %s", text)
	}
}

func TestResolveAnalyticsIdentityFiltersFromSafeDiscoveryIndex(t *testing.T) {
	rawHash := strings.Repeat("a", 64)
	index := newAnalyticsIdentityIndex(apiKeyAliasEnvelope{})
	collectAnalyticsIdentityIndex(index, map[string]any{
		"api_key_hashes": []any{rawHash},
		"auth_files":     []any{"codex-account.json"},
		"project_ids":    []any{"project-visible"},
		"account_stats": []any{map[string]any{
			"account_snapshot": "viewer@example.test",
			"auth_indices":     []any{"auth-index-private"},
			"source_hashes":    []any{"source-hash-private"},
		}},
	})
	payload := map[string]any{"filters": map[string]any{
		"api_key_ids":  []any{pseudonym(rawHash)},
		"accounts":     []any{pseudonym("viewer@example.test")},
		"auth_files":   []any{pseudonym("codex-account.json")},
		"auth_indices": []any{pseudonym("auth-index-private")},
		"source_hashes": []any{
			pseudonym("source-hash-private"),
		},
		"project_ids": []any{pseudonym("project-visible")},
	}}
	if err := resolveAnalyticsIdentityFilters(payload, index); err != nil {
		t.Fatal(err)
	}
	filters := payload["filters"].(map[string]any)
	for key, want := range map[string]string{
		"api_key_hashes": rawHash,
		"accounts":       "viewer@example.test",
		"auth_files":     "codex-account.json",
		"auth_indices":   "auth-index-private",
		"source_hashes":  "source-hash-private",
		"project_ids":    "project-visible",
	} {
		values, ok := filters[key].([]any)
		if !ok || len(values) != 1 || values[0] != want {
			t.Fatalf("resolved %s = %#v, want %q", key, filters[key], want)
		}
	}
	if _, exists := filters["api_key_ids"]; exists {
		t.Fatalf("Viewer-only API key field survived: %#v", filters)
	}
}

func TestProjectAnalyticsPublishesSafeIdentityDisplayOptions(t *testing.T) {
	response := map[string]any{"filter_options": map[string]any{
		"auth_files":  []any{"codex-viewer@example.test.json"},
		"project_ids": []any{"visible-project"},
	}}
	projectAnalytics(response)
	options := response["filter_options"].(map[string]any)
	authOptions := options["auth_file_options"].([]any)
	projectOptions := options["project_options"].([]any)
	if len(authOptions) != 1 || len(projectOptions) != 1 {
		t.Fatalf("safe identity display options missing: %#v", options)
	}
	encoded, _ := json.Marshal(response)
	text := string(encoded)
	for _, want := range []string{"auth_file_ids", "auth_file_options", "codex-viewer@example.test.json", "project_options", "visible-project", "view_"} {
		if !strings.Contains(text, want) {
			t.Fatalf("safe option %q missing: %s", want, text)
		}
	}
	if strings.Contains(text, `"auth_files"`) {
		t.Fatalf("raw filter field name survived: %s", text)
	}
}

func TestProjectAnalyticsKeepsSyntheticAPIKeyInTotalsButNotSelectors(t *testing.T) {
	synthetic := "unknown-client-api-key:private-source:private-auth"
	realHash := strings.Repeat("d", 64)
	response := map[string]any{
		"api_key_stats": []any{
			map[string]any{"id": synthetic, "api_key_hash": synthetic, "calls": json.Number("3")},
			map[string]any{"id": realHash, "api_key_hash": realHash, "calls": json.Number("5")},
		},
		"filter_options": map[string]any{
			"api_key_hashes": []any{synthetic, realHash},
		},
		"events": map[string]any{"items": []any{map[string]any{
			"event_hash": "synthetic-event", "timestamp_ms": json.Number("1700000000000"),
			"api_key_hash": synthetic,
		}}},
	}
	projectAnalytics(response)

	stats := response["api_key_stats"].([]any)
	if len(stats) != 2 {
		t.Fatalf("synthetic statistics row was dropped: %#v", stats)
	}
	unknown := stats[0].(map[string]any)
	if unknown["api_key_selectable"] != false || unknown["api_key_id"] != pseudonym(synthetic) {
		t.Fatalf("synthetic row was not safely marked: %#v", unknown)
	}
	options := response["filter_options"].(map[string]any)["api_key_ids"].([]any)
	if len(options) != 1 || options[0] != pseudonym(realHash) {
		t.Fatalf("synthetic key survived selector projection: %#v", options)
	}
	encodedBytes, _ := json.Marshal(response)
	encoded := string(encodedBytes)
	for _, secret := range []string{synthetic, "private-source", "private-auth", realHash, `"api_key_hash"`} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("API-key projection leaked %q: %s", secret, encoded)
		}
	}
}

func TestProjectAnalyticsPseudonymizesProviderAccountSubject(t *testing.T) {
	rawSubject := "acct-private-subject-123"
	response := map[string]any{"events": map[string]any{"items": []any{map[string]any{
		"event_hash": "subject-event", "timestamp_ms": json.Number("1700000000000"),
		"auth_account_id_snapshot": rawSubject,
	}}}}
	projectAnalytics(response)
	encodedBytes, _ := json.Marshal(response)
	encoded := string(encodedBytes)
	if strings.Contains(encoded, rawSubject) || strings.Contains(encoded, "auth_account_id_snapshot") {
		t.Fatalf("raw provider account subject leaked: %s", encoded)
	}
	if !strings.Contains(encoded, `"account_subject_id":"`+pseudonym(rawSubject)+`"`) {
		t.Fatalf("safe provider account subject missing: %s", encoded)
	}
}

func TestFilterActiveAliasesDropsDeletedAPIKeys(t *testing.T) {
	activeKey := "active-viewer-test-key"
	activeSum := sha256.Sum256([]byte(activeKey))
	activeHash := hex.EncodeToString(activeSum[:])
	deletedSum := sha256.Sum256([]byte("deleted-viewer-test-key"))
	deletedHash := hex.EncodeToString(deletedSum[:])
	aliases := []apiKeyAlias{
		{APIKeyHash: activeHash, Alias: "有效别名"},
		{APIKeyHash: deletedHash, Alias: "已删别名"},
	}
	filtered := filterActiveAliases(aliases, []string{activeKey})
	if len(filtered) != 1 || filtered[0].Alias != "有效别名" || filtered[0].APIKeyHash != activeHash {
		t.Fatalf("active aliases = %#v", filtered)
	}
}

func TestHistoricalAliasContractSeparatesActiveListAndEventLabels(t *testing.T) {
	activeKey := "active-viewer-test-key"
	activeSum := sha256.Sum256([]byte(activeKey))
	activeHash := hex.EncodeToString(activeSum[:])
	deletedSum := sha256.Sum256([]byte("deleted-viewer-test-key"))
	deletedHash := hex.EncodeToString(deletedSum[:])

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/api-key-aliases":
			writeJSON(w, http.StatusOK, apiKeyAliasEnvelope{Items: []apiKeyAlias{
				{APIKeyHash: activeHash, Alias: "当前别名"},
				{APIKeyHash: deletedHash, Alias: "历史别名"},
			}})
		case "/v0/management/api-keys":
			writeJSON(w, http.StatusOK, activeAPIKeyEnvelope{APIKeys: []string{activeKey}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	server := &Server{cpamp: cpamp.New(upstream.URL, "test-admin-key", time.Second, 1<<20)}
	allAliases, err := server.loadAllAliases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	activeAliases, err := server.loadActiveAliases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(allAliases.Items) != 2 {
		t.Fatalf("all aliases = %#v", allAliases.Items)
	}
	if len(activeAliases.Items) != 1 || activeAliases.Items[0].Alias != "当前别名" {
		t.Fatalf("active aliases = %#v", activeAliases.Items)
	}

	response := map[string]any{
		"events": map[string]any{"items": []any{map[string]any{
			"event_hash":   "historical-event",
			"api_key_hash": deletedHash,
		}}},
	}
	attachAnalyticsAliases(response, allAliases)
	projectAnalytics(response)
	encoded, _ := json.Marshal(response)
	text := string(encoded)
	if !strings.Contains(text, "历史别名") || !strings.Contains(text, pseudonym(deletedHash)) {
		t.Fatalf("historical alias was not preserved on the event row: %s", text)
	}
	if strings.Contains(text, deletedHash) || strings.Contains(text, `"api_key_hash"`) {
		t.Fatalf("historical event leaked its raw API key hash: %s", text)
	}
}

func TestMaintenanceProjectionKeepsOnlyPublicStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" || r.URL.Query().Get("scope") != "database-maintenance" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"service": "usage",
			"dbPath":  "/private/usage.sqlite",
			"databaseMaintenance": map[string]any{
				"required":            true,
				"performanceDegraded": true,
				"deferredIndexes":     2_000_000,
				"offlineJobs":         -4,
				"reasons":             []string{"private_reason"},
				"command":             "private cleanup command",
			},
		})
	}))
	defer upstream.Close()

	server := &Server{cpamp: cpamp.New(upstream.URL, "test-admin-key", time.Second, 1<<20)}
	response := httptest.NewRecorder()
	server.handleMaintenance(response, httptest.NewRequest(http.MethodGet, "http://viewer.test/viewer/api/v1/maintenance", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("maintenance status = %d: %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	encoded := response.Body.String()
	for _, secret := range []string{"dbPath", "/private/usage.sqlite", "private_reason", "private cleanup command", `"service"`} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("maintenance response leaked %q: %s", secret, encoded)
		}
	}
	maintenance := payload["databaseMaintenance"].(map[string]any)
	if maintenance["required"] != true || maintenance["performanceDegraded"] != true || maintenance["deferredIndexes"] != float64(1_000_000) || maintenance["offlineJobs"] != float64(0) {
		t.Fatalf("unexpected maintenance projection: %#v", maintenance)
	}
}

func TestUsageMigrationReadyAcceptsCurrentAndLegacyStatus(t *testing.T) {
	for _, status := range []string{"ready", "migrated", "completed", " MIGRATED "} {
		if !usageMigrationReady(status) {
			t.Fatalf("ready migration status rejected: %q", status)
		}
	}
	for _, status := range []string{"", "pending", "failed"} {
		if usageMigrationReady(status) {
			t.Fatalf("unfinished migration status accepted: %q", status)
		}
	}
}

func TestProjectDashboardChannelsKeepsSafeDisplaysAndAnonymizesIndexes(t *testing.T) {
	rows := projectDashboardChannels([]dashboardChannelUpstream{{
		AuthIndex:            "620a026566e94d09",
		Source:               "masked@example.test",
		AccountSnapshot:      "private-account@example.test",
		AuthLabelSnapshot:    "private-label@example.test",
		AuthProviderSnapshot: "codex",
		Calls:                22,
		SuccessRate:          1,
		Tokens:               83500,
		Tone:                 "good",
	}})
	if len(rows) != 1 || rows[0].Calls != 22 || rows[0].Provider != "codex" {
		t.Fatalf("unexpected channel projection: %#v", rows)
	}
	encoded, _ := json.Marshal(rows)
	text := string(encoded)
	for _, secret := range []string{"620a026566e94d09", `"auth_index"`} {
		if strings.Contains(text, secret) {
			t.Fatalf("dashboard channel leaked %q: %s", secret, text)
		}
	}
	for _, expected := range []string{"view_", "codex", "masked@example.test", "private-account@example.test", "private-label@example.test", "account_display", "auth_label_display", "source_display"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("safe channel field %q missing: %s", expected, text)
		}
	}
}

func TestSanitizeDashboardFailuresKeepsSafeDisplaysAndDropsSecrets(t *testing.T) {
	rawHash := strings.Repeat("a", 64)
	upstream := dashboardUpstream{RecentFailures: []dashboardFailure{{
		APIKeyHash:             rawHash,
		Source:                 "codex-source@example.test",
		SourceHash:             "source-hash-secret",
		AuthIndex:              "auth-index-secret",
		AccountSnapshot:        "private-account@example.test",
		AuthLabelSnapshot:      "private-label@example.test",
		AuthProjectIDSnapshot:  "project-visible",
		Endpoint:               "https://provider.test/v1/responses?token=secret",
		HeaderQuotaPlanType:    "plus",
		HeaderQuotaUsedPercent: floatPointer(20),
		HeaderErrorKind:        "quota",
	}}}
	sanitizeDashboardUpstream(&upstream, apiKeyAliasEnvelope{Items: []apiKeyAlias{{APIKeyHash: rawHash, Alias: "牛哥"}}})
	encoded, _ := json.Marshal(upstream.RecentFailures)
	text := string(encoded)
	for _, secret := range []string{rawHash, "source-hash-secret", "auth-index-secret", "token=secret", `"api_key_hash"`, `"source_hash"`, `"auth_index"`} {
		if strings.Contains(text, secret) {
			t.Fatalf("dashboard failure leaked %q: %s", secret, text)
		}
	}
	for _, expected := range []string{"牛哥", "api_key_id", "source_id", "auth_id", "account_id", "auth_label_id", "project_id", "codex-source@example.test", "private-account@example.test", "private-label@example.test", "project-visible", "plus", "quota", `"endpoint":"/v1/responses"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("safe dashboard failure field %q missing: %s", expected, text)
		}
	}
}

func TestAliasFilterPrefersNewAPIKeyIDsField(t *testing.T) {
	firstHash := strings.Repeat("a", 64)
	secondHash := strings.Repeat("b", 64)
	aliases := apiKeyAliasEnvelope{Items: []apiKeyAlias{
		{APIKeyHash: firstHash, Alias: "马哥"},
		{APIKeyHash: secondHash, Alias: "牛哥"},
	}}
	payload := map[string]any{
		"filters": map[string]any{
			"api_key_ids":    []any{pseudonym(firstHash)},
			"api_key_hashes": []any{pseudonym(secondHash)},
		},
	}
	if err := resolveAliasFilters(payload, aliases); err != nil {
		t.Fatal(err)
	}
	filters := payload["filters"].(map[string]any)
	values := filters["api_key_hashes"].([]any)
	if len(values) != 1 || values[0] != firstHash {
		t.Fatalf("api_key_ids did not take priority: %#v", filters)
	}
	if _, exists := filters["api_key_ids"]; exists {
		t.Fatalf("Viewer-only filter leaked upstream: %#v", filters)
	}
}

func TestAliasFilterRejectsRawHash(t *testing.T) {
	rawHash := strings.Repeat("a", 64)
	payload := map[string]any{
		"filters": map[string]any{"api_key_hashes": []any{rawHash}},
	}
	if err := resolveAliasFilters(payload, apiKeyAliasEnvelope{}); err == nil {
		t.Fatal("raw API key hash was accepted")
	}
}

func TestAliasFilterRejectsUnknownPseudonym(t *testing.T) {
	payload := map[string]any{
		"filters": map[string]any{"api_key_hashes": []any{"view_000000000000"}},
	}
	if err := resolveAliasFilters(payload, apiKeyAliasEnvelope{}); err == nil {
		t.Fatal("unknown alias filter was accepted")
	}
}

func TestSafeEndpointDropsCredentialsAndPath(t *testing.T) {
	if got := safeEndpoint("https://user:password@example.test/private?q=secret"); got != "https://example.test" {
		t.Fatalf("safe endpoint = %q", got)
	}
}

func TestSafeDashboardURLDropsCredentialsAndQuery(t *testing.T) {
	if got := safeDisplayURL("https://user:password@example.test:8443/base?token=secret#fragment"); got != "https://example.test:8443/base" {
		t.Fatalf("safe dashboard URL = %q", got)
	}
	for _, raw := range []string{
		"file:///etc/passwd",
		"https://example.test/token=secret",
		"https://example.test/access_token/secret",
	} {
		if got := safeDisplayURL(raw); got != "" {
			t.Fatalf("unsafe dashboard URL %q survived as %q", raw, got)
		}
	}
}

func TestCountDashboardModelsDeduplicatesNamedModels(t *testing.T) {
	models := dashboardModelList{Data: []map[string]any{
		{"id": "gpt-safe"},
		{"id": "gpt-safe"},
		{"model": "claude-safe"},
		{},
	}}
	if got := countDashboardModels(models); got != 3 {
		t.Fatalf("dashboard model count = %d", got)
	}
}
