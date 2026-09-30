package accessguard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientNeverFollowsRedirects(t *testing.T) {
	var calls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"bindings":[]}`))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL+"/private")
		w.WriteHeader(http.StatusFound)
	}))
	defer source.Close()
	_, err := New(source.URL, "private-management-key", time.Second, 1024).NativeKeyBindings(context.Background())
	if err != ErrUnavailable || calls.Load() != 0 {
		t.Fatalf("redirect was not safely rejected: err=%v calls=%d", err, calls.Load())
	}
}

func TestClientSanitizesStatusDecodeAndBodyLimitErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		limit  int64
	}{
		{"HTTP error", http.StatusInternalServerError, `{"error":"private-secret"}`, 1024},
		{"decode error", http.StatusOK, `{"bindings":"private-secret"}`, 1024},
		{"oversized", http.StatusOK, strings.Repeat("private-secret", 100), 16},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer upstream.Close()
			_, err := New(upstream.URL, "private-management-key", time.Second, test.limit).NativeKeyBindings(context.Background())
			if err != ErrUnavailable || strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe upstream error: %v", err)
			}
		})
	}
}
