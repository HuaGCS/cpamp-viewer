package httpapi

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// projectCodexUsage projects a complete /wham/usage inventory. A successful
// empty inventory is authoritative: callers must not append historical headers.
func projectCodexUsage(payload map[string]any, observedAt int64) ([]quotaWindow, string, bool) {
	windows := make([]quotaWindow, 0)
	if observedAt <= 0 {
		return windows, "", false
	}
	planValue, _ := codexUsageField(payload, "plan_type", "planType")
	plan := cleanText(stringValue(planValue), 80)
	foundInventory := false
	for _, spec := range []struct {
		keys []string
		pool string
	}{
		{[]string{"rate_limit", "rateLimit"}, "codex_main"},
		{[]string{"code_review_rate_limit", "codeReviewRateLimit"}, "codex_review"},
	} {
		value, exists := codexUsageField(payload, spec.keys...)
		if !exists {
			continue
		}
		foundInventory = true
		projected, valid := codexUsageRateLimit(value, spec.pool, spec.pool, observedAt)
		if !valid {
			return []quotaWindow{}, "", false
		}
		windows = append(windows, projected...)
	}
	if value, exists := codexUsageField(payload, "additional_rate_limits", "additionalRateLimits"); exists {
		foundInventory = true
		entries, valid := codexUsageAdditionalEntries(value)
		if !valid {
			return []quotaWindow{}, "", false
		}
		for _, entry := range entries {
			pool, family := codexUsageAdditionalIdentity(entry.value, entry.key)
			rate, exists := codexUsageField(entry.value, "rate_limit", "rateLimit")
			if !exists {
				// Object mappings may directly contain a rate-limit object.
				if !codexUsageHasWindowFields(entry.value) {
					return []quotaWindow{}, "", false
				}
				rate = entry.value
			}
			projected, valid := codexUsageRateLimit(rate, pool, family, observedAt)
			if !valid {
				return []quotaWindow{}, "", false
			}
			windows = append(windows, projected...)
		}
	}
	if !foundInventory {
		return []quotaWindow{}, "", false
	}

	// Identical aliases are one window. Conflicting observations with the same
	// confirmed pool and duration must not win based on JSON array order.
	unique := make(map[string]quotaWindow, len(windows))
	conflicts := make(map[string]bool)
	for _, window := range windows {
		if previous, exists := unique[window.ID]; exists && previous != window {
			conflicts[window.ID] = true
		}
		unique[window.ID] = window
	}
	windows = windows[:0]
	for id, window := range unique {
		if !conflicts[id] {
			windows = append(windows, window)
		}
	}
	ranks := map[string]int{"codex_main": 0, "codex_spark": 1, "codex_review": 2, "unknown": 3}
	sort.Slice(windows, func(i, j int) bool {
		left, right := windows[i], windows[j]
		if ranks[left.Pool] != ranks[right.Pool] {
			return ranks[left.Pool] < ranks[right.Pool]
		}
		if left.WindowMins != right.WindowMins {
			if left.WindowMins == 0 {
				return false
			}
			if right.WindowMins == 0 {
				return true
			}
			return left.WindowMins < right.WindowMins
		}
		return left.ID < right.ID
	})
	return windows, plan, true
}

// Match the upstream's snake_case ?? camelCase behavior, including explicit null.
func codexUsageField(object map[string]any, keys ...string) (any, bool) {
	exists := false
	for _, key := range keys {
		if value, found := object[key]; found {
			exists = true
			if value != nil {
				return value, true
			}
		}
	}
	return nil, exists
}

