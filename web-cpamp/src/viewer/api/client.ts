import type {
  AliasResponse,
  AnalyticsRequest,
  AnalyticsResponse,
  DashboardResponse,
  MaintenanceResponse,
  ModelPricesResponse,
  QuotaResponse,
  KeyQuotaResponse,
  SessionResponse,
} from "./types";
import type { ModelPriceStatusResponse } from "./modelPriceStatus";
import type { UsageStatusResponse } from "./usageStatus";

const API_BASE = "/viewer/api/v1";

let csrfToken = "";
let authenticationFailureHandler: (() => void) | null = null;

export class ViewerApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = "ViewerApiError";
    this.status = status;
  }
}

export const setAuthenticationFailureHandler = (
  handler: (() => void) | null,
) => {
  authenticationFailureHandler = handler;
};

const readErrorMessage = (payload: unknown, status: number) => {
  if (payload && typeof payload === "object" && !Array.isArray(payload)) {
    const message = (payload as Record<string, unknown>).error;
    if (typeof message === "string" && message.trim()) {
      return message.trim();
    }
  }
  return `请求失败 (${status})`;
};

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  const method = (init.method ?? "GET").toUpperCase();

  if (init.body) {
    headers.set("Content-Type", "application/json");
  }
  if (method !== "GET" && method !== "HEAD" && csrfToken) {
    headers.set("X-CSRF-Token", csrfToken);
  }

  const response = await fetch(`${API_BASE}${path}`, {
    ...init,
    method,
    headers,
    credentials: "same-origin",
  });
  const payload: unknown = await response.json().catch(() => ({}));

  if (!response.ok) {
    const message = readErrorMessage(payload, response.status);
    if (response.status === 401) {
      csrfToken = "";
      authenticationFailureHandler?.();
    }
    if (response.status === 403 && message === "CSRF check failed") {
      csrfToken = "";
      void viewerApi.session().catch(() => authenticationFailureHandler?.());
    }
    throw new ViewerApiError(message, response.status);
  }

  return payload as T;
}

export const viewerApi = {
  session: async (): Promise<SessionResponse> => {
    const session = await request<SessionResponse>("/session");
    csrfToken = session.csrf_token ?? "";
    return session;
  },

  login: async (password: string): Promise<SessionResponse> => {
    const session = await request<SessionResponse>("/login", {
      method: "POST",
      body: JSON.stringify({ password }),
    });
    csrfToken = session.csrf_token ?? "";
    return session;
  },

  logout: async (): Promise<void> => {
    await request("/logout", { method: "POST", body: "{}" });
    csrfToken = "";
  },

  dashboard: (): Promise<DashboardResponse> => request("/dashboard"),

  maintenance: (): Promise<MaintenanceResponse> => request("/maintenance"),

  aliases: (): Promise<AliasResponse> => request("/aliases"),

  quota: (): Promise<QuotaResponse> => request("/quota"),

  keyQuotas: (signal?: AbortSignal): Promise<KeyQuotaResponse> =>
    request("/access-guard/quotas", { signal }),

  modelPrices: (): Promise<ModelPricesResponse> => request("/model-prices"),

  modelPriceStatus: (signal?: AbortSignal): Promise<ModelPriceStatusResponse> =>
    request("/model-price-status", { signal }),

  usageStatus: (signal?: AbortSignal): Promise<UsageStatusResponse> =>
    request("/usage-status", { signal }),

  analytics: (
    payload: AnalyticsRequest,
    signal?: AbortSignal,
  ): Promise<AnalyticsResponse> =>
    request("/analytics", {
      method: "POST",
      body: JSON.stringify(payload),
      signal,
    }),
};

export type {
  AccountStat,
  AliasItem,
  AliasResponse,
  AnalyticsAnomalyPoint,
  AnalyticsFilters,
  AnalyticsRequest,
  AnalyticsResponse,
  AnalyticsSummary,
  ApiKeyStat,
  ChannelStat,
  DashboardHealthPoint,
  DashboardResponse,
  FailureSource,
  FilterOption,
  HeatmapContributor,
  HeatmapPoint,
  MaintenanceResponse,
  ModelStat,
  ModelPricesResponse,
  MonitoringEvent,
  QuotaAccount,
  QuotaResponse,
  QuotaWindow,
  RecentFailure,
  SessionResponse,
  TimelinePoint,
} from "./types";
