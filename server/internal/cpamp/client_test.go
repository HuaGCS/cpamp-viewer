package cpamp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientInjectsAdminKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/management/test" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer cpamp-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	client := New(server.URL, "cpamp-secret", time.Second, 1024)
	var response map[string]bool
	if err := client.GetJSON(context.Background(), "/v0/management/test", nil, &response); err != nil {
		t.Fatal(err)
	}
	if !response["ok"] {
		t.Fatal("response not decoded")
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 2048))
	}))
	defer server.Close()
	client := New(server.URL, "secret", time.Second, 1024)
	if err := client.GetJSON(context.Background(), "/x", nil, &map[string]any{}); err == nil {
		t.Fatal("oversized response was accepted")
	}
}

func TestClientSupportsReadOnlyAlternateBearerAndResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		if authorization != "Bearer viewer-api-key" && authorization != "Bearer cpamp-secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("X-CPA-Version", "v7.2.72")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "gpt-safe"}}})
	}))
	defer server.Close()

	client := New(server.URL, "cpamp-secret", time.Second, 1024)
	var response map[string]any
	if err := client.GetJSONWithBearer(context.Background(), "/v1/models", "viewer-api-key", nil, &response); err != nil {
		t.Fatal(err)
	}
	if len(response["data"].([]any)) != 1 {
		t.Fatalf("alternate bearer response = %#v", response)
	}

	headers, err := client.GetJSONWithHeaders(context.Background(), "/v1/models", nil, &response)
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("X-CPA-Version") != "v7.2.72" {
		t.Fatalf("response version header = %q", headers.Get("X-CPA-Version"))
	}
}

func TestClientDoesNotFollowRedirectsWithCredentials(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("redirect target received authorization: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer source.Close()

	client := New(source.URL, "cpamp-secret", time.Second, 1024)
	err := client.GetJSON(context.Background(), "/redirect", nil, &map[string]any{})
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusFound {
		t.Fatalf("redirect result = %#v, want HTTP %d", err, http.StatusFound)
	}
	if targetCalled {
		t.Fatal("credential-bearing client followed an upstream redirect")
	}
}

func TestUpstreamErrorDoesNotRetainSensitiveBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"access_token":"must-not-enter-error-logs"}`))
	}))
	defer server.Close()

	client := New(server.URL, "cpamp-secret", time.Second, 1024)
	err := client.GetJSON(context.Background(), "/failure", nil, &map[string]any{})
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || upstream.Status != http.StatusInternalServerError {
		t.Fatalf("upstream error = %#v", err)
	}
	if strings.Contains(err.Error(), "must-not-enter-error-logs") || strings.Contains(err.Error(), "access_token") {
		t.Fatalf("upstream error retained sensitive body: %v", err)
	}
}
