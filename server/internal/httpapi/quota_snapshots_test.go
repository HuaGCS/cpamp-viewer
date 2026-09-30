package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpamp-viewer/server/internal/cpamp"
)

var storedQuotaTestTime = time.Date(2030, 9, 26, 12, 0, 0, 0, time.UTC)

func storedQuotaTestFile(provider, index string) map[string]any {
	return map[string]any{"name": "private-" + index + ".json", "auth_index": index, "provider": provider, "email": "same@example.test"}
}

func storedQuotaTestWindow(id, kind string, used any) map[string]any {
	window := map[string]any{
		"provider_window_id": id, "window_kind": kind, "window_mode": "fixed", "model_scope_kind": "all",
		"observed_at_ms": storedQuotaTestTime.UnixMilli(), "cycle_end_ms": storedQuotaTestTime.Add(time.Hour).UnixMilli(),
		"duration_seconds": 18000, "availability": "active", "plan_type": "pro",
	}
	if used != nil {
		window["used_percent"] = used
	}
	return window
}

func storedQuotaTestRaw(windows ...map[string]any) []json.RawMessage {
	var values []json.RawMessage
	for _, window := range windows {
		raw, _ := json.Marshal(window)
		values = append(values, raw)
	}
	return values
}

func storedQuotaTestCache(url string) *quotaSnapshotCache {
	cache := newQuotaSnapshotCache(cpamp.New(url, "private-test-admin-key", time.Second, 1<<20), time.Second)
	cache.now = func() time.Time { return storedQuotaTestTime }
	return cache
}

func TestStoredQuotaWindowsPreserveUnknownZeroAndWindowSemantics(t *testing.T) {
	unknown := storedQuotaTestWindow("meta:weekly", "weekly", nil)
	delete(unknown, "duration_seconds")
	unknown["window_mode"] = "unknown"
	remainingOnly := storedQuotaTestWindow("remaining-only", "daily", nil)
	remainingOnly["remaining_percent"] = 35
	zero := storedQuotaTestWindow("zero", "window", 0)
	exhausted := storedQuotaTestWindow("exhausted", "window", 100)
	conflict := storedQuotaTestWindow("conflict", "window", 10)
	conflict["remaining_percent"] = 20
	stale := storedQuotaTestWindow("stale", "weekly", 17)
	stale["cycle_end_ms"] = storedQuotaTestTime.Add(-time.Minute).UnixMilli()
	stale["field_sources"] = map[string]any{"quota": map[string]any{"observed_at_ms": storedQuotaTestTime.Add(-time.Hour).UnixMilli()}}
	result := projectStoredQuotaWindows(storedQuotaTestRaw(unknown, remainingOnly, zero, exhausted, conflict, stale), "meta", storedQuotaTestTime)
	if !result.Valid || len(result.Windows) != 6 {
		t.Fatalf("unexpected windows: %+v", result)
	}
	byID := map[string]quotaWindow{}
	for _, window := range result.Windows {
		byID[window.ID] = window
	}
	get := func(id string) quotaWindow { return byID[pseudonym("meta|"+id+"|all||")] }
	if window := get("meta:weekly"); !window.UnknownUsed || !window.UnknownRemaining || window.WindowMins != 0 || window.Label != "周额度" || window.ResetAtMS == 0 {
		t.Fatalf("unknown weekly quota was fabricated: %+v", window)
	}
	if window := get("remaining-only"); !window.UnknownUsed || window.UnknownRemaining || window.Remaining != 35 {
		t.Fatalf("remaining-only evidence fabricated used: %+v", window)
	}
	if window := get("zero"); window.UnknownUsed || window.UnknownRemaining || window.Used != 0 || window.Remaining != 100 {
		t.Fatalf("explicit zero lost: %+v", window)
	}
	if window := get("exhausted"); window.Used != 100 || window.Remaining != 0 || window.UnknownRemaining {
		t.Fatalf("exhausted quota lost: %+v", window)
	}
	if window := get("conflict"); !window.UnknownUsed || !window.UnknownRemaining {
		t.Fatalf("conflicting percentages invented progress: %+v", window)
	}
	if window := get("stale"); !window.Stale || window.ObservedAt != storedQuotaTestTime.Add(-time.Hour).UnixMilli() {
		t.Fatalf("old quota progress became fresh: %+v", window)
	}
	raw, err := json.Marshal(get("meta:weekly"))
	if err != nil || !strings.Contains(string(raw), `"used_percent":null`) || !strings.Contains(string(raw), `"remaining_percent":null`) {
		t.Fatalf("unknown percentage JSON = %s, %v", raw, err)
	}
	raw, _ = json.Marshal(quotaWindow{Used: 25, Remaining: 75})
	if !strings.Contains(string(raw), `"used_percent":25`) || !strings.Contains(string(raw), `"remaining_percent":75`) {
		t.Fatalf("existing Codex JSON changed: %s", raw)
	}
}

