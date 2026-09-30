package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

const key = "cpamp-test-admin-key"

var mockAPIKeys = []string{
	"mock-client-key-mage",
	"mock-client-key-niuge",
	"mock-client-key-jige",
}

func mockAPIKeyHash(index int) string {
	sum := sha256.Sum256([]byte(mockAPIKeys[index]))
	return hex.EncodeToString(sum[:])
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		write(w, map[string]any{"ok": true, "service": "mock-cpamp"})
	})
	mux.HandleFunc("/usage-service/info", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		write(w, map[string]any{"service": "cpa-manager-plus", "mode": "embedded", "startedAt": time.Now().Add(-2 * time.Hour).UnixMilli(), "configured": true, "adminReady": true, "projectInitialized": true, "migrationStatus": "ready"})
	}))
	mux.HandleFunc("/usage-service/config", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		write(w, map[string]any{"config": map[string]any{"cpaConnection": map[string]any{"cpaBaseUrl": "http://cli-proxy-api:8317", "managementKey": "must-never-reach-viewer"}}})
	}))
	mux.HandleFunc("/status", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		now := time.Now().UnixMilli()
		write(w, map[string]any{"service": "cpa-manager-plus", "events": 12480, "deadLetters": 0, "collector": map[string]any{"mode": "auto", "collector": "running", "queue": "usage", "lastConsumedAt": now - 5000, "lastInsertedAt": now - 6000, "totalInserted": 12480, "totalSkipped": 3}})
	}))
	mux.HandleFunc("/v0/management/config", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		w.Header().Set("X-CPA-Version", "v7.2.72")
		w.Header().Set("X-CPA-Build-Date", "2026-07-13T10:24:28Z")
		write(w, map[string]any{
			"api-keys": mockAPIKeys, "gemini-api-key": []any{}, "codex-api-key": []any{},
			"claude-api-key": []any{}, "openai-compatibility": []any{},
			"debug": false, "logging-to-file": true, "request-retry": 3,
			"ws-auth": true, "routing": map[string]any{"strategy": "round-robin"},
		})
	}))
	mux.HandleFunc("/v0/management/request-error-logs", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		write(w, map[string]any{"files": []map[string]any{{"name": "error-redacted.log", "size": 128}}})
	}))
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		if r.Header.Get("Authorization") != "Bearer "+mockAPIKeys[0] {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		write(w, map[string]any{"data": []map[string]any{{"id": "gpt-5.2-codex"}, {"id": "claude-sonnet-4-5"}, {"id": "grok-code-fast-1"}}})
	})
	mux.HandleFunc("/v0/management/dashboard/summary", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		now := time.Now().UnixMilli()
		write(w, map[string]any{
			"generated_at_ms":               now,
			"window":                        map[string]any{"today_start_ms": now - 12*3600000, "now_ms": now, "rolling_30m_start_ms": now - 1800000},
			"today":                         map[string]any{"total_calls": 128, "success_calls": 124, "failure_calls": 4, "success_rate": 96.875, "input_tokens": 82000, "output_tokens": 23000, "cached_tokens": 12000, "reasoning_tokens": 4000, "total_tokens": 121000, "total_cost": 0.842, "average_latency_ms": 1260, "zero_token_calls": 1},
			"rolling_30m":                   map[string]any{"rpm": 4.2, "tpm": 4800, "total_calls": 126, "total_tokens": 144000},
			"traffic_timeline":              []map[string]any{{"bucket_ms": now - 2*3600000, "calls": 32, "tokens": 28000, "success": 31, "failure": 1}, {"bucket_ms": now - 3600000, "calls": 45, "tokens": 41000, "success": 44, "failure": 1}, {"bucket_ms": now, "calls": 51, "tokens": 52000, "success": 49, "failure": 2}},
			"today_request_health_timeline": map[string]any{"success_calls": 124, "failure_calls": 4, "total_calls": 128, "success_rate": 96.875, "points": []map[string]any{{"bucket_ms": now - 1200000, "calls": 8, "tokens": 9200, "success": 8, "failure": 0, "success_rate": 100, "tone": "success"}, {"bucket_ms": now - 600000, "calls": 7, "tokens": 8100, "success": 6, "failure": 1, "success_rate": 85.7, "tone": "warning"}}},
			"token_mix":                     []map[string]any{{"key": "input", "tokens": 82000, "share": 67.8}, {"key": "output", "tokens": 23000, "share": 19}, {"key": "cached", "tokens": 12000, "share": 9.9}, {"key": "reasoning", "tokens": 4000, "share": 3.3}},
			"top_models_today":              []map[string]any{{"model": "gpt-5.2-codex", "calls": 82, "tokens": 76000, "success_rate": 97.56, "cost": 0.52}},
			"model_cost_rank":               []map[string]any{{"model": "gpt-5.2-codex", "calls": 82, "tokens": 76000, "success_rate": 97.56, "cost": 0.52, "cost_share": 1}},
			"recent_failures":               []map[string]any{{"timestamp_ms": now - 48000, "model": "gpt-5.2-codex", "api_key_hash": mockAPIKeyHash(0), "account_snapshot": "Platform Team", "endpoint": "/v1/responses", "duration_ms": 3050, "fail_status_code": 429, "fail_summary": "Rate limit reached"}},
		})
	}))
	mux.HandleFunc("/v0/management/api-keys", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		write(w, map[string]any{"api-keys": mockAPIKeys})
	}))
	mux.HandleFunc("/v0/management/api-key-aliases", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		write(w, map[string]any{"items": []map[string]any{
			{"apiKeyHash": mockAPIKeyHash(0), "alias": "马哥", "updatedAtMs": time.Now().UnixMilli()},
			{"apiKeyHash": mockAPIKeyHash(1), "alias": "牛哥", "updatedAtMs": time.Now().UnixMilli()},
			{"apiKeyHash": mockAPIKeyHash(2), "alias": "鸡哥", "updatedAtMs": time.Now().UnixMilli()},
		}})
	}))
	mux.HandleFunc("/v0/management/model-prices", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		write(w, map[string]any{"prices": map[string]any{
			"gpt-5.2-codex": map[string]any{
				"prompt": 1.25, "completion": 10.0, "cache": 0.125,
				"cacheRead": 0.125, "cacheCreation": 1.25,
				"promptConfigured": true, "completionConfigured": true,
				"cacheReadConfigured": true, "cacheCreationConfigured": true,
			},
		}})
	}))
	mux.HandleFunc("/v0/management/auth-files", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		write(w, map[string]any{"files": []map[string]any{
			{"name": "codex-team.json", "provider": "codex", "authIndex": "codex-team", "disabled": false, "status": "healthy", "account_snapshot": "Platform Team", "success": 1842, "failed": 18},
			{"name": "claude-research.json", "provider": "claude", "authIndex": "claude-research", "disabled": false, "status": "healthy", "account_snapshot": "Research Team", "success": 952, "failed": 12},
			{"name": "xai-ops.json", "provider": "xai", "authIndex": "xai-ops", "disabled": false, "status": "cooldown", "statusMessage": "Short retry backoff", "account_snapshot": "xAI Ops", "success": 294, "failed": 4},
		}})
	}))
	mux.HandleFunc("/v0/management/monitoring/header-snapshots", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodGet)
		now := time.Now().UnixMilli()
		write(w, map[string]any{"generated_at_ms": now, "items": []map[string]any{
			{"timestamp_ms": now - 120000, "auth_file_snapshot": "codex-team.json", "auth_index": "codex-team", "account_snapshot": "Platform Team", "header_quota_plan_type": "team", "response_metadata": map[string]any{"quota": map[string]any{"plan_type": "team", "primary": map[string]any{"used_percent": 8, "window_minutes": 300, "reset_at_ms": now + 3*3600000}, "secondary": map[string]any{"used_percent": 18, "window_minutes": 10080, "reset_at_ms": now + 5*86400000}}}},
			{"timestamp_ms": now - 300000, "auth_file_snapshot": "claude-research.json", "auth_index": "claude-research", "account_snapshot": "Research Team", "header_quota_plan_type": "max", "header_quota_used_percent": 44, "header_quota_recover_at_ms": now + 7200000, "response_metadata": map[string]any{"quota": map[string]any{"plan_type": "max", "used_percent": 44, "recover_at_ms": now + 7200000}}},
		}})
	}))
	mux.HandleFunc("/v0/management/monitoring/analytics", authorized(func(w http.ResponseWriter, r *http.Request) {
		mustMethod(w, r, http.MethodPost)
		now := time.Now().UnixMilli()
		day := int64(86400000)
		var request map[string]any
		_ = json.NewDecoder(r.Body).Decode(&request)
		write(w, map[string]any{"generated_at_ms": now, "granularity": "day", "summary": map[string]any{"total_calls": 12480, "success_calls": 12226, "failure_calls": 254, "success_rate": 97.96, "input_tokens": 6240000, "output_tokens": 2210000, "cached_tokens": 1380000, "reasoning_tokens": 320000, "total_tokens": 10150000, "total_cost": 42.683, "average_latency_ms": 1388, "p95_latency_ms": 3180, "rpm_30m": 18, "tpm_30m": 18400}, "timeline": []map[string]any{{"bucket_ms": now - 6*day, "label": "07/07", "calls": 1240, "tokens": 880000, "success": 1216, "failure": 24, "cost": 4.2}, {"bucket_ms": now - 5*day, "label": "07/08", "calls": 1630, "tokens": 1180000, "success": 1598, "failure": 32, "cost": 5.7}, {"bucket_ms": now - 4*day, "label": "07/09", "calls": 1880, "tokens": 1410000, "success": 1848, "failure": 32, "cost": 6.4}, {"bucket_ms": now - 3*day, "label": "07/10", "calls": 2140, "tokens": 1660000, "success": 2100, "failure": 40, "cost": 7.8}, {"bucket_ms": now - 2*day, "label": "07/11", "calls": 2310, "tokens": 1810000, "success": 2256, "failure": 54, "cost": 8.9}, {"bucket_ms": now - day, "label": "07/12", "calls": 1890, "tokens": 1520000, "success": 1850, "failure": 40, "cost": 6.5}, {"bucket_ms": now, "label": "07/13", "calls": 1390, "tokens": 970000, "success": 1358, "failure": 32, "cost": 3.183}}, "model_stats": []map[string]any{{"model": "gpt-5.2-codex", "calls": 4880, "success_calls": 4800, "failure_calls": 80, "success_rate": 98.36, "input_tokens": 2600000, "output_tokens": 920000, "cached_tokens": 740000, "total_tokens": 4260000, "cost": 16.8}, {"model": "claude-sonnet-4-5", "calls": 3620, "success_calls": 3538, "failure_calls": 82, "success_rate": 97.73, "input_tokens": 1920000, "output_tokens": 730000, "cached_tokens": 360000, "total_tokens": 3010000, "cost": 14.2}, {"model": "grok-code-fast-1", "calls": 2180, "success_calls": 2124, "failure_calls": 56, "success_rate": 97.43, "input_tokens": 1060000, "output_tokens": 390000, "cached_tokens": 180000, "total_tokens": 1630000, "cost": 7.4}}, "account_stats": []map[string]any{{"id": "a1", "account_snapshot": "Platform Team", "auth_label_snapshot": "Codex Team", "auth_provider_snapshot": "codex", "auth_indices": []string{"codex-team"}, "source_hashes": []string{"source-team"}, "calls": 4880, "success_calls": 4800, "failure_calls": 80, "success_rate": 98.36, "total_tokens": 4260000, "cost": 16.8, "average_latency_ms": 1210, "last_seen_ms": now}, {"id": "a2", "account_snapshot": "Research Team", "auth_label_snapshot": "Claude Team", "auth_provider_snapshot": "claude", "auth_indices": []string{"claude-research"}, "source_hashes": []string{"source-research"}, "calls": 3620, "success_calls": 3538, "failure_calls": 82, "success_rate": 97.73, "total_tokens": 3010000, "cost": 14.2, "average_latency_ms": 1460, "last_seen_ms": now - 60000}}, "api_key_stats": []map[string]any{{"id": "k1", "api_key_hash": mockAPIKeyHash(0), "calls": 6460, "success_calls": 6332, "failure_calls": 128, "success_rate": 98.01, "total_tokens": 5580000, "cost": 23.4, "average_latency_ms": 1320, "last_seen_ms": now}}, "filter_options": map[string]any{"account_stats": []map[string]any{{"account_snapshot": "Platform Team", "auth_label_snapshot": "Codex Team", "auth_indices": []string{"codex-team"}, "source_hashes": []string{"source-team"}}}, "api_key_hashes": []string{mockAPIKeyHash(0)}, "auth_files": []string{"codex-team.json", "claude-research.json"}, "project_ids": []string{"project-demo"}, "models": []string{"gpt-5.2-codex"}, "providers": []string{"codex"}}, "filter_selectors": map[string]any{"api_key_hashes": []string{mockAPIKeyHash(0)}, "auth_files": []string{"codex-team.json", "claude-research.json"}, "models": []string{"gpt-5.2-codex"}, "providers": []string{"codex"}}, "recent_failures": []map[string]any{{"timestamp_ms": now - 48000, "model": "gpt-5.2-codex", "api_key_hash": mockAPIKeyHash(0), "account_snapshot": "Platform Team", "endpoint": "/v1/responses", "duration_ms": 3050, "fail_status_code": 429, "fail_summary": "Rate limit reached"}}, "events": map[string]any{"items": []map[string]any{{"event_hash": "evt1", "timestamp_ms": now - 12000, "model": "gpt-5.2-codex", "resolved_model": "gpt-5.2-codex", "endpoint": "/v1/responses", "method": "POST", "path": "/v1/responses", "auth_index": "codex-team", "auth_file_snapshot": "codex-team.json", "source": "team", "source_hash": "source-team", "api_key_hash": mockAPIKeyHash(0), "account_snapshot": "Platform Team", "auth_label_snapshot": "Codex Team", "auth_provider_snapshot": "codex", "reasoning_effort": "high", "service_tier": "default", "executor_type": "responses", "input_tokens": 1860, "output_tokens": 442, "cached_tokens": 620, "cache_read_tokens": 620, "reasoning_tokens": 80, "total_tokens": 3002, "latency_ms": 1240, "ttft_ms": 180, "failed": false}, {"event_hash": "evt2", "timestamp_ms": now - 48000, "model": "gpt-5.2-codex", "resolved_model": "gpt-5.2-codex", "endpoint": "/v1/responses", "method": "POST", "path": "/v1/responses", "auth_index": "codex-team", "auth_file_snapshot": "codex-team.json", "source": "team", "source_hash": "source-team", "api_key_hash": mockAPIKeyHash(0), "account_snapshot": "Platform Team", "auth_label_snapshot": "Codex Team", "auth_provider_snapshot": "codex", "reasoning_effort": "high", "service_tier": "default", "executor_type": "responses", "input_tokens": 400, "output_tokens": 0, "cached_tokens": 0, "reasoning_tokens": 0, "total_tokens": 400, "latency_ms": 3050, "ttft_ms": 350, "failed": true, "fail_status_code": 429, "fail_summary": "Rate limit reached"}}, "has_more": false, "next_before_ms": 0, "total_count": 2}})
	}))
	log.Println("mock CPAMP listening on :18318")
	log.Fatal(http.ListenAndServe(":18318", mux))
}

func authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
func mustMethod(w http.ResponseWriter, r *http.Request, method string) {
	if r.Method != method {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		panic(http.ErrAbortHandler)
	}
}
func write(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
