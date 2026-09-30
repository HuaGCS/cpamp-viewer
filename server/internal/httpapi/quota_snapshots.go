package httpapi

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"cpamp-viewer/server/internal/cpamp"
)

const quotaSnapshotPath = "/v0/management/quota-snapshots/query"
const quotaSnapshotTTL = time.Minute
const quotaSnapshotBatchSize = 200

// Keep existing numeric Go callers while representing unknown progress honestly
// on the public wire. No absent provider value becomes an invented zero/full bar.
func (w quotaWindow) MarshalJSON() ([]byte, error) {
	type windowAlias quotaWindow
	var used, remaining *float64
	if !w.UnknownUsed {
		used = &w.Used
	}
	if !w.UnknownRemaining {
		remaining = &w.Remaining
	}
	return json.Marshal(struct {
		windowAlias
		Used      *float64 `json:"used_percent"`
		Remaining *float64 `json:"remaining_percent"`
	}{windowAlias(w), used, remaining})
}

type quotaSnapshotTarget struct {
	AuthFileSnapshot     string `json:"auth_file_snapshot,omitempty"`
	AuthProviderSnapshot string `json:"auth_provider_snapshot"`
	AuthIndex            string `json:"auth_index,omitempty"`
	AccountSnapshot      string `json:"account_snapshot,omitempty"`
	AuthLabelSnapshot    string `json:"auth_label_snapshot,omitempty"`
	AuthProjectID        string `json:"auth_project_id_snapshot,omitempty"`
}

type quotaSnapshotQueryAccount struct {
	RowKey   string              `json:"row_key"`
	Provider string              `json:"provider"`
	Account  quotaSnapshotTarget `json:"account"`
}

type quotaSnapshotResult struct {
	Windows    []quotaWindow
	Plan       string
	ObservedAt int64
	Valid      bool
	Stale      bool
}

type quotaSnapshotCache struct {
	client  *cpamp.Client
	timeout time.Duration
	now     func() time.Time

	mu          sync.Mutex
	pending     chan struct{}
	nextAttempt time.Time
	current     map[string]quotaSnapshotResult
}

func newQuotaSnapshotCache(client *cpamp.Client, timeout time.Duration) *quotaSnapshotCache {
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	return &quotaSnapshotCache{client: client, timeout: timeout, now: time.Now}
}

func canonicalQuotaProvider(value string) string {
	switch value = strings.ToLower(strings.TrimSpace(value)); value {
	case "muse":
		return "meta"
	case "x-ai", "grok":
		return "xai"
	default:
		return value
	}
}

func quotaSnapshotAccount(file map[string]any) (quotaSnapshotQueryAccount, bool) {
	provider := canonicalQuotaProvider(stringValue(file["provider"], file["type"]))
	if provider == "codex" || !isQuotaProvider(provider) {
		return quotaSnapshotQueryAccount{}, false
	}
	// Strong locator evidence is required. Never query a display/email-only
	// account bucket, and never obtain credential bodies or provider tokens.
	name := quotaSnapshotIdentity(stringValue(file["physicalName"], file["name"]))
	index := quotaSnapshotIdentity(stringValue(file["auth_index"], file["authIndex"]))
	if name == "" && index == "" {
		return quotaSnapshotQueryAccount{}, false
	}
	target := quotaSnapshotTarget{
		AuthFileSnapshot: name, AuthIndex: index, AuthProviderSnapshot: provider,
		AccountSnapshot:   quotaSnapshotIdentity(stringValue(file["accountSnapshot"], file["account_snapshot"], file["account"], file["email"])),
		AuthLabelSnapshot: quotaSnapshotIdentity(stringValue(file["authLabelSnapshot"], file["auth_label_snapshot"], file["label"])),
		AuthProjectID:     quotaSnapshotIdentity(stringValue(file["project_id"], file["projectId"])),
	}
	raw, _ := json.Marshal(target)
	return quotaSnapshotQueryAccount{RowKey: pseudonym(string(raw)), Provider: provider, Account: target}, true
}

func quotaSnapshotIdentity(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return ""
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return ""
		}
	}
	return value
}

