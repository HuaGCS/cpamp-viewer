package httpapi

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A quota observation contains complete snapshots of several independent pools.
// Model-state keys identify the request, not the pools carried in its headers.
type codexPoolObservation struct {
	pool, family, plan string
	observedAt         int64
	windows            []quotaWindow
}

func projectCodexQuota(file map[string]any, snapshots []map[string]any) ([]quotaWindow, string, int64) {
	var observations []codexPoolObservation
	if quota, ok := file["quota"].(map[string]any); ok {
		observations = append(observations, codexSignalPools(quota)...)
	}
	if models, ok := file["model_quotas"].(map[string]any); ok {
		// Stable traversal handles duplicated account/model observations deterministically.
		names := make([]string, 0, len(models))
		for name := range models {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if quota, ok := models[name].(map[string]any); ok {
				observations = append(observations, codexSignalPools(quota)...)
			}
		}
	}
	sort.SliceStable(observations, func(i, j int) bool { return observations[i].observedAt > observations[j].observedAt })
	windows := make([]quotaWindow, 0)
	seen := make(map[string]bool)
	plan, observedAt := "", int64(0)
	for _, observation := range observations {
		if seen[observation.family] || len(observation.windows) == 0 {
			continue
		}
		// Replace a whole pool. A newer weekly-only pool must not inherit an old 5H window.
		seen[observation.family] = true
		windows = append(windows, observation.windows...)
		if plan == "" {
			plan = observation.plan
		}
		if observation.observedAt > observedAt {
			observedAt = observation.observedAt
		}
	}
	if len(windows) == 0 {
		snapshot := findCodexQuotaSnapshot(file, snapshots)
		windows, plan, observedAt = projectQuotaSnapshot(snapshot)
		metadata, _ := snapshot["response_metadata"].(map[string]any)
		quota, _ := metadata["quota"].(map[string]any)
		pool := codexRootPool(stringValue(quota["active_limit"]))
		for i := range windows {
			if windows[i].ID == "observed" {
				pool = "unknown"
			}
			windows[i].Pool = pool
			windows[i].ID = pool + "-" + windows[i].ID
		}
	}
	sort.SliceStable(windows, func(i, j int) bool {
		rank := map[string]int{"codex_main": 0, "codex_spark": 1, "codex_review": 2, "unknown": 3}
		if rank[windows[i].Pool] != rank[windows[j].Pool] {
			return rank[windows[i].Pool] < rank[windows[j].Pool]
		}
		if windows[i].WindowMins != windows[j].WindowMins {
			if windows[i].WindowMins == 0 {
				return false
			}
			if windows[j].WindowMins == 0 {
				return true
			}
			return windows[i].WindowMins < windows[j].WindowMins
		}
		return windows[i].ID < windows[j].ID
	})
	return windows, plan, observedAt
}

