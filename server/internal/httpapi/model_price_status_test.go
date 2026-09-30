package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpamp-viewer/server/internal/cpamp"
)

func newModelPriceStatusTestCache(t *testing.T, handler http.HandlerFunc) *modelPriceStatusCache {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	return newModelPriceStatusCache(cpamp.New(upstream.URL, "test-admin-key", time.Second, 1<<20), time.Second)
}

func TestModelPriceStatusUsesFixedReadAndProjectsSafeInventory(t *testing.T) {
	var calls atomic.Int32
	cache := newModelPriceStatusTestCache(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodGet || r.URL.Path != modelPriceStatusPath || r.URL.RawQuery != "" || len(body) != 0 {
			t.Errorf("unexpected upstream request: %s %s body=%q", r.Method, r.URL.String(), body)
		}
		if r.Header.Get("Authorization") != "Bearer test-admin-key" {
			t.Error("missing server-side authentication")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"models":         []any{"gpt-5", "org/model:latest", "gpt-5", " sk-private-credential ", strings.Repeat("a", 64), "bad\nname", "secret@example.com", "敏感错误详情已隐藏", true},
			"unpricedModels": []any{"org/model:latest", "gpt-5", "gpt-5", "not-in-inventory", "sk-private-credential", strings.Repeat("a", 64), false},
			"count":          false, "unpricedCount": -123,
			"management_key": "private-management-credential", "access_token_sha256": "private-token-fingerprint",
		})
	})
	got, err := cache.snapshot(context.Background())
	if err != nil || !got.Available || got.Stale || got.CheckedAtMS <= 0 {
		t.Fatalf("snapshot = %+v, %v", got, err)
	}
	if got.ModelCount != 2 || got.UnpricedCount != 2 || !reflect.DeepEqual(got.UnpricedModels, []string{"gpt-5", "org/model:latest"}) {
		t.Fatalf("unsafe or fabricated inventory: %+v", got)
	}
	raw, _ := json.Marshal(got)
	for _, secret := range []string{"private", "token", "management", "example.com", "敏感", "not-in-inventory", strings.Repeat("a", 64), `"models"`} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("public DTO leaked %q: %s", secret, raw)
		}
	}
	got.UnpricedModels[0] = "visitor-mutated-cache"
	again, err := cache.snapshot(context.Background())
	if err != nil || again.UnpricedModels[0] != "gpt-5" || calls.Load() != 1 {
		t.Fatalf("cache must retain an independent public projection: %+v %v calls=%d", again, err, calls.Load())
	}
}

func TestModelPriceStatusBoundsListWithoutInventingCounts(t *testing.T) {
	cache := newModelPriceStatusTestCache(t, func(w http.ResponseWriter, _ *http.Request) {
		names := make([]string, 250)
		for i := range names {
			names[i] = fmt.Sprintf("model-%03d", i)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": names, "unpricedModels": names, "count": 999999999, "unpricedCount": 0})
	})
	got, err := cache.snapshot(context.Background())
	if err != nil || !got.Available || len(got.UnpricedModels) != 200 || got.UnpricedCount != 250 || got.ModelCount != 250 {
		t.Fatalf("bounded status = %+v, %v", got, err)
	}
}

func TestModelPriceStatusUnavailableHasCooldownAndNoErrorDetails(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"legacy 404", 404, `{"error":"Bearer private-management-credential"}`},
		{"legacy 405", 405, "private-management-credential"},
		{"upstream failure", 500, "private-provider-diagnostics"},
		{"malformed", 200, `{"models":`},
		{"missing lists", 200, `{"count":0,"unpricedCount":0}`},
		{"null lists", 200, `{"models":null,"unpricedModels":null}`},
		{"false inventory", 200, `{"models":false,"unpricedModels":[]}`},
		{"false unpriced", 200, `{"models":[],"unpricedModels":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			cache := newModelPriceStatusTestCache(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			for i := 0; i < 2; i++ {
				got, err := cache.snapshot(context.Background())
				if err != nil || got.Available || got.Stale || got.CheckedAtMS != 0 || got.ModelCount != 0 || got.UnpricedCount != 0 || got.UnpricedModels == nil || len(got.UnpricedModels) != 0 {
					t.Fatalf("unavailable status = %+v, %v", got, err)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("failure cooldown did not hold: calls=%d", calls.Load())
			}
		})
	}
}

func TestModelPriceStatusTTLStaleAndSuccessfulEmptyInventory(t *testing.T) {
	var nowMS atomic.Int64
	nowMS.Store(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC).UnixMilli())
	var calls atomic.Int32
	cache := newModelPriceStatusTestCache(t, func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			_, _ = io.WriteString(w, `{"models":["gpt-5"],"unpricedModels":["gpt-5"]}`)
		case 2:
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"Bearer private-upstream-credential"}`)
		default:
			_, _ = io.WriteString(w, `{"models":[],"unpricedModels":[],"count":99,"unpricedCount":99}`)
		}
	})
	cache.now = func() time.Time { return time.UnixMilli(nowMS.Load()) }
	first, _ := cache.snapshot(context.Background())
	nowMS.Add((59 * time.Second).Milliseconds())
	cached, _ := cache.snapshot(context.Background())
	if !reflect.DeepEqual(first, cached) || calls.Load() != 1 {
		t.Fatalf("cache expired before TTL: %+v calls=%d", cached, calls.Load())
	}
	nowMS.Add(time.Second.Milliseconds())
	stale, err := cache.snapshot(context.Background())
	if err != nil || !stale.Available || !stale.Stale || stale.CheckedAtMS != first.CheckedAtMS || !reflect.DeepEqual(stale.UnpricedModels, first.UnpricedModels) || calls.Load() != 2 {
		t.Fatalf("failed refresh should retain stale public status: %+v %v", stale, err)
	}
	_, _ = cache.snapshot(context.Background())
	if calls.Load() != 2 {
		t.Fatal("stale refresh failure must be throttled")
	}
	nowMS.Add(time.Minute.Milliseconds())
	empty, err := cache.snapshot(context.Background())
	if err != nil || !empty.Available || empty.Stale || empty.CheckedAtMS <= first.CheckedAtMS || len(empty.UnpricedModels) != 0 || empty.UnpricedCount != 0 || empty.ModelCount != 0 || calls.Load() != 3 {
		t.Fatalf("successful empty inventory did not replace stale state: %+v %v", empty, err)
	}
}

