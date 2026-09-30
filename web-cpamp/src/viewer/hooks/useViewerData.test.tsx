import { useEffect } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ViewerAnalyticsRequest,
  ViewerAnalyticsResponse,
} from "@/viewer/model/viewerTypes";
import { useViewerAnalytics } from "./useViewerData";

const { analytics } = vi.hoisted(() => ({
  analytics:
    vi.fn<
      (
        request: ViewerAnalyticsRequest,
        signal?: AbortSignal,
      ) => Promise<ViewerAnalyticsResponse>
    >(),
}));

vi.mock("@/viewer/viewerApi", () => ({ viewerApi: { analytics } }));
vi.mock("@/hooks/useHeaderRefresh", () => ({ useHeaderRefresh: vi.fn() }));

const responseFor = (deleted: number): ViewerAnalyticsResponse => ({
  summary: { total_calls: deleted + 10 },
  coverage: {
    scope: "time_range",
    mode: "mixed",
    raw_complete: false,
    core_aggregate_used: true,
    raw_deleted_event_count: deleted,
    min_deleted_timestamp_ms: 1,
    max_deleted_timestamp_ms: 2,
    fidelity_limitations: ["event_details_require_raw_events"],
  },
});

describe("useViewerAnalytics coverage scope", () => {
  let renderer: ReactTestRenderer | undefined;
  let latest: ReturnType<typeof useViewerAnalytics>;
  const responseA = responseFor(2);
  const responseB = responseFor(7);

  function Harness({
    requestKey,
    enabled = true,
  }: {
    requestKey: string;
    enabled?: boolean;
  }) {
    const result = useViewerAnalytics(
      {
        from_ms: requestKey === "A" ? 1000 : 3000,
        to_ms: requestKey === "A" ? 2000 : 4000,
        include: { summary: true },
      },
      { requestKey, enabled, registerGlobalRefresh: false },
    );
    useEffect(() => {
      latest = result;
    }, [result]);
    return null;
  }

  async function renderScope(requestKey: string, enabled = true) {
    await act(async () => {
      if (renderer)
        renderer.update(<Harness requestKey={requestKey} enabled={enabled} />);
      else
        renderer = create(
          <Harness requestKey={requestKey} enabled={enabled} />,
        );
    });
  }

  async function advance(ms: number) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ms);
    });
  }

  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    vi.stubGlobal("window", {
      setTimeout: globalThis.setTimeout.bind(globalThis),
      clearTimeout: globalThis.clearTimeout.bind(globalThis),
      setInterval: globalThis.setInterval.bind(globalThis),
      clearInterval: globalThis.clearInterval.bind(globalThis),
    });
    analytics.mockReset();
    analytics.mockResolvedValue(responseA);
  });

  afterEach(async () => {
    if (renderer)
      await act(async () => {
        renderer?.unmount();
      });
    renderer = undefined;
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("hides A coverage immediately on B selection, through B failure, until B succeeds", async () => {
    await renderScope("A");
    await advance(120);
    expect(latest.coverage).toEqual(responseA.coverage);

    analytics.mockRejectedValueOnce(new Error("scope B unavailable"));
    await renderScope("B");
    expect(latest.data).toBe(responseA);
    expect(latest.coverage).toBeUndefined();
    expect(analytics).toHaveBeenCalledTimes(1);
    await advance(119);
    expect(latest.coverage).toBeUndefined();
    expect(analytics).toHaveBeenCalledTimes(1);
    await advance(1);
    expect(latest.error).toBe("scope B unavailable");
    expect(latest.data).toBe(responseA);
    expect(latest.coverage).toBeUndefined();

    analytics.mockResolvedValueOnce(responseB);
    await act(async () => {
      await latest.refresh();
    });
    expect(latest.data).toBe(responseB);
    expect(latest.coverage).toEqual(responseB.coverage);
  });

  it("retains coverage during and after a failed refresh of the same scope", async () => {
    await renderScope("A");
    await advance(120);
    let rejectRefresh!: (error: Error) => void;
    analytics.mockImplementationOnce(
      () =>
        new Promise((_, reject) => {
          rejectRefresh = reject;
        }),
    );
    let pending!: Promise<void>;
    await act(async () => {
      pending = latest.refresh();
    });
    expect(latest.loading).toBe(true);
    expect(latest.coverage).toEqual(responseA.coverage);

    await act(async () => {
      rejectRefresh(new Error("refresh unavailable"));
      await pending;
    });
    expect(latest.coverage).toEqual(responseA.coverage);
    expect(latest.data).toBe(responseA);
  });

  it("hides coverage while disabled without discarding the last statistics", async () => {
    await renderScope("A");
    await advance(120);
    await renderScope("A", false);
    expect(latest.coverage).toBeUndefined();
    expect(latest.data).toBe(responseA);
    await advance(120);
    expect(analytics).toHaveBeenCalledTimes(1);
  });

  it("does not restore an aborted response after a newer scope succeeds", async () => {
    let resolveA!: (response: ViewerAnalyticsResponse) => void;
    analytics.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveA = resolve;
        }),
    );
    await renderScope("A");
    await advance(120);
    const oldSignal = analytics.mock.calls[0][1];
    analytics.mockResolvedValueOnce(responseB);
    await renderScope("B");
    await advance(120);
    expect(oldSignal?.aborted).toBe(true);
    expect(latest.coverage).toEqual(responseB.coverage);
    await act(async () => {
      resolveA(responseA);
    });
    expect(latest.coverage).toEqual(responseB.coverage);
    expect(latest.data).toBe(responseB);
  });
});