func TestStoredQuotaWindowsFilterFinancialPlaceholdersAndPrivateMetadata(t *testing.T) {
	weekly := storedQuotaTestWindow("private-weekly-id", "weekly", nil)
	weekly["model_scope_kind"] = "family"
	weekly["model_scope_key"] = "claude_gpt"
	weekly["source_observation_id"] = "private-observation"
	weekly["account_key"] = "private-account-key"
	weekly["reset_credits"] = []any{map[string]any{"id": "private-reset-credit"}}
	weekly["dca_token"] = "dca:private-credential"
	monthly := storedQuotaTestWindow("private-monthly", "monthly", 0)
	monthly["limit_value"] = 0
	privateScope := storedQuotaTestWindow("private-scope-id", "weekly", 20)
	privateScope["model_scope_kind"] = "feature"
	privateScope["model_scope_key"] = "Bearer private-credential"
	privateScope["model_ids"] = []string{"/private/credential.json", "https://private.invalid", strings.Repeat("b", 64)}
	privateScope["plan_type"] = "sk-private-credential"
	result := projectStoredQuotaWindows(storedQuotaTestRaw(weekly, monthly, privateScope), "xai", storedQuotaTestTime)
	if !result.Valid || len(result.Windows) != 2 {
		t.Fatalf("zero monthly limit became a quota: %+v", result)
	}
	encoded, _ := json.Marshal(result)
	for _, denied := range []string{"private", "Bearer", "dca:", "credential", "reset_credits", "account_key", "source_observation_id", strings.Repeat("b", 64)} {
		if strings.Contains(string(encoded), denied) {
			t.Fatalf("projection leaked %q: %s", denied, encoded)
		}
	}
	if !strings.Contains(string(encoded), "claude_gpt") {
		t.Fatal("safe model-family scope was lost")
	}
	if result := projectStoredQuotaWindows(storedQuotaTestRaw(monthly), "xai", storedQuotaTestTime); !result.Valid || len(result.Windows) != 0 {
		t.Fatal("valid inventory containing only omitted placeholders must clear old windows")
	}
}

func TestStoredQuotaMetaCredentialFormatsCannotEnterDisplayFields(t *testing.T) {
	for _, secret := range []string{"dca:fixture-only", "LLM|fixture-only", "prefix DCA:fixture-only", "prefix llm|fixture-only"} {
		window := storedQuotaTestWindow("window", "weekly", 20)
		window["model_scope_kind"] = "feature"
		window["model_scope_key"] = secret
		window["model_ids"] = []string{secret}
		window["plan_type"] = secret
		result := projectStoredQuotaWindows(storedQuotaTestRaw(window), "meta", storedQuotaTestTime)
		if !result.Valid || len(result.Windows) != 1 || result.Windows[0].ModelScope != "" || result.Plan != "" {
			t.Fatalf("credential material entered safe display: %#v", result)
		}
		if cleanText(secret, 240) != "敏感错误详情已隐藏" {
			t.Fatal("Meta credential bypassed common text projection")
		}
	}
}