func codexUsageRateLimit(value any, pool, family string, observedAt int64) ([]quotaWindow, bool) {
	windows := make([]quotaWindow, 0, 2)
	if value == nil {
		return windows, true
	}
	rate, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	for _, slot := range []string{"primary", "secondary"} {
		value, _ := codexUsageField(rate, slot+"_window", slot+"Window")
		if value == nil {
			continue
		}
		window, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		usedValue, _ := codexUsageField(window, "used_percent", "usedPercent")
		used, validUsed := quotaNumber(usedValue)
		if !validUsed || used < 0 || used > 100 {
			continue
		}
		durationValue, _ := codexUsageField(window, "limit_window_seconds", "limitWindowSeconds")
		seconds, validDuration := quotaNumber(durationValue)
		minutes := float64(0)
		if validDuration && seconds > 0 {
			minutes = seconds / 60
		}
		resetValue, _ := codexUsageField(window, "reset_at", "resetAt")
		resetAt := quotaTimestampMS(resetValue)
		relativeValue, _ := codexUsageField(window, "reset_after_seconds", "resetAfterSeconds")
		relative, validRelative := quotaNumber(relativeValue)
		hasResetSignal := resetAt > 0 || (validRelative && relative > 0)
		if !hasResetSignal && ((validDuration && seconds == 0) || (minutes == 0 && used == 0)) {
			// Disabled/absent provider slots often contain zeros. They are not
			// evidence of another available 100% quota window.
			continue
		}
		if resetAt == 0 {
			resetAt = quotaRelativeReset(observedAt, relativeValue)
		}
		identity := slot
		if minutes > 0 {
			identity = strconv.FormatFloat(minutes, 'f', -1, 64) + "m"
		}
		id := pool + "-" + identity
		if pool == "unknown" {
			id = "unknown-" + pseudonym("codex-usage:" + family)[5:] + "-" + identity
		}
		windows = append(windows, quotaWindow{
			ID: id, Pool: pool, Label: quotaWindowLabel(minutes, "额度窗口"),
			Used: used, Remaining: 100 - used, WindowMins: minutes,
			ResetAtMS: resetAt, ObservedAt: observedAt,
		})
	}
	return windows, true
}

type codexUsageAdditionalEntry struct {
	key   string
	value map[string]any
}

func codexUsageHasWindowFields(value map[string]any) bool {
	for _, key := range []string{"primary_window", "primaryWindow", "secondary_window", "secondaryWindow"} {
		if _, exists := value[key]; exists {
			return true
		}
	}
	return false
}

func codexUsageAdditionalEntries(value any) ([]codexUsageAdditionalEntry, bool) {
	entries := make([]codexUsageAdditionalEntry, 0)
	add := func(key string, value any) bool {
		if value == nil {
			return true
		}
		entry, ok := value.(map[string]any)
		if !ok {
			return false
		}
		entries = append(entries, codexUsageAdditionalEntry{key: key, value: entry})
		return true
	}
	switch typed := value.(type) {
	case nil:
		return entries, true
	case []any:
		for index, entry := range typed {
			if !add(strconv.Itoa(index), entry) {
				return nil, false
			}
		}
	case map[string]any:
		if _, exists := codexUsageField(typed, "rate_limit", "rateLimit", "metered_feature", "meteredFeature", "limit_name", "limitName"); exists || codexUsageHasWindowFields(typed) {
			return []codexUsageAdditionalEntry{{key: "single", value: typed}}, true
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !add(key, typed[key]) {
				return nil, false
			}
		}
	default:
		return nil, false
	}
	return entries, true
}

func codexUsageAdditionalIdentity(entry map[string]any, key string) (string, string) {
	featureValue, _ := codexUsageField(entry, "metered_feature", "meteredFeature")
	nameValue, _ := codexUsageField(entry, "limit_name", "limitName")
	feature, validFeature := featureValue.(string)
	name, _ := nameValue.(string)
	feature, name = strings.TrimSpace(feature), strings.TrimSpace(name)
	identity := feature
	if featureValue == nil || (validFeature && feature == "") {
		identity = name
	}
	if codexNamedPool(identity) == "codex_spark" {
		return "codex_spark", "codex_spark"
	}
	// Unknown feature names are only inputs to a one-way public window ID.
	family, _ := json.Marshal([]any{featureValue, nameValue, key})
	return "unknown", string(family)
}
