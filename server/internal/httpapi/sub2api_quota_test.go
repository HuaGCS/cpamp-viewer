package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cpamp-viewer/server/internal/config"
	"cpamp-viewer/server/internal/cpamp"
	"cpamp-viewer/server/internal/sub2api"
)

func TestSub2APIQuotaProjectsPassiveUsageWithoutSecrets(t *testing.T) {
	var requests []string
	var requestsMu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "private-admin-key" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected upstream authentication headers: %#v", r.Header)
		}
		requestsMu.Lock()
		requests = append(requests, r.Method+" "+r.URL.String())
		requestsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/admin/accounts":
			_, _ = io.WriteString(w, `{"code":0,"data":{"items":[{"id":10,"name":"Alpha","platform":"openai","type":"oauth","status":"active","updated_at":"2026-09-30T00:00:00Z","extra":{"plan_type":"Plus","access_token":"secret-must-not-leak"}},{"id":11,"name":"Beta","platform":"anthropic","type":"api-key","status":"error","error_message":"Bearer secret-must-not-leak","updated_at":"2026-09-30T00:00:00Z","quota_limit":100,"quota_used":25}],"total":2,"page":1,"page_size":200,"pages":1}}`)
		case "/api/v1/admin/accounts/10/usage":
			if r.URL.Query().Get("source") != "passive" {
				t.Errorf("usage query is not passive: %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"source":"passive","updated_at":"2026-09-30T01:00:00Z","five_hour":{"utilization":25,"resets_at":"2026-09-30T05:00:00Z"},"seven_day":{"resets_at":"2026-10-07T00:00:00Z"},"subscription_tier":"Plus","error":"Bearer secret-must-not-leak"}}`)
		case "/api/v1/admin/accounts/11/usage":
			_, _ = io.WriteString(w, `{"code":0,"data":{"source":"passive","updated_at":null,"five_hour":null}}`)
		default:
			t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	cpampUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{storedQuotaTestFile("muse", "cpamp-one")}})
		case "/v0/management/monitoring/header-snapshots":
			_, _ = io.WriteString(w, `{"items":[]}`)
		default:
			t.Errorf("unexpected CPAMP request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer cpampUpstream.Close()

	server := &Server{
		cfg:     config.Config{PublicAccess: true},
		cpamp:   cpamp.New(cpampUpstream.URL, "private-cpamp-key", 3*time.Second, 1<<20),
		sub2api: sub2api.New(upstream.URL, "private-admin-key", "", 3*time.Second, 1<<20),
		logger:  slog.Default(),
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/viewer/api/v1/quota", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("quota status %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-must-not-leak") || strings.Contains(response.Body.String(), "private-admin-key") || strings.Contains(response.Body.String(), `"id":10`) {
		t.Fatalf("quota response leaked upstream details: %s", response.Body.String())
	}
	var payload quotaResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Source != "combined-quota" || len(payload.Accounts) != 3 || len(payload.Warnings) != 0 {
		t.Fatalf("unexpected quota response: %#v", payload)
	}
	var codex, claude, cpampAccount quotaAccount
	for _, account := range payload.Accounts {
		if account.Source == "cpamp" {
			cpampAccount = account
		} else if account.Provider == "codex" {
			codex = account
		} else if account.Provider == "claude" {
			claude = account
		}
	}
	if codex.ID != pseudonym("sub2api-account:10") || codex.Source != "sub2api" || codex.Plan != "Plus" || len(codex.Windows) != 2 || codex.Windows[0].Used != 25 || !strings.Contains(response.Body.String(), `"remaining_percent":null`) {
		t.Fatalf("unexpected Codex projection: %#v", codex)
	}
	if claude.ID != pseudonym("sub2api-account:11") || !claude.Disabled || len(claude.Windows) != 1 || claude.Windows[0].Remaining != 75 {
		t.Fatalf("unexpected Claude projection: %#v", claude)
	}
	if cpampAccount.Provider != "meta" || cpampAccount.DisplayName != "same@example.test" {
		t.Fatalf("CPAMP account was not preserved: %#v", cpampAccount)
	}
	if len(requests) != 3 {
		t.Fatalf("expected one list and two passive GETs, got %v", requests)
	}
	for _, request := range requests {
		if !strings.HasPrefix(request, "GET ") || strings.Contains(request, "/batch") {
			t.Fatalf("unexpected upstream method: %s", request)
		}
	}
}