func TestStoredQuotaQueryUsesOnlyTrustedIdentityAndFixedReadEndpoint(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != quotaSnapshotPath || r.URL.RawQuery != "" {
			t.Errorf("unexpected upstream request %s %s", r.Method, r.URL)
		}
		var body struct {
			Accounts []quotaSnapshotQueryAccount `json:"accounts"`
		}
		data, _ := io.ReadAll(r.Body)
		if json.Unmarshal(data, &body) != nil || len(body.Accounts) != 2 {
			t.Errorf("unexpected server-built query %s", data)
			w.WriteHeader(400)
			return
		}
		for _, denied := range []string{"attacker", "dca:", "api_key", "Authorization", "proxy_url", "include_inactive", "now_ms"} {
			if strings.Contains(string(data), denied) {
				t.Errorf("untrusted request field leaked: %s", denied)
			}
		}
		items := []any{}
		for _, account := range body.Accounts {
			if account.Provider != "meta" && account.Provider != "devin" {
				t.Errorf("wrong provider %s", account.Provider)
			}
			items = append(items, map[string]any{"row_key": account.RowKey, "provider": account.Provider, "account_key": "private-upstream-account", "windows": []any{storedQuotaTestWindow("private-id", "daily", 20)}})
		}
		items = append(items, map[string]any{"row_key": "unrequested-private-account", "provider": "meta", "windows": []any{storedQuotaTestWindow("foreign", "weekly", 99)}})
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer upstream.Close()
	meta := storedQuotaTestFile("muse", "meta-index")
	meta["url"], meta["method"], meta["dca_token"] = "https://attacker.invalid", "DELETE", "dca:private-token"
	meta["header"] = map[string]string{"Authorization": "Bearer attacker"}
	devin := storedQuotaTestFile("devin", "devin-index")
	files := []map[string]any{meta, devin, storedQuotaTestFile("codex", "codex-index"), {"provider": "meta", "email": "same@example.test"}}
	cache := storedQuotaTestCache(upstream.URL)
	result := cache.snapshot(context.Background(), files)
	if len(result) != 2 || calls.Load() != 1 {
		t.Fatalf("results=%+v calls=%d", result, calls.Load())
	}
	for _, value := range result {
		if !value.Valid || len(value.Windows) != 1 || value.Windows[0].Remaining != 80 {
			t.Fatalf("bad result: %+v", value)
		}
	}
	cache.mu.Lock()
	encoded, _ := json.Marshal(cache.current)
	cache.mu.Unlock()
	for _, denied := range []string{"private", "example.test", "meta-index", "devin-index", "account_key"} {
		if strings.Contains(string(encoded), denied) {
			t.Fatalf("cached raw identity %q: %s", denied, encoded)
		}
	}
}

func TestStoredQuotaCacheTTLFailureRecoveryAndCredentialIsolation(t *testing.T) {
	var calls atomic.Int32
	var clock atomic.Int64
	clock.Store(storedQuotaTestTime.UnixMilli())
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 2 {
			w.WriteHeader(404)
			_, _ = io.WriteString(w, "Bearer private-management-key")
			return
		}
		var body struct {
			Accounts []quotaSnapshotQueryAccount `json:"accounts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		items := []any{}
		for _, account := range body.Accounts {
			windows := []any{}
			if call == 1 {
				windows = append(windows, storedQuotaTestWindow("weekly", "weekly", 10))
			}
			items = append(items, map[string]any{"row_key": account.RowKey, "provider": account.Provider, "windows": windows})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer upstream.Close()
	cache := storedQuotaTestCache(upstream.URL)
	cache.now = func() time.Time { return time.UnixMilli(clock.Load()) }
	file := storedQuotaTestFile("meta", "one")
	account, _ := quotaSnapshotAccount(file)
	first := cache.snapshot(context.Background(), []map[string]any{file})[account.RowKey]
	if !first.Valid || first.Stale || len(first.Windows) != 1 {
		t.Fatalf("first result %+v", first)
	}
	first.Windows[0].Label = "caller-mutation"
	clock.Add(59000)
	if again := cache.snapshot(context.Background(), []map[string]any{file})[account.RowKey]; again.Windows[0].Label == "caller-mutation" || calls.Load() != 1 {
		t.Fatal("cache mutated or expired early")
	}
	changed := storedQuotaTestFile("meta", "two")
	if got := cache.snapshot(context.Background(), []map[string]any{changed}); len(got) != 0 || calls.Load() != 1 {
		t.Fatalf("credential switch crossed identity or cooldown: %+v", got)
	}
	clock.Add(1000)
	stale := cache.snapshot(context.Background(), []map[string]any{file})[account.RowKey]
	if !stale.Valid || !stale.Stale || !stale.Windows[0].Stale || stale.ObservedAt != first.ObservedAt || calls.Load() != 2 {
		t.Fatalf("bad stale result %+v calls=%d", stale, calls.Load())
	}
	_ = cache.snapshot(context.Background(), []map[string]any{file})
	if calls.Load() != 2 {
		t.Fatal("failure cooldown was not shared")
	}
	clock.Add(60000)
	empty := cache.snapshot(context.Background(), []map[string]any{file})[account.RowKey]
	if !empty.Valid || empty.Stale || len(empty.Windows) != 0 {
		t.Fatalf("successful empty inventory did not clear old windows %+v", empty)
	}
	if got := cache.snapshot(context.Background(), nil); len(got) != 0 {
		t.Fatal("deleted inventory returned cached accounts")
	}
}

func TestStoredQuotaCacheSharesRefreshAndReleasesCanceledWaiters(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		var body struct {
			Accounts []quotaSnapshotQueryAccount `json:"accounts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"row_key": body.Accounts[0].RowKey, "provider": "meta", "windows": []any{storedQuotaTestWindow("week", "weekly", 25)}}}})
	}))
	defer upstream.Close()
	cache := storedQuotaTestCache(upstream.URL)
	files := []map[string]any{storedQuotaTestFile("meta", "one")}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { cache.snapshot(ctx, files); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("canceled initiator waited on shared refresh")
	}
	var waiters sync.WaitGroup
	for i := 0; i < 12; i++ {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			for _, result := range cache.snapshot(context.Background(), files) {
				if !result.Valid {
					t.Error("shared refresh failed")
				}
			}
		}()
	}
	close(release)
	waiters.Wait()
	if calls.Load() != 1 {
		t.Fatalf("singleflight made %d calls", calls.Load())
	}
}

