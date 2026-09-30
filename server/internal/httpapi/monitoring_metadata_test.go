package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMonitoringMetadataProjectionPreservesResponseModelAndExplicitFalse(t *testing.T) {
	event := map[string]any{
		"event_hash": "private-event", "model": "gpt-6-astra", "requested_model": "my-alias", "resolved_model": "gpt-6-astra", "response_model": "gpt-5.6-luna",
		"reasoning_effort": "high", "service_tier": "auto", "request_service_tier": "priority", "response_service_tier": "default",
		"generate": false, "stream": false,
		"session_id": "private-session", "parent_session_id": "private-parent", "access_token_sha256": strings.Repeat("b", 64),
		"client_ip": "private-client-ip", "user_agent": "private-user-agent", "response_metadata": map[string]any{"raw": "private-response-metadata"},
		"authorization": "Bearer private-secret", "unexpected_future_field": "private-future-field",
	}
	response := map[string]any{
		"events":            map[string]any{"items": []any{event}},
		"drilldown_preview": map[string]any{"items": []any{event}},
	}
	projectAnalytics(response)
	for _, container := range []string{"events", "drilldown_preview"} {
		got := response[container].(map[string]any)["items"].([]any)[0].(map[string]any)
		for field, want := range map[string]any{"model": "gpt-6-astra", "requested_model": "my-alias", "resolved_model": "gpt-6-astra", "response_model": "gpt-5.6-luna", "reasoning_effort": "high", "service_tier": "auto", "request_service_tier": "priority", "response_service_tier": "default", "generate": false, "stream": false} {
			if value, exists := got[field]; !exists || value != want {
				t.Fatalf("%s.%s=%#v (present=%v), want %#v", container, field, value, exists, want)
			}
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	for _, denied := range []string{"private-", strings.Repeat("b", 64), "session_id", "parent_session_id", "access_token_sha256", "client_ip", "user_agent", "response_metadata", "authorization", "unexpected_future_field"} {
		if strings.Contains(string(encoded), denied) {
			t.Fatalf("private field survived public projection: %s", denied)
		}
	}
}

func TestMonitoringMetadataProjectionDoesNotInventMissingOrMalformedSettings(t *testing.T) {
	for _, raw := range []map[string]any{
		{"model": "legacy", "service_tier": "priority"},
		{"model": "legacy", "generate": nil, "stream": nil, "response_model": nil},
		{"model": "legacy", "generate": "false", "stream": float64(0), "response_model": []any{"private-model"}, "request_service_tier": map[string]any{"value": "private-tier"}, "response_service_tier": true},
	} {
		response := map[string]any{"events": map[string]any{"items": []any{raw}}}
		projectAnalytics(response)
		got := response["events"].(map[string]any)["items"].([]any)[0].(map[string]any)
		for _, field := range []string{"generate", "stream", "response_model", "request_service_tier", "response_service_tier"} {
			if _, exists := got[field]; exists {
				t.Fatalf("missing/malformed %s was treated as known: %#v", field, got)
			}
		}
		if got["model"] != "legacy" {
			t.Fatal("legacy event was lost")
		}
	}
}

func TestMonitoringMetadataProjectionKeepsTrueSeparateFromFalse(t *testing.T) {
	response := map[string]any{"events": map[string]any{"items": []any{map[string]any{"generate": true, "stream": false}, map[string]any{"generate": false, "stream": true}}}}
	projectAnalytics(response)
	rows := response["events"].(map[string]any)["items"].([]any)
	if rows[0].(map[string]any)["generate"] != true || rows[0].(map[string]any)["stream"] != false || rows[1].(map[string]any)["generate"] != false || rows[1].(map[string]any)["stream"] != true {
		t.Fatalf("request flag tri-state changed: %#v", rows)
	}
}

func TestMonitoringModelComparisonUsesExactConsistentNames(t *testing.T) {
	longName := strings.Repeat("model-", 35)
	for _, name := range []string{longName, "  " + longName + "  "} {
		response := map[string]any{"events": map[string]any{"items": []any{map[string]any{"requested_model": name, "resolved_model": name, "response_model": name}}}}
		projectAnalytics(response)
		row := response["events"].(map[string]any)["items"].([]any)[0].(map[string]any)
		for _, key := range []string{"requested_model", "resolved_model", "response_model"} {
			if row[key] != longName {
				t.Fatalf("%s was projected differently: %#v", key, row)
			}
		}
	}
	for _, name := range []string{strings.Repeat("model-", 50), "敏感错误详情已隐藏", "内部错误详情已隐藏", "Authorization: Bearer private-model-token"} {
		response := map[string]any{"events": map[string]any{"items": []any{map[string]any{"resolved_model": name, "response_model": name}}}}
		projectAnalytics(response)
		row := response["events"].(map[string]any)["items"].([]any)[0].(map[string]any)
		if _, exists := row["resolved_model"]; exists {
			t.Fatalf("uncomparable routed model was retained: %#v", row)
		}
		if _, exists := row["response_model"]; exists {
			t.Fatalf("uncomparable response model was retained: %#v", row)
		}
	}
}

func TestMonitoringOpaqueSourceDoesNotReplaceReadableAccountWithRedactionText(t *testing.T) {
	hash := strings.Repeat("c", 64)
	response := map[string]any{"events": map[string]any{"items": []any{map[string]any{"source": "h:" + hash, "source_hash": hash, "account_snapshot": "visible@example.test", "auth_label_snapshot": "Visible account"}}}}
	projectAnalytics(response)
	row := response["events"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if _, exists := row["source_display"]; exists {
		t.Fatalf("redaction marker became the account label: %#v", row)
	}
	if _, exists := row["source"]; exists {
		t.Fatalf("opaque source should not replace the readable account: %#v", row)
	}
	if row["account_display"] != "visible@example.test" || row["auth_label_display"] != "Visible account" || row["source_id"] == "" {
		t.Fatalf("safe identity/display evidence was lost: %#v", row)
	}
}