func TestSub2APIQuotaRejectsUnexpectedParameters(t *testing.T) {
	server := &Server{cfg: config.Config{PublicAccess: true}}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/viewer/api/v1/quota?force=true", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected quota query status %d", response.Code)
	}
}

func TestCombinedQuotaKeepsCPAMPWhenSub2APIUnavailable(t *testing.T) {
	cpampUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0/management/auth-files":
			_ = json.NewEncoder(w).Encode(map[string]any{"files": []any{storedQuotaTestFile("muse", "cpamp-one")}})
		case "/v0/management/monitoring/header-snapshots":
			_, _ = io.WriteString(w, `{"items":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer cpampUpstream.Close()
	sub2APIUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"Bearer secret-must-not-leak"}`)
	}))
	defer sub2APIUpstream.Close()
	server := &Server{
		cfg:     config.Config{PublicAccess: true},
		cpamp:   cpamp.New(cpampUpstream.URL, "private-cpamp-key", time.Second, 1<<20),
		sub2api: sub2api.New(sub2APIUpstream.URL, "private-sub2api-key", "", time.Second, 1<<20),
		logger:  slog.Default(),
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/viewer/api/v1/quota", nil))
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "secret-must-not-leak") {
		t.Fatalf("unexpected partial quota response: %d %s", response.Code, response.Body.String())
	}
	var payload quotaResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 || payload.Accounts[0].Source != "cpamp" || len(payload.Warnings) != 1 || payload.Warnings[0] != "Sub2API 额度暂不可用" {
		t.Fatalf("incorrect partial quota result: %#v", payload)
	}
}

func TestCombinedQuotaKeepsSub2APIWhenCPAMPUnavailable(t *testing.T) {
	cpampUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer cpampUpstream.Close()
	sub2APIUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/admin/accounts":
			_, _ = io.WriteString(w, `{"code":0,"data":{"items":[{"id":4,"name":"Sub account","platform":"anthropic","status":"active"}],"total":1,"pages":1}}`)
		case "/api/v1/admin/accounts/4/usage":
			_, _ = io.WriteString(w, `{"code":0,"data":{"updated_at":"2026-09-30T01:00:00Z","five_hour":{"utilization":50}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer sub2APIUpstream.Close()
	server := &Server{
		cfg:     config.Config{PublicAccess: true},
		cpamp:   cpamp.New(cpampUpstream.URL, "private-cpamp-key", time.Second, 1<<20),
		sub2api: sub2api.New(sub2APIUpstream.URL, "private-sub2api-key", "", time.Second, 1<<20),
		logger:  slog.Default(),
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/viewer/api/v1/quota", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("partial quota status %d: %s", response.Code, response.Body.String())
	}
	var payload quotaResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Accounts) != 1 || payload.Accounts[0].Source != "sub2api" || len(payload.Warnings) != 1 || payload.Warnings[0] != "CPA Manager Plus 额度暂不可用" {
		t.Fatalf("incorrect partial quota result: %#v", payload)
	}
}

func TestSub2APIAccountListPaginatesWithoutTruncation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"code":0,"data":{"items":[{"id":1,"name":"one"}],"total":2,"page":1,"page_size":1,"pages":2}}`)
		case "2":
			_, _ = io.WriteString(w, `{"code":0,"data":{"items":[{"id":2,"name":"two"}],"total":2,"page":2,"page_size":1,"pages":2}}`)
		default:
			t.Errorf("unexpected account page %q", r.URL.Query().Get("page"))
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	server := &Server{sub2api: sub2api.New(upstream.URL, "test-key", "", time.Second, 1<<20)}
	accounts, err := server.fetchSub2APIAccounts(context.Background())
	if err != nil || len(accounts) != 2 || accounts[0].ID != 1 || accounts[1].ID != 2 {
		t.Fatalf("pagination result = %#v, %v", accounts, err)
	}
}

func TestSub2APIUnnamedAccountDoesNotExposeRawID(t *testing.T) {
	account := projectSub2APIAccount(sub2APIAccount{ID: 12345, Platform: "openai", Status: "active"}, sub2APIUsageInfo{}, false)
	if strings.Contains(account.DisplayName, "12345") || account.ID != pseudonym("sub2api-account:12345") {
		t.Fatalf("account identity was not projected: %#v", account)
	}
}