func TestStoredQuotaSchemaFailuresAreUnavailableAndCooledDown(t *testing.T) {
	for _, body := range []string{`{}`, `{"items":null}`, `{"items":false}`, `{"items":[]}`, `{"items":[{"row_key":"foreign","provider":"meta","windows":[]}]}`} {
		t.Run(body, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, body) }))
			defer upstream.Close()
			cache := storedQuotaTestCache(upstream.URL)
			file := storedQuotaTestFile("meta", "one")
			account, _ := quotaSnapshotAccount(file)
			for i := 0; i < 2; i++ {
				if result := cache.snapshot(context.Background(), []map[string]any{file})[account.RowKey]; result.Valid {
					t.Fatalf("invalid schema considered valid: %+v", result)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("invalid response retried without cooldown")
			}
		})
	}
}

func TestStoredQuotaBatchesAtMost200AndTimeoutBounded(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Accounts []quotaSnapshotQueryAccount `json:"accounts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Accounts) > 200 {
			t.Errorf("oversized batch %d", len(body.Accounts))
		}
		items := []any{}
		for _, account := range body.Accounts {
			items = append(items, map[string]any{"row_key": account.RowKey, "provider": account.Provider, "windows": []any{}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer upstream.Close()
	files := make([]map[string]any, 401)
	for i := range files {
		files[i] = storedQuotaTestFile("meta", fmt.Sprintf("index-%d", i))
	}
	cache := storedQuotaTestCache(upstream.URL)
	if got := cache.snapshot(context.Background(), files); len(got) != 401 || calls.Load() != 3 {
		t.Fatalf("batches produced %d results in %d calls", len(got), calls.Load())
	}
	for _, timeout := range []time.Duration{0, -time.Second, time.Minute} {
		if got := newQuotaSnapshotCache(nil, timeout).timeout; got != 5*time.Second {
			t.Fatalf("unbounded timeout %s", got)
		}
	}
}

func TestStoredQuotaHeaderFallbackNeverUsesEmailOrConflictingIdentity(t *testing.T) {
	file := storedQuotaTestFile("meta", "wanted")
	makeSnapshot := func(provider, name, index string) map[string]any {
		return map[string]any{"auth_provider_snapshot": provider, "auth_file_snapshot": name, "auth_index": index, "account_snapshot": "same@example.test", "timestamp_ms": storedQuotaTestTime.UnixMilli(), "header_quota_used_percent": float64(10)}
	}
	for _, snapshot := range []map[string]any{
		makeSnapshot("xai", "private-wanted.json", "wanted"),
		makeSnapshot("meta", "private-wanted.json", "wrong"),
		makeSnapshot("meta", "private-other.json", "wanted"),
		makeSnapshot("meta", "", ""),
	} {
		account, ok := projectQuotaAccount(file, []map[string]any{snapshot})
		if !ok || len(account.Windows) != 0 {
			t.Fatalf("conflicting fallback matched: %+v", account)
		}
	}
	account, ok := projectQuotaAccount(file, []map[string]any{makeSnapshot("meta", "private-wanted.json", "wanted")})
	if !ok || len(account.Windows) != 1 || account.Windows[0].Remaining != 90 {
		t.Fatalf("exact fallback failed: %+v", account)
	}
}

func TestStoredQuotaRejectsWrongProviderDuplicateAndMalformedWindow(t *testing.T) {
	for _, mode := range []string{"wrong-provider", "duplicate-row", "null-windows", "invalid-progress"} {
		t.Run(mode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Accounts []quotaSnapshotQueryAccount `json:"accounts"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				window := storedQuotaTestWindow("weekly", "weekly", 10)
				item := map[string]any{"row_key": body.Accounts[0].RowKey, "provider": "meta", "windows": []any{window}}
				items := []any{item}
				switch mode {
				case "wrong-provider":
					item["provider"] = "xai"
				case "duplicate-row":
					items = append(items, item)
				case "null-windows":
					item["windows"] = nil
				case "invalid-progress":
					window["used_percent"] = false
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
			}))
			defer upstream.Close()
			cache := storedQuotaTestCache(upstream.URL)
			file := storedQuotaTestFile("meta", "one")
			key, _ := quotaSnapshotAccount(file)
			if result := cache.snapshot(context.Background(), []map[string]any{file})[key.RowKey]; result.Valid {
				t.Fatalf("invalid response became successful: %+v", result)
			}
		})
	}
}

