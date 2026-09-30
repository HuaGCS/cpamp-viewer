package httpapi

import (
	"context"
	"cpamp-viewer/server/internal/cpamp"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func usageStatusFixture() map[string]any {
	return map[string]any{
		"raw_event_count": 6, "raw_archived_event_count": 4, "raw_deleted_event_count": 2, "raw_min_timestamp_ms": 100, "raw_max_timestamp_ms": 1000,
		"readiness":                       map[string]any{"migration_ready": true, "hourly_aggregate_ready": false, "archive_delete_enabled": true},
		"storage":                         map[string]any{"database_bytes": 4096, "wal_bytes": 1024, "shm_bytes": 512, "total_bytes": 5632, "reclaimable_bytes": 1024, "path": "private-database"},
		"compact_requires_stopped_server": true, "active_run": map[string]any{"status": "verifying", "id": "private-run", "last_error": "Bearer private-secret"},
		"active_lock": map[string]any{"operation": "verify", "run_id": "private-run"}, "migration": map[string]any{"name": "private-migration", "last_event_id": 999},
	}
}
func newUsageStatusTestCache(t *testing.T, h http.HandlerFunc) *usageStatusCache {
	t.Helper()
	up := httptest.NewServer(h)
	t.Cleanup(up.Close)
	return newUsageStatusCache(cpamp.New(up.URL, "synthetic-admin", time.Second, 1<<20), time.Second)
}
func TestUsageStatusValidatedReadAndPrivacy(t *testing.T) {
	var calls atomic.Int32
	c := newUsageStatusTestCache(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != "GET" || r.URL.Path != usageStatusPath || r.URL.RawQuery != "" || len(body) != 0 || r.Header.Get("Authorization") != "Bearer synthetic-admin" {
			t.Errorf("unexpected request shape")
		}
		_ = json.NewEncoder(w).Encode(usageStatusFixture())
	})
	got, err := c.snapshot(context.Background())
	if err != nil || !got.Available || got.Stale || got.Status == nil {
		t.Fatalf("snapshot unavailable: %+v %v", got, err)
	}
	if got.Status.RawDeletedEventCount != 2 || got.Status.Storage.TotalBytes != 5632 || got.Status.HourlyAggregateReady || got.Status.ActiveOperation != "verify" {
		t.Fatalf("incorrect projection: %+v", got.Status)
	}
	raw, _ := json.Marshal(got)
	for _, bad := range []string{"private", "run_id", "last_event_id", "archive_delete_enabled", "compact_requires", "migration\"", "path\""} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("private field leaked: %s", bad)
		}
	}
	*got.Status.RawArchivedEventCount = 999
	got.Status.Storage.TotalBytes = 0
	again, _ := c.snapshot(context.Background())
	if *again.Status.RawArchivedEventCount != 4 || again.Status.Storage.TotalBytes != 5632 || calls.Load() != 1 {
		t.Fatal("cache alias or missing shared cache")
	}
}
func TestUsageStatusRejectsLegacyAndMalformedSchemas(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body any
	}{
		{"old missing endpoint", 404, map[string]any{"error": "private"}},
		{"old GET 200", 200, map[string]any{"enabled": true, "usage": map[string]any{}}},
		{"null", 200, nil}, {"wrong counts", 200, map[string]any{"raw_event_count": "0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			c := newUsageStatusTestCache(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tc.code)
				_ = json.NewEncoder(w).Encode(tc.body)
			})
			for i := 0; i < 2; i++ {
				got, err := c.snapshot(context.Background())
				if err != nil || got.Available || got.Status != nil || got.Stale {
					t.Fatalf("old schema fabricated data: %+v %v", got, err)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("missing failure cooldown")
			}
		})
	}
	for _, field := range []string{"raw_event_count", "raw_deleted_event_count", "storage", "readiness", "compact_requires_stopped_server"} {
		raw := usageStatusFixture()
		delete(raw, field)
		if _, ok := projectUsageStatus(raw); ok {
			t.Errorf("accepted missing schema marker %s", field)
		}
	}
	raw := usageStatusFixture()
	delete(raw, "raw_min_timestamp_ms")
	delete(raw, "raw_max_timestamp_ms")
	delete(raw, "raw_archived_event_count")
	if got, ok := projectUsageStatus(raw); !ok || got.RawMinTimestampMS != nil || got.RawArchivedEventCount != nil {
		t.Fatal("valid older status with no optional times rejected")
	}
}
func TestUsageStatusConcurrentCancellationAndStaleRecovery(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var clock atomic.Int64
	clock.Store(time.Now().UnixMilli())
	c := newUsageStatusTestCache(t, func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			close(entered)
			<-release
		}
		if call == 2 {
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(usageStatusFixture())
	})
	c.now = func() time.Time { return time.UnixMilli(clock.Load()) }
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := c.snapshot(ctx); first <- err }()
	<-entered
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := c.snapshot(context.Background())
			if err != nil || !got.Available || got.Stale {
				t.Errorf("shared read failed")
			}
		}()
	}
	cancel()
	if <-first == nil {
		t.Fatal("canceled visitor was not released")
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("shared refresh duplicated")
	}
	clock.Add(usageStatusCacheTTL.Milliseconds())
	stale, _ := c.snapshot(context.Background())
	if !stale.Available || !stale.Stale || stale.Status == nil {
		t.Fatal("successful state lost on failure")
	}
	clock.Add(usageStatusCacheTTL.Milliseconds())
	fresh, _ := c.snapshot(context.Background())
	if !fresh.Available || fresh.Stale || fresh.CheckedAtMS <= stale.CheckedAtMS {
		t.Fatal("failed to recover")
	}
}
func TestUsageStatusRejectsVisitorControl(t *testing.T) {
	var calls atomic.Int32
	c := newUsageStatusTestCache(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(usageStatusFixture())
	})
	s := &Server{usageStatus: c}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "HEAD"} {
		w := httptest.NewRecorder()
		s.handleUsageStatus(w, httptest.NewRequest(method, "/viewer/api/v1/usage-status", nil))
		if w.Code != 405 {
			t.Errorf("accepted %s", method)
		}
	}
	for _, suffix := range []string{"?", "?url=https://attacker.invalid", "?refresh=true"} {
		w := httptest.NewRecorder()
		s.handleUsageStatus(w, httptest.NewRequest("GET", "/viewer/api/v1/usage-status"+suffix, nil))
		if w.Code != 400 {
			t.Errorf("accepted query")
		}
	}
	for _, hidden := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/viewer/api/v1/usage-status", strings.NewReader("{}"))
		if hidden {
			r.ContentLength = 0
		}
		w := httptest.NewRecorder()
		s.handleUsageStatus(w, r)
		if w.Code != 400 {
			t.Error("accepted body")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected request reached upstream")
	}
}
