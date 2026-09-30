import type {
  DashboardModelCostRank,
  DashboardSummaryResponse,
  DashboardTodayRequestHealthTimelinePoint,
  DashboardTopModel,
  DashboardTrafficPoint,
} from "@/services/api/usageService";
import type { ViewerDashboardResponse } from "@/viewer/model/viewerTypes";

export type ViewerDashboardReadOnlyProjection = ViewerDashboardResponse & {
  system?: ViewerDashboardResponse["system"] & {
    cpa_base?: string;
  };
  stats?: {
    management_keys?: number;
    auth_files?: number;
    available_models?: number | null;
    providers?: {
      gemini?: number;
      codex?: number;
      xai?: number;
      meta?: number;
      claude?: number;
      openai?: number;
      total?: number;
    };
  };
  config?: {
    debug?: boolean;
    logging_to_file?: boolean;
    request_retry?: number;
    ws_auth?: boolean;
    routing_strategy?: string;
    proxy_url?: string;
  };
  collector?: {
    service?: string;
    events?: number;
    deadLetters?: number;
    collector?: {
      collector?: string;
      upstream?: string;
      mode?: string;
      transport?: string;
      queue?: string;
      lastConsumedAt?: number;
      lastInsertedAt?: number;
      totalInserted?: number;
      totalSkipped?: number;
      deadLetters?: number;
      lastError?: string;
    };
  };
};

const numberOrZero = (value: number | null | undefined) => {
  const number = Number(value ?? 0);
  return Number.isFinite(number) ? number : 0;
};

const positiveNumberOr = (
  value: number | null | undefined,
  fallback: number,
) => {
  const number = Number(value);
  return Number.isFinite(number) && number > 0 ? number : fallback;
};

const normalizeHealthTone = (tone: string | undefined) => {
  if (tone === "success") return "good";
  if (tone === "warning") return "warn";
  if (tone === "failure") return "bad";
  return tone || "empty";
};