func TestStoredQuotaHandlerIntegratesSavedWindowsAndOldCPAMPFallback(t *testing.T) {
	for _, saved := range []bool{true, false} {
		t.Run(fmt.Sprint(saved), func(t *testing.T) {
			var pathsMu sync.Mutex
			paths := []string{}
			file := storedQuotaTestFile("muse", "meta-one")
			file["dca_token"] = "dca:private-auth-credential"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				pathsMu.Lock()
				paths = append(paths, r.Method+" "+r.URL.Path)
				pathsMu.Unlock()
				switch r.URL.Path {
				case "/v0/management/auth-files":
					_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{file}})
				case "/v0/management/monitoring/header-snapshots":
					_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"auth_file_snapshot": file["name"], "auth_index": file["auth_index"], "auth_provider_snapshot": "meta", "timestamp_ms": storedQuotaTestTime.Add(-time.Hour).UnixMilli(), "header_quota_used_percent": 10}}})
				case quotaSnapshotPath:
					if !saved {
						w.WriteHeader(404)
						_, _ = io.WriteString(w, "Bearer private-error-token")
						return
					}
					var body struct {
						Accounts []quotaSnapshotQueryAccount `json:"accounts"`
					}
					_ = json.NewDecoder(r.Body).Decode(&body)
					window := storedQuotaTestWindow("private-provider-window", "weekly", nil)
					delete(window, "duration_seconds")
					_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"row_key": body.Accounts[0].RowKey, "provider": "meta", "windows": []any{window}}}})
				default:
					t.Errorf("unexpected management/provider operation: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer upstream.Close()
			cache := storedQuotaTestCache(upstream.URL)
			server := &Server{cpamp: cache.client, cachedQuotaSnapshots: cache}
			recorder := httptest.NewRecorder()
			server.handleQuota(recorder, httptest.NewRequest(http.MethodGet, "/viewer/api/v1/quota", nil))
			if recorder.Code != 200 {
				t.Fatalf("quota HTTP %d: %s", recorder.Code, recorder.Body.String())
			}
			var response map[string]any
			_ = json.Unmarshal(recorder.Body.Bytes(), &response)
			accounts := response["accounts"].([]any)
			if len(accounts) != 1 {
				t.Fatalf("accounts=%#v", accounts)
			}
			account := accounts[0].(map[string]any)
			if account["provider"] != "meta" {
				t.Fatalf("muse not canonical: %#v", account)
			}
			window := account["windows"].([]any)[0].(map[string]any)
			if saved {
				if window["remaining_percent"] != nil || window["used_percent"] != nil || window["window_minutes"] != nil {
					t.Fatalf("unknown stored week guessed: %#v", window)
				}
			} else if window["remaining_percent"] != float64(90) || !strings.Contains(account["status_message"].(string), "暂不可用") {
				t.Fatalf("old CPAMP fallback failed: %#v", account)
			}
			for _, secret := range []string{"private-auth-credential", "private-provider-window", "private-error-token", "dca_token", "auth_index"} {
				if strings.Contains(recorder.Body.String(), secret) {
					t.Fatalf("quota response leaked %s", secret)
				}
			}
			pathsMu.Lock()
			count := len(paths)
			pathsMu.Unlock()
			if count != 3 {
				t.Fatalf("upstream calls=%d want3", count)
			}
		})
	}
}

func TestStoredQuotaTimeoutReturnsUnavailableWithCooldown(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	cache := storedQuotaTestCache(upstream.URL)
	cache.timeout = 20 * time.Millisecond
	file := storedQuotaTestFile("meta", "one")
	key, _ := quotaSnapshotAccount(file)
	start := time.Now()
	if result := cache.snapshot(context.Background(), []map[string]any{file})[key.RowKey]; result.Valid || time.Since(start) > time.Second {
		t.Fatalf("timeout failed: %+v elapsed=%s", result, time.Since(start))
	}
	_ = cache.snapshot(context.Background(), []map[string]any{file})
	if calls.Load() != 1 {
		t.Fatal("timed-out query bypassed cooldown")
	}
}