func codexSignalPools(observation map[string]any) []codexPoolObservation {
	observedAt := quotaTimestampMS(observation["observed_at"])
	if observedAt == 0 {
		observedAt = intValue(observation["observed_at_ms"])
	}
	if observedAt <= 0 {
		return nil
	}
	headers := make(map[string]string)
	add := func(key, value string) {
		key = strings.ToLower(strings.TrimSpace(key))
		if !strings.HasPrefix(key, "x-codex-") {
			return
		}
		value = strings.TrimSpace(value)
		if previous, exists := headers[key]; exists && previous != value {
			value = ""
		}
		headers[key] = value
	}
	switch signals := observation["signals"].(type) {
	case map[string]any:
		for key, value := range signals {
			if text, ok := value.(string); ok {
				add(key, text)
			}
		}
	case map[string]string:
		for key, value := range signals {
			add(key, value)
		}
	}
	prefixes := make(map[string]bool)
	for key := range headers {
		for _, suffix := range []string{"primary-used-percent", "secondary-used-percent"} {
			if strings.HasSuffix(key, suffix) {
				prefixes[strings.TrimSuffix(key, suffix)] = true
			}
		}
	}
	ordered := make([]string, 0, len(prefixes))
	for prefix := range prefixes {
		ordered = append(ordered, prefix)
	}
	sort.Strings(ordered)
	result := make([]codexPoolObservation, 0, len(ordered))
	for _, prefix := range ordered {
		pool := "unknown"
		if prefix == "x-codex-" {
			pool = codexRootPool(headers["x-codex-active-limit"])
		} else {
			identity := headers[prefix+"limit-name"]
			if identity == "" {
				identity = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(prefix, "x-codex-"), "additional-"), "-")
			}
			pool = codexNamedPool(identity)
		}
		family := pool
		if pool == "unknown" {
			family += ":" + prefix
		}
		item := codexPoolObservation{pool: pool, family: family, plan: cleanText(headers["x-codex-plan-type"], 80), observedAt: observedAt}
		seen := make(map[string]int)
		conflicts := make(map[string]bool)
		for _, slot := range []string{"primary", "secondary"} {
			used, ok := quotaNumber(headers[prefix+slot+"-used-percent"])
			if !ok || used < 0 || used > 100 {
				continue
			}
			minutes, validMinutes := quotaNumber(headers[prefix+slot+"-window-minutes"])
			if !validMinutes || minutes < 0 {
				minutes = 0
			}
			resetAt := quotaTimestampMS(headers[prefix+slot+"-reset-at"])
			relativeReset, validReset := quotaNumber(headers[prefix+slot+"-reset-after-seconds"])
			if used == 0 && validMinutes && minutes == 0 && resetAt == 0 && (!validReset || relativeReset <= 0) {
				continue // Explicitly empty secondary slot, not a measured 100% quota.
			}
			if resetAt == 0 {
				resetAt = quotaRelativeReset(observedAt, headers[prefix+slot+"-reset-after-seconds"])
			}
			identity := slot
			if minutes > 0 {
				identity = strconv.FormatFloat(minutes, 'f', -1, 64) + "m"
			}
			id := pool + "-" + identity
			if pool == "unknown" {
				id = "unknown-" + pseudonym(family)[5:] + "-" + identity
			}
			window := quotaWindow{ID: id, Pool: pool, Label: quotaWindowLabel(minutes, "额度窗口"), Used: used, Remaining: 100 - used, WindowMins: minutes, ResetAtMS: resetAt, ObservedAt: observedAt}
			if index, exists := seen[id]; exists {
				if item.windows[index] != window {
					conflicts[id] = true
				}
				continue
			}
			seen[id] = len(item.windows)
			item.windows = append(item.windows, window)
		}
		valid := item.windows[:0]
		for _, window := range item.windows {
			if !conflicts[window.ID] {
				valid = append(valid, window)
			}
		}
		item.windows = valid
		if len(item.windows) > 0 {
			result = append(result, item)
		}
	}
	// A flattened WS active pool can mirror an explicit namespace. Do not
	// repeat that ambiguous copy as an additional quota pool in the fallback.
	filtered := make([]codexPoolObservation, 0, len(result))
	for _, item := range result {
		duplicate := false
		if item.family == "unknown:x-codex-" {
			for _, known := range result {
				if known.pool != "unknown" && known.pool == codexNamedPool(headers["x-codex-active-limit"]) && codexSamePoolWindows(item.windows, known.windows) {
					duplicate = true
					break
				}
			}
		}
		if !duplicate {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func codexSamePoolWindows(left, right []quotaWindow) bool {
	if len(left) == 0 || len(left) != len(right) {
		return false
	}
	for i := range left {
		a, b := left[i], right[i]
		if a.Used != b.Used || a.WindowMins != b.WindowMins || a.ResetAtMS != b.ResetAtMS || a.ObservedAt != b.ObservedAt {
			return false
		}
	}
	return true
}

func codexNamedPool(value string) string {
	identity := strings.NewReplacer("-", "", "_", "", ".", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(value)))
	switch identity {
	case "spark", "codexspark", "gpt53codexspark", "bengalfox", "codexbengalfox":
		return "codex_spark"
	case "codereview", "codexcodereview":
		return "codex_review"
	case "main", "codex", "codexmain", "premium":
		return "codex_main"
	default:
		return "unknown"
	}
}

func codexRootPool(active string) string {
	// HTTP defaults to the Codex pool; WS rate_limits belongs to its metered
	// limit. Flattened passive signals lack transport provenance, so additional
	// namespaces alone cannot prove that their generic root is the main pool.
	if strings.TrimSpace(active) == "" || codexNamedPool(active) == "codex_main" {
		return "codex_main"
	}
	// A flattened observation cannot distinguish the HTTP default family from
	// a WS metered pool here. Only a complete usage query can resolve it.
	return "unknown"
}

func quotaNumber(value any) (float64, bool) {
	var text string
	switch n := value.(type) {
	case string:
		text = strings.TrimSpace(n)
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		if n, ok := value.(interface{ String() string }); ok {
			text = n.String()
		}
	}
	if text == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(text, 64)
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}

func quotaTimestampMS(value any) int64 {
	if text, ok := value.(string); ok {
		if timestamp, err := time.Parse(time.RFC3339Nano, text); err == nil && timestamp.UnixMilli() > 0 {
			return timestamp.UnixMilli()
		}
	}
	n, ok := quotaNumber(value)
	if !ok || n <= 0 {
		return 0
	}
	if n < 1e12 {
		n *= 1000
	}
	if n >= float64(math.MaxInt64) {
		return 0
	}
	return int64(n)
}

func quotaRelativeReset(observedAt int64, value any) int64 {
	seconds, ok := quotaNumber(value)
	if !ok || seconds < 0 || observedAt <= 0 || seconds*1000 >= float64(math.MaxInt64-observedAt) {
		return 0
	}
	return observedAt + int64(seconds*1000)
}

func findCodexQuotaSnapshot(file map[string]any, snapshots []map[string]any) map[string]any {
	name := stringValue(file["name"], file["id"])
	index := stringValue(file["auth_index"], file["authIndex"])
	var best map[string]any
	bestRank := 0
	for _, snapshot := range snapshots {
		provider := strings.ToLower(stringValue(snapshot["auth_provider_snapshot"], snapshot["provider"]))
		if provider != "" && provider != "codex" {
			continue
		}
		otherIndex := stringValue(snapshot["auth_index"])
		otherName := stringValue(snapshot["auth_file_snapshot"])
		rank := 0
		if index != "" && otherIndex != "" {
			if index != otherIndex {
				continue
			}
			rank = 2
		} else if name != "" && name == otherName {
			rank = 1
		}
		if rank == 0 {
			continue
		} // A shared email/display name is not credential identity.
		if rank > bestRank || (rank == bestRank && intValue(snapshot["timestamp_ms"]) > intValue(best["timestamp_ms"])) {
			best, bestRank = snapshot, rank
		}
	}
	return best
}