func (c *quotaSnapshotCache) snapshot(ctx context.Context, files []map[string]any) map[string]quotaSnapshotResult {
	if c == nil || c.client == nil {
		return nil
	}
	accounts := make([]quotaSnapshotQueryAccount, 0, len(files))
	seen := map[string]bool{}
	for _, file := range files {
		if account, ok := quotaSnapshotAccount(file); ok && !seen[account.RowKey] {
			seen[account.RowKey] = true
			accounts = append(accounts, account)
		}
	}
	if len(accounts) == 0 {
		return map[string]quotaSnapshotResult{}
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		c.mu.Lock()
		if c.now().Before(c.nextAttempt) {
			current := selectQuotaSnapshotResults(c.current, accounts)
			c.mu.Unlock()
			return current
		}
		if c.pending == nil {
			c.pending = make(chan struct{})
			previous := c.current
			refreshParent := context.WithoutCancel(ctx)
			go func() {
				refreshCtx, cancel := context.WithTimeout(refreshParent, c.timeout)
				results := c.refresh(refreshCtx, accounts, previous)
				cancel()
				c.mu.Lock()
				c.current = results
				c.nextAttempt = c.now().Add(quotaSnapshotTTL)
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

func selectQuotaSnapshotResults(current map[string]quotaSnapshotResult, accounts []quotaSnapshotQueryAccount) map[string]quotaSnapshotResult {
	selected := make(map[string]quotaSnapshotResult, len(accounts))
	for _, account := range accounts {
		if result, exists := current[account.RowKey]; exists {
			result.Windows = append([]quotaWindow{}, result.Windows...)
			selected[account.RowKey] = result
		}
	}
	return selected
}

func (c *quotaSnapshotCache) refresh(ctx context.Context, accounts []quotaSnapshotQueryAccount, previous map[string]quotaSnapshotResult) map[string]quotaSnapshotResult {
	results := make(map[string]quotaSnapshotResult, len(accounts))
	for start := 0; start < len(accounts); start += quotaSnapshotBatchSize {
		end := min(start+quotaSnapshotBatchSize, len(accounts))
		batch := accounts[start:end]
		var response struct {
			Items *[]struct {
				RowKey   string             `json:"row_key"`
				Provider string             `json:"provider"`
				Windows  *[]json.RawMessage `json:"windows"`
			} `json:"items"`
		}
		// Only this server-built, read-only query is reachable. The sibling
		// /quota-snapshots write endpoint and provider refreshes are never used.
		err := c.client.PostJSON(ctx, quotaSnapshotPath, struct {
			Accounts []quotaSnapshotQueryAccount `json:"accounts"`
		}{batch}, &response)
		found := make(map[string]quotaSnapshotResult, len(batch))
		requested := make(map[string]string, len(batch))
		for _, account := range batch {
			requested[account.RowKey] = account.Provider
		}
		duplicates := map[string]bool{}
		if err == nil && response.Items != nil {
			for _, item := range *response.Items {
				provider, exists := requested[item.RowKey]
				if !exists || provider != canonicalQuotaProvider(item.Provider) || item.Windows == nil {
					continue
				}
				if _, exists := found[item.RowKey]; exists {
					duplicates[item.RowKey] = true
					continue
				}
				found[item.RowKey] = projectStoredQuotaWindows(*item.Windows, provider, c.now())
			}
		}
		for _, account := range batch {
			result, exists := found[account.RowKey]
			if !exists || !result.Valid || duplicates[account.RowKey] {
				result = previous[account.RowKey]
				result.Windows = append([]quotaWindow{}, result.Windows...)
				result.Stale = true
				for index := range result.Windows {
					result.Windows[index].Stale = true
				}
			}
			results[account.RowKey] = result
		}
	}
	return results
}

type storedQuotaWindow struct {
	ID           string   `json:"provider_window_id"`
	Kind         string   `json:"window_kind"`
	Mode         string   `json:"window_mode"`
	ScopeKind    string   `json:"model_scope_kind"`
	ScopeKey     string   `json:"model_scope_key"`
	ModelIDs     []string `json:"model_ids"`
	ObservedAt   int64    `json:"observed_at_ms"`
	ResetAt      *int64   `json:"cycle_end_ms"`
	Duration     *float64 `json:"duration_seconds"`
	Used         *float64 `json:"used_percent"`
	Remaining    *float64 `json:"remaining_percent"`
	Limit        *float64 `json:"limit_value"`
	Plan         string   `json:"plan_type"`
	Stale        bool     `json:"stale"`
	Availability string   `json:"availability"`
	FieldSources map[string]struct {
		ObservedAt int64 `json:"observed_at_ms"`
	} `json:"field_sources"`
}

func projectStoredQuotaWindows(raw []json.RawMessage, provider string, now time.Time) quotaSnapshotResult {
	result := quotaSnapshotResult{Windows: []quotaWindow{}, Valid: true}
	seen := map[string]bool{}
	validRecords := 0
	for _, data := range raw {
		var window storedQuotaWindow
		if json.Unmarshal(data, &window) != nil || window.ID == "" || window.Kind == "" || window.ObservedAt <= 0 {
			continue
		}
		validRecords++
		if window.Availability == "inactive" {
			continue
		}
		if window.Availability != "" && window.Availability != "active" && window.Availability != "unknown" && window.Availability != "pending_absent" {
			validRecords--
			continue
		}
		// A zero financial allowance is not evidence of an xAI monthly quota.
		if provider == "xai" && (window.Kind == "billing" || window.Kind == "monthly" || window.Kind == "payg") && window.Limit != nil && *window.Limit <= 0 {
			continue
		}
		kind := safeQuotaSnapshotText(window.Kind, 40)
		if kind == "" {
			continue
		}
		id := pseudonym(provider + "|" + window.ID + "|" + window.ScopeKind + "|" + window.ScopeKey + "|" + strings.Join(window.ModelIDs, "|"))
		if seen[id] || len(result.Windows) >= 100 {
			continue
		}
		seen[id] = true
		used, hasUsed := validStoredQuotaPercent(window.Used)
		remaining, hasRemaining := validStoredQuotaPercent(window.Remaining)
		if hasUsed && !hasRemaining {
			remaining, hasRemaining = 100-used, true
		}
		if hasUsed && hasRemaining && math.Abs(used+remaining-100) > 0.01 {
			used, remaining, hasUsed, hasRemaining = 0, 0, false, false
		}
		reset := int64(0)
		if window.ResetAt != nil && *window.ResetAt > 0 {
			reset = *window.ResetAt
		}
		minutes := float64(0)
		if window.Duration != nil && !math.IsNaN(*window.Duration) && !math.IsInf(*window.Duration, 0) && *window.Duration > 0 && *window.Duration <= 366*24*3600 {
			minutes = *window.Duration / 60
		}
		if !hasUsed && !hasRemaining && reset == 0 && minutes == 0 {
			continue
		}
		observedAt := window.ObservedAt
		if source, ok := window.FieldSources["quota"]; ok && source.ObservedAt > 0 && (hasUsed || hasRemaining) {
			observedAt = source.ObservedAt
		}
		scope := safeQuotaSnapshotText(window.ScopeKey, 100)
		if window.ScopeKind == "all" {
			scope = ""
		} else if scope == "" {
			for _, model := range window.ModelIDs {
				if safe := safeQuotaSnapshotText(model, 100); safe != "" {
					if scope != "" {
						scope += ", "
					}
					scope += safe
					if len(scope) > 160 {
						break
					}
				}
			}
		}
		stale := window.Stale || window.Availability == "pending_absent" || (reset > 0 && reset <= now.UnixMilli())
		result.Windows = append(result.Windows, quotaWindow{
			ID: id, Label: storedQuotaLabel(kind, minutes), WindowKind: kind, ModelScope: scope,
			Used: used, Remaining: remaining, UnknownUsed: !hasUsed, UnknownRemaining: !hasRemaining,
			WindowMins: minutes, ResetAtMS: reset, ObservedAt: observedAt, Stale: stale,
		})
		if observedAt > result.ObservedAt {
			result.ObservedAt = observedAt
			result.Plan = safeQuotaSnapshotText(window.Plan, 80)
		}
	}
	// Malformed records must not erase a previously successful inventory.
	if len(raw) > 0 && validRecords == 0 {
		result.Valid = false
	}
	sort.Slice(result.Windows, func(i, j int) bool {
		if result.Windows[i].WindowMins == result.Windows[j].WindowMins {
			return result.Windows[i].ID < result.Windows[j].ID
		}
		return result.Windows[i].WindowMins < result.Windows[j].WindowMins
	})
	return result
}

func validStoredQuotaPercent(value *float64) (float64, bool) {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 || *value > 100 {
		return 0, false
	}
	return *value, true
}

func safeQuotaSnapshotText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit || cleanText(value, limit) != value || strings.Contains(value, "\\") || strings.Contains(value, "://") || strings.HasPrefix(value, "/") || strings.Contains(value, "@") || value == "敏感错误详情已隐藏" || value == "内部错误详情已隐藏" {
		return ""
	}
	return value
}

func storedQuotaLabel(kind string, minutes float64) string {
	switch kind {
	case "daily":
		return "每日额度"
	case "weekly":
		return "周额度"
	case "monthly":
		return "月额度"
	case "billing":
		return "账单额度"
	case "payg":
		return "按量付费额度"
	case "product":
		return "产品额度"
	case "summary":
		return "已观测额度"
	default:
		return quotaWindowLabel(minutes, "额度窗口")
	}
}

func findCredentialQuotaSnapshot(items []map[string]any, provider, name, index string) map[string]any {
	for _, item := range items {
		itemProvider := canonicalQuotaProvider(stringValue(item["auth_provider_snapshot"], item["provider"]))
		if itemProvider != "" && itemProvider != provider {
			continue
		}
		itemName, itemIndex := stringValue(item["auth_file_snapshot"]), stringValue(item["auth_index"])
		if (name != "" && itemName != "" && name != itemName) || (index != "" && itemIndex != "" && index != itemIndex) {
			continue
		}
		if (name != "" && itemName == name) || (index != "" && itemIndex == index) {
			return item
		}
	}
	return nil
}
