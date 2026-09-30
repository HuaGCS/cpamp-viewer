import { describe, expect, it } from "vitest";
import type { ViewerDashboardResponse } from "@/viewer/model/viewerTypes";
import {
  getViewerDashboardCounts,
  normalizeViewerDashboard,
} from "./ViewerDashboardPage.model";

describe("ViewerDashboardPage model", () => {
  it("includes Meta provider keys in the safe count fallback", () => {
    const counts = getViewerDashboardCounts({
      stats: { providers: { codex: 2, meta: 3 } },
    });
    expect(counts.providerTotal).toBe(5);
    expect(counts.providers.meta).toBe(3);
    expect(
      getViewerDashboardCounts({
        stats: { providers: { codex: 2, meta: 3, total: 9 } },
      }).providerTotal,
    ).toBe(9);
  });

  it("adapts the safe Viewer projection to the upstream dashboard cards", () => {
    const data: ViewerDashboardResponse = {
      generated_at_ms: Date.UTC(2026, 6, 14, 8, 0, 0),
      window: {
        today_start_ms: Date.UTC(2026, 6, 13, 16, 0, 0),
        now_ms: Date.UTC(2026, 6, 14, 7, 59, 30),
        rolling_30m_start_ms: Date.UTC(2026, 6, 14, 7, 29, 30),
      },
      today: {
        total_calls: 10,
        success_calls: 8,
        failure_calls: 2,
        success_rate: 0.8,
        total_tokens: 1200,
        total_cost: 8,
      },
      rolling_30m: {
        rpm: 2,
        tpm: 240,
        total_calls: 4,
        total_tokens: 480,
      },
      top_models_today: [
        {
          model: "top-model",
          calls: 9,
          tokens: 111,
          total_tokens: 0,
          cost: 3,
          success_rate: 0.9,
        },
      ],
      model_cost_rank: [
        {
          model: "model-a",
          calls: 3,
          tokens: 300,
          total_tokens: 0,
          cost: 2,
          cost_share: 0.2,
        },
        {
          model: "model-b",
          calls: 7,
          tokens: 900,
          total_tokens: 0,
          cost: 6,
          cost_share: 0.8,
        },
      ],
      traffic_timeline: [
        {
          bucket_ms: Date.UTC(2026, 6, 14, 7, 0, 0),
          calls: 4,
          total_tokens: 480,
          success: 3,
          failure: 1,
          calls_share: 0.4,
          tokens_share: 0.6,
          failure_rate: 0.25,
        },
      ],
      today_request_health_timeline: {
        success_calls: 3,
        failure_calls: 1,
        total_calls: 4,
        points: [
          {
            bucket_ms: Date.UTC(2026, 6, 14, 7, 0, 0),
            calls: 4,
            tokens: 480,
            success: 3,
            failure: 1,
            tone: "warning",
          },
        ],
      },
    };

    const result = normalizeViewerDashboard(data);

    expect(result.today.total_calls).toBe(10);
    expect(result.window).toEqual(data.window);
    expect(result.top_models_today).toEqual([
      {
        model: "top-model",
        calls: 9,
        tokens: 111,
        cost: 3,
        success_rate: 0.9,
      },
    ]);
    expect(result.model_cost_rank?.map((item) => item.cost_share)).toEqual([
      0.2, 0.8,
    ]);
    expect(result.traffic_timeline?.[0]).toMatchObject({
      tokens: 480,
      calls_share: 0.4,
      tokens_share: 0.6,
      failure_rate: 0.25,
    });
    expect(result.today_request_health_timeline?.points[0]).toMatchObject({
      tone: "warn",
      success_rate: 0.75,
      failure_rate: 0.25,
    });
  });

  it("falls back to the cost ranking only when top models are absent", () => {
    const ranking = [
      { model: "model-a", calls: 1, tokens: 20, cost: 1 },
      { model: "model-b", calls: 2, tokens: 40, cost: 3 },
    ];

    const fallback = normalizeViewerDashboard({ model_cost_rank: ranking });
    expect(fallback.top_models_today.map((item) => item.model)).toEqual([
      "model-a",
      "model-b",
    ]);
    expect(fallback.model_cost_rank?.map((item) => item.cost_share)).toEqual([
      0.25, 0.75,
    ]);

    const explicitEmpty = normalizeViewerDashboard({
      top_models_today: [],
      model_cost_rank: ranking,
    });
    expect(explicitEmpty.top_models_today).toEqual([]);
  });

  it("maps the read-only dashboard management statistics", () => {
    const data = {
      generated_at_ms: 1,
      stats: {
        management_keys: 3,
        auth_files: 9,
        available_models: 27,
        providers: { gemini: 1, codex: 2, xai: 5, claude: 3, openai: 4 },
      },
    } as ViewerDashboardResponse & {
      stats: {
        management_keys: number;
        auth_files: number;
        available_models: number;
        providers: {
          gemini: number;
          codex: number;
          xai: number;
          claude: number;
          openai: number;
        };
      };
    };

    expect(getViewerDashboardCounts(data)).toEqual({
      accessKeyCount: 3,
      authFiles: 9,
      availableModels: 27,
      providerTotal: 15,
      providers: { gemini: 1, codex: 2, xai: 5, meta: 0, claude: 3, openai: 4 },
    });
  });
});
