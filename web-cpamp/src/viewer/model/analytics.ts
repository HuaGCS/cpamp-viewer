import type {
  ViewerAlias,
  ViewerAnalyticsRequest,
  ViewerAnalyticsResponse,
  ViewerApiKeyStat,
} from "./viewerTypes";

export type ViewerTimeRange =
  "24h" | "today" | "yesterday" | "7d" | "14d" | "30d" | "1y" | "custom";
export type ViewerGranularity = "auto" | "hour" | "day";

export type CustomRange = {
  fromMs: number;
  toMs: number;
};

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;

export const isViewerKeyId = (value: string) =>
  /^view_[a-f0-9]{12}$/i.test(value.trim());

export const getRangeBounds = (
  range: ViewerTimeRange,
  nowMs = Date.now(),
  customRange?: CustomRange | null,
) => {
  const now = new Date(nowMs);
  if (
    range === "custom" &&
    customRange &&
    customRange.fromMs > 0 &&
    customRange.toMs > customRange.fromMs
  ) {
    return customRange;
  }
  if (range === "today") {
    const start = new Date(now);
    start.setHours(0, 0, 0, 0);
    return { fromMs: start.getTime(), toMs: nowMs };
  }
  if (range === "yesterday") {
    const end = new Date(now);
    end.setHours(0, 0, 0, 0);
    return { fromMs: end.getTime() - DAY_MS, toMs: end.getTime() };
  }
  const duration =
    range === "7d"
      ? 7 * DAY_MS
      : range === "14d"
        ? 14 * DAY_MS
        : range === "30d"
          ? 30 * DAY_MS
          : range === "1y"
            ? 365 * DAY_MS
            : 24 * HOUR_MS;
  return { fromMs: Math.max(1, nowMs - duration), toMs: nowMs };
};

export const resolveGranularity = (
  granularity: ViewerGranularity,
  bounds: { fromMs: number; toMs: number },
) => {
  if (granularity !== "auto") return granularity;
  return bounds.toMs - bounds.fromMs > 7 * DAY_MS ? "day" : "hour";
};

export const buildAnalyticsRequest = ({
  range,
  customRange,
  granularity,
  searchQuery,
  filters,
  include,
  nowMs = Date.now(),
}: {
  range: ViewerTimeRange;
  customRange?: CustomRange | null;
  granularity: ViewerGranularity;
  searchQuery?: string;
  filters?: Record<string, unknown>;
  include: Record<string, unknown>;
  nowMs?: number;
}): ViewerAnalyticsRequest => {
  const bounds = getRangeBounds(range, nowMs, customRange);
  const resolvedGranularity = resolveGranularity(granularity, bounds);
  const request: ViewerAnalyticsRequest = {
    from_ms: bounds.fromMs,
    to_ms: bounds.toMs,
    now_ms: nowMs,
    time_zone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
    include: { ...include, granularity: resolvedGranularity },
  };
  const query = searchQuery?.trim();
  if (query) request.search_query = query;
  if (filters && Object.keys(filters).length > 0) request.filters = filters;
  return request;
};

export const compactNumber = (value: number | null | undefined) => {
  const number = Number(value ?? 0);
  if (!Number.isFinite(number)) return "0";
  return new Intl.NumberFormat("zh-CN", {
    notation: Math.abs(number) >= 10_000 ? "compact" : "standard",
    maximumFractionDigits: 1,
  }).format(number);
};

export const formatMoney = (value: number | null | undefined) => {
  const number = Number(value ?? 0);
  return `$${number.toLocaleString("en-US", {
    minimumFractionDigits: number > 0 && number < 0.01 ? 4 : 2,
    maximumFractionDigits: number > 0 && number < 0.01 ? 6 : 2,
  })}`;
};

export const normalizeRate = (value: number | null | undefined) => {
  const number = Number(value ?? 0);
  if (!Number.isFinite(number)) return 0;
  return Math.min(1, Math.max(0, number > 1 ? number / 100 : number));
};

export const formatPercent = (value: number | null | undefined, digits = 1) =>
  `${(normalizeRate(value) * 100).toFixed(digits)}%`;

export const formatDuration = (value: number | null | undefined) => {
  const number = Number(value ?? 0);
  if (!Number.isFinite(number) || number <= 0) return "--";
  if (number >= 1000)
    return `${(number / 1000).toFixed(number >= 10_000 ? 1 : 2)}s`;
  return `${Math.round(number)}ms`;
};

export const formatDateTime = (value: number | null | undefined) => {
  if (!value || !Number.isFinite(value)) return "--";
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
};

export const aliasMapFrom = (items: ViewerAlias[]) =>
  new Map(
    items
      .filter((item) => isViewerKeyId(item.id))
      .map((item) => [item.id, item.alias]),
  );

export const getApiKeyId = (row: ViewerApiKeyStat) =>
  row.api_key_id || row.id || "";

export const getApiKeyLabel = (
  row: Pick<ViewerApiKeyStat, "id" | "api_key_id" | "api_key_alias">,
  aliases: Map<string, string>,
) => {
  const id = row.api_key_id || row.id || "";
  return (
    row.api_key_alias ||
    aliases.get(id) ||
    (id ? `Key ${id.slice(-6)}` : "未识别 Key")
  );
};

export const hasAnalyticsData = (
  data: ViewerAnalyticsResponse | null | undefined,
) =>
  Number(data?.summary?.total_calls ?? 0) > 0 ||
  Boolean(data?.timeline?.length) ||
  Boolean(data?.events?.items?.length);

export const clampPercent = (value: number | null | undefined) =>
  Math.min(100, Math.max(0, Number(value ?? 0)));