export function normalizeViewerDashboard(
  data: ViewerDashboardResponse | null,
): DashboardSummaryResponse {
  const generatedAt = positiveNumberOr(data?.generated_at_ms, Date.now());
  const now = positiveNumberOr(data?.window?.now_ms, generatedAt);
  const todayStart = positiveNumberOr(
    data?.window?.today_start_ms,
    new Date(new Date(now).setHours(0, 0, 0, 0)).getTime(),
  );
  const rolling30mStart = positiveNumberOr(
    data?.window?.rolling_30m_start_ms,
    now - 30 * 60 * 1000,
  );
  const today = data?.today || {};
  const rolling = data?.rolling_30m || {};
  const rawTopModels = data?.top_models_today ?? data?.model_cost_rank ?? [];
  const rawModelCostRank = data?.model_cost_rank || [];
  const totalModelCost = rawModelCostRank.reduce(
    (sum, item) => sum + numberOrZero(item.cost),
    0,
  );
  const topModels: DashboardTopModel[] = rawTopModels.map((item) => ({
    model: item.model,
    calls: numberOrZero(item.calls),
    tokens: numberOrZero(item.tokens ?? item.total_tokens),
    cost: numberOrZero(item.cost),
    success_rate: numberOrZero(item.success_rate),
  }));
  const modelCostRank: DashboardModelCostRank[] = rawModelCostRank.map(
    (item) => {
      const cost = numberOrZero(item.cost);
      return {
        model: item.model,
        calls: numberOrZero(item.calls),
        tokens: numberOrZero(item.tokens ?? item.total_tokens),
        cost,
        success_rate: numberOrZero(item.success_rate),
        cost_share:
          item.cost_share === undefined
            ? totalModelCost > 0
              ? cost / totalModelCost
              : 0
            : numberOrZero(item.cost_share),
      };
    },
  );
  const trafficTimeline: DashboardTrafficPoint[] = (
    data?.traffic_timeline || []
  ).map((point) => {
    const calls = numberOrZero(point.calls);
    const tokens = numberOrZero(point.tokens ?? point.total_tokens);
    const success = numberOrZero(point.success);
    const failure = numberOrZero(point.failure);
    return {
      bucket_ms: point.bucket_ms,
      calls,
      tokens,
      success,
      failure,
      calls_share: numberOrZero(point.calls_share),
      tokens_share: numberOrZero(point.tokens_share),
      failure_rate:
        point.failure_rate === undefined
          ? calls > 0
            ? failure / calls
            : 0
          : numberOrZero(point.failure_rate),
    };
  });
  const healthPoints: DashboardTodayRequestHealthTimelinePoint[] = (
    data?.today_request_health_timeline?.points || []
  ).map((point) => {
    const calls = numberOrZero(point.calls);
    const success = numberOrZero(point.success);
    const failure = numberOrZero(point.failure);
    return {
      bucket_ms: point.bucket_ms,
      calls,
      tokens: numberOrZero(point.tokens ?? point.total_tokens),
      success,
      failure,
      success_rate:
        point.success_rate === undefined
          ? calls > 0
            ? success / calls
            : 0
          : numberOrZero(point.success_rate),
      failure_rate:
        point.failure_rate === undefined
          ? calls > 0
            ? failure / calls
            : 0
          : numberOrZero(point.failure_rate),
      tone: normalizeHealthTone(point.tone),
      intensity: numberOrZero(point.intensity),
      future: Boolean(point.future),
    };
  });
  const healthSuccess = numberOrZero(
    data?.today_request_health_timeline?.success_calls,
  );
  const healthFailure = numberOrZero(
    data?.today_request_health_timeline?.failure_calls,
  );
  const healthTotal =
    numberOrZero(data?.today_request_health_timeline?.total_calls) ||
    healthSuccess + healthFailure;

  return {
    generated_at_ms: generatedAt,
    window: {
      today_start_ms: todayStart,
      now_ms: now,
      rolling_30m_start_ms: rolling30mStart,
    },
    today: {
      total_calls: numberOrZero(today.total_calls),
      success_calls: numberOrZero(today.success_calls),
      failure_calls: numberOrZero(today.failure_calls),
      success_rate: numberOrZero(today.success_rate),
      input_tokens: numberOrZero(today.input_tokens),
      output_tokens: numberOrZero(today.output_tokens),
      cached_tokens: numberOrZero(today.cached_tokens),
      cache_read_tokens: numberOrZero(today.cache_read_tokens),
      cache_creation_tokens: numberOrZero(today.cache_creation_tokens),
      reasoning_tokens: numberOrZero(today.reasoning_tokens),
      total_tokens: numberOrZero(today.total_tokens),
      total_cost: numberOrZero(today.total_cost),
      average_latency_ms:
        today.average_latency_ms === undefined
          ? null
          : today.average_latency_ms,
      zero_token_calls: numberOrZero(today.zero_token_calls),
    },
    rolling_30m: {
      rpm: numberOrZero(rolling.rpm),
      tpm: numberOrZero(rolling.tpm),
      total_calls: numberOrZero(rolling.total_calls),
      total_tokens: numberOrZero(rolling.total_tokens),
    },
    top_models_today: topModels,
    model_cost_rank: modelCostRank,
    traffic_timeline: trafficTimeline,
    hourly_activity: [],
    today_request_health_timeline: data?.today_request_health_timeline
      ? {
          from_ms: data.today_request_health_timeline.from_ms || todayStart,
          to_ms: data.today_request_health_timeline.to_ms || now,
          bucket_ms:
            data.today_request_health_timeline.bucket_ms || 10 * 60 * 1000,
          success_calls: healthSuccess,
          failure_calls: healthFailure,
          total_calls: healthTotal,
          success_rate:
            data.today_request_health_timeline.success_rate === undefined
              ? healthTotal > 0
                ? healthSuccess / healthTotal
                : 0
              : numberOrZero(data.today_request_health_timeline.success_rate),
          points: healthPoints,
        }
      : undefined,
    token_mix: (data?.token_mix || []).map((item) => ({
      key: item.key,
      tokens: numberOrZero(item.tokens),
      share: numberOrZero(item.share),
    })),
    channel_health: [],
    failure_sources: [],
    recent_failures: [],
  };
}

export function getViewerDashboardCounts(
  data: ViewerDashboardReadOnlyProjection | null,
) {
  const providers = data?.stats?.providers;
  const providerTotal =
    providers?.total ??
    numberOrZero(providers?.gemini) +
      numberOrZero(providers?.codex) +
      numberOrZero(providers?.xai) +
      numberOrZero(providers?.meta) +
      numberOrZero(providers?.claude) +
      numberOrZero(providers?.openai);

  return {
    accessKeyCount: numberOrZero(data?.stats?.management_keys),
    authFiles: numberOrZero(data?.stats?.auth_files),
    availableModels: data?.stats?.available_models ?? null,
    providerTotal,
    providers: {
      gemini: numberOrZero(providers?.gemini),
      codex: numberOrZero(providers?.codex),
      xai: numberOrZero(providers?.xai),
      meta: numberOrZero(providers?.meta),
      claude: numberOrZero(providers?.claude),
      openai: numberOrZero(providers?.openai),
    },
  };
}
