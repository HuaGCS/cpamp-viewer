import type { AccountQuotaEntry } from "@/features/monitoring/components/accountOverviewPresentation";
import type { ViewerQuotaAccount } from "./viewerTypes";

const toQuotaProvider = (value: string): AccountQuotaEntry["provider"] => {
  const provider = value.trim().toLocaleLowerCase();
  if (
    provider === "antigravity" ||
    provider === "claude" ||
    provider === "kimi" ||
    provider === "xai" ||
    provider === "codex" ||
    provider === "meta" ||
    provider === "devin"
  ) {
    return provider;
  }
  return "unknown";
};

export const buildViewerMonitoringQuotaEntry = (
  account: ViewerQuotaAccount,
  locale: string,
): AccountQuotaEntry => ({
  key: account.id,
  provider: toQuotaProvider(account.provider),
  providerLabel: account.provider || "--",
  authLabel: account.display_name,
  fileName: account.display_name,
  planType: account.plan || null,
  metaLabels: account.status_message ? [account.status_message] : undefined,
  windows: (account.windows ?? []).map((window) => ({
    id: window.id,
    label: window.label,
    remainingPercent:
      typeof window.remaining_percent === "number" &&
      Number.isFinite(window.remaining_percent)
        ? window.remaining_percent
        : null,
    resetLabel: window.reset_at_ms
      ? new Date(window.reset_at_ms).toLocaleString(locale)
      : "--",
    usageLabel: null,
  })),
  fetchedAtMs: account.updated_at_ms,
  observedAtMs: account.updated_at_ms,
  observedFromUsageHeaders: true,
});
