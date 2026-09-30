package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"cpamp-viewer/server/internal/cpamp"
)

const codexUsageCacheTTL = time.Minute
const codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

type codexUsageResult struct {
	Windows    []quotaWindow
	Plan       string
	ObservedAt int64
	Valid      bool
	Stale      bool
}

// Only the safe projection survives a refresh. Credential metadata and raw
// provider responses stay local to the fixed, server-controlled read operation.
type codexUsageCache struct {
	client  *cpamp.Client
	timeout time.Duration
	now     func() time.Time

	mu          sync.Mutex
	pending     chan struct{}
	nextAttempt time.Time
	current     map[string]codexUsageResult
}

func newCodexUsageCache(client *cpamp.Client, timeout time.Duration) *codexUsageCache {
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	return &codexUsageCache{client: client, timeout: timeout, now: time.Now}
}

func codexUsageKey(file map[string]any) string {
	return pseudonym(stringValue(file["name"], file["id"]) + "|" + stringValue(file["auth_index"], file["authIndex"]) + "|" + codexUsageAccountID(file))
}

func (c *codexUsageCache) snapshot(ctx context.Context, files []map[string]any) map[string]codexUsageResult {
	if c == nil {
		return nil
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		c.mu.Lock()
		if c.now().Before(c.nextAttempt) {
			current := c.current
			c.mu.Unlock()
			return codexUsageForFiles(current, files)
		}
		if c.pending == nil {
			c.pending = make(chan struct{})
			previous := c.current
			// One disconnected visitor must not cancel the shared refresh; the
			// visitor itself still stops waiting when its deadline expires.
			refreshParent := context.WithoutCancel(ctx)
			go func() {
				refreshCtx, cancel := context.WithTimeout(refreshParent, 20*time.Second)
				current := c.refresh(refreshCtx, files, previous)
				cancel()
				c.mu.Lock()
				c.current = current
				c.nextAttempt = c.now().Add(codexUsageCacheTTL)
				close(c.pending)
				c.pending = nil
				c.mu.Unlock()
			}()
		}
		pending := c.pending
		c.mu.Unlock()
		select {
		case <-pending:
		case <-ctx.Done():
			return nil
		}
	}
}

func codexUsageForFiles(current map[string]codexUsageResult, files []map[string]any) map[string]codexUsageResult {
	selected := make(map[string]codexUsageResult)
	for _, file := range files {
		if strings.ToLower(stringValue(file["provider"], file["type"])) != "codex" {
			continue
		}
		key := codexUsageKey(file)
		if result, exists := current[key]; exists {
			if boolValue(file["disabled"]) {
				result.Stale = true
			}
			selected[key] = result
		}
	}
	return selected
}

func (c *codexUsageCache) refresh(ctx context.Context, files []map[string]any, previous map[string]codexUsageResult) map[string]codexUsageResult {
	type job struct {
		key  string
		file map[string]any
	}
	type outcome struct {
		key   string
		value codexUsageResult
	}
	jobs := make(chan job, len(files))
	results := make(chan outcome, len(files))
	seen := make(map[string]bool)
	for _, file := range files {
		if strings.ToLower(stringValue(file["provider"], file["type"])) != "codex" {
			continue
		}
		key := codexUsageKey(file)
		if !seen[key] {
			seen[key] = true
			jobs <- job{key, file}
		}
	}
	close(jobs)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range jobs {
				result := c.fetch(ctx, task.file)
				if !result.Valid {
					result = previous[task.key]
					result.Stale = true
				}
				results <- outcome{task.key, result}
			}
		}()
	}
	workers.Wait()
	close(results)
	current := make(map[string]codexUsageResult, len(seen))
	for result := range results {
		current[result.key] = result.value
	}
	return current
}

func (c *codexUsageCache) fetch(ctx context.Context, file map[string]any) codexUsageResult {
	index := strings.TrimSpace(stringValue(file["auth_index"], file["authIndex"]))
	if c.client == nil || boolValue(file["disabled"]) || !codexUsageIdentityValid(index) || ctx.Err() != nil {
		return codexUsageResult{}
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	headers := map[string]string{
		"Authorization": "Bearer $TOKEN$",
		"Content-Type":  "application/json",
		"Accept":        "application/json",
		"User-Agent":    "codex-tui/0.149.1 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.149.1)",
	}
	if accountID := codexUsageAccountID(file); accountID != "" {
		headers["Chatgpt-Account-Id"] = accountID
	}
	// The visitor cannot choose a destination, method, credential, header or
	// body. CPA substitutes the selected account's token for this GET only.
	body := struct {
		AuthIndex string            `json:"auth_index"`
		Method    string            `json:"method"`
		URL       string            `json:"url"`
		Header    map[string]string `json:"header"`
	}{index, "GET", codexUsageURL, headers}
	var response struct {
		Status int             `json:"status_code"`
		Body   json.RawMessage `json:"body"`
	}
	if err := c.client.PostJSON(ctx, "/v0/management/api-call", body, &response); err != nil || response.Status < 200 || response.Status >= 300 {
		return codexUsageResult{}
	}
	raw := response.Body
	if len(raw) > 0 && raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return codexUsageResult{}
		}
		raw = []byte(text)
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return codexUsageResult{}
	}
	observedAt := c.now().UnixMilli()
	windows, plan, valid := projectCodexUsage(payload, observedAt)
	return codexUsageResult{Windows: windows, Plan: plan, ObservedAt: observedAt, Valid: valid}
}

func codexUsageIdentityValid(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, char := range value {
		if char < 33 || char > 126 || char == '$' {
			return false
		}
	}
	return true
}

func codexUsageAccountID(file map[string]any) string {
	records := []map[string]any{file}
	for _, key := range []string{"metadata", "attributes"} {
		if record, ok := file[key].(map[string]any); ok {
			records = append(records, record)
		}
	}
	for _, record := range records {
		for _, key := range []string{"chatgpt_account_id", "chatgptAccountId", "account_id", "accountId"} {
			value := strings.TrimSpace(stringValue(record[key]))
			if codexUsageIdentityValid(value) {
				return value
			}
		}
	}
	for _, record := range records {
		if token, ok := record["id_token"].(map[string]any); ok {
			for _, key := range []string{"chatgpt_account_id", "chatgptAccountId", "account_id", "accountId"} {
				value := strings.TrimSpace(stringValue(token[key]))
				if codexUsageIdentityValid(value) {
					return value
				}
			}
		}
	}
	return ""
}