func TestModelPriceStatusSharedRefreshSurvivesCanceledAndTimedOutWaiters(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	cache := newModelPriceStatusTestCache(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			_, _ = io.WriteString(w, `{"models":["gpt-5"],"unpricedModels":["gpt-5"]}`)
		case <-r.Context().Done():
			t.Error("visitor cancellation aborted shared upstream read")
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { _, err := cache.snapshot(ctx); firstDone <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	cancel()
	select {
	case err := <-firstDone:
		if err != context.Canceled {
			t.Fatalf("first canceled waiter error = %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("first canceled waiter was blocked by shared refresh")
	}
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer shortCancel()
	if _, err := cache.snapshot(shortCtx); err != context.DeadlineExceeded {
		t.Fatalf("short deadline waiter error = %v", err)
	}
	const visitors = 12
	results := make(chan modelPriceStatusResponse, visitors)
	var waiters sync.WaitGroup
	for i := 0; i < visitors; i++ {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			result, err := cache.snapshot(context.Background())
			if err != nil {
				t.Errorf("shared waiter: %v", err)
			}
			results <- result
		}()
	}
	close(release)
	waiters.Wait()
	close(results)
	for result := range results {
		if !result.Available || result.UnpricedCount != 1 {
			t.Fatalf("shared result = %+v", result)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("shared refresh duplicated calls: %d", calls.Load())
	}
}

func TestModelPriceStatusTimeoutIsBoundedAndCooledDown(t *testing.T) {
	var calls atomic.Int32
	cache := newModelPriceStatusTestCache(t, func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	})
	cache.timeout = 20 * time.Millisecond
	started := time.Now()
	got, err := cache.snapshot(context.Background())
	if err != nil || got.Available || time.Since(started) > time.Second {
		t.Fatalf("timeout status = %+v %v elapsed=%s", got, err, time.Since(started))
	}
	_, _ = cache.snapshot(context.Background())
	if calls.Load() != 1 {
		t.Fatal("timed-out refresh was not cooled down")
	}
	for _, timeout := range []time.Duration{0, -time.Second, time.Minute} {
		if got := newModelPriceStatusCache(nil, timeout).timeout; got != 5*time.Second {
			t.Fatalf("timeout %s was not capped: %s", timeout, got)
		}
	}
}

func TestModelPriceStatusHandlerRejectsVisitorControlBeforeUpstream(t *testing.T) {
	var calls atomic.Int32
	cache := newModelPriceStatusTestCache(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"models":[],"unpricedModels":[]}`)
	})
	server := &Server{modelPriceStatus: cache}
	cases := []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodPost, "/viewer/api/v1/model-price-status", "", 405},
		{http.MethodPut, "/viewer/api/v1/model-price-status", "", 405},
		{http.MethodDelete, "/viewer/api/v1/model-price-status", "", 405},
		{http.MethodHead, "/viewer/api/v1/model-price-status", "", 405},
		{http.MethodGet, "/viewer/api/v1/model-price-status?url=https://attacker.invalid&method=POST", "", 400},
		{http.MethodGet, "/viewer/api/v1/model-price-status?", "", 400},
		{http.MethodGet, "/viewer/api/v1/model-price-status", `{"headers":{"Authorization":"visitor-key"}}`, 400},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()
			server.handleModelPriceStatus(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("HTTP status=%d want=%d", recorder.Code, tc.status)
			}
		})
	}
	for _, chunked := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/viewer/api/v1/model-price-status", nil)
		req.Body = io.NopCloser(strings.NewReader("hidden body"))
		if chunked {
			req.TransferEncoding = []string{"chunked"}
		}
		recorder := httptest.NewRecorder()
		server.handleModelPriceStatus(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("hidden body accepted: HTTP %d", recorder.Code)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("rejected visitor control reached upstream: %d", calls.Load())
	}
	recorder := httptest.NewRecorder()
	server.handleModelPriceStatus(recorder, httptest.NewRequest(http.MethodGet, "/viewer/api/v1/model-price-status", nil))
	if recorder.Code != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("valid read failed: %d calls=%d", recorder.Code, calls.Load())
	}
}

func TestModelPriceStatusDisabledReturnsExplicitUnavailable(t *testing.T) {
	var cache *modelPriceStatusCache
	got, err := cache.snapshot(context.Background())
	if err != nil || got.Available || got.Stale || got.UnpricedModels == nil {
		t.Fatalf("nil cache = %+v %v", got, err)
	}
}
