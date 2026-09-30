import { useEffect } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { UsageStatusResponse } from "@/viewer/api/usageStatus";
import { useViewerUsageStatus } from "./useViewerUsageStatus";

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;
const { usageStatus } = vi.hoisted(() => ({
  usageStatus: vi.fn<(signal?: AbortSignal) => Promise<UsageStatusResponse>>(),
}));
vi.mock("@/viewer/viewerApi", () => ({ viewerApi: { usageStatus } }));

const snapshot: UsageStatusResponse = {
  available: true,
  stale: false,
  checked_at_ms: 1_790_414_400_000,
  status: {
    raw_event_count: 12,
    raw_deleted_event_count: 8,
    migration_ready: true,
    hourly_aggregate_ready: false,
    storage: {
      database_bytes: 2048,
      wal_bytes: 1024,
      shm_bytes: 0,
      total_bytes: 3072,
      reclaimable_bytes: 512,
    },
  },
};

describe("usage status request lifecycle", () => {
  let renderer: ReactTestRenderer | undefined;
  let result: ReturnType<typeof useViewerUsageStatus>;
  let visibility: "visible" | "hidden";
  let fakeDocument: EventTarget;
  function Harness() {
    const state = useViewerUsageStatus();
    useEffect(() => {
      result = state;
    }, [state]);
    return null;
  }
  async function mount() {
    await act(async () => {
      renderer = create(<Harness />);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
  }
  beforeEach(() => {
    vi.useFakeTimers();
    visibility = "visible";
    fakeDocument = new EventTarget();
    Object.defineProperty(fakeDocument, "visibilityState", {
      get: () => visibility,
    });
    vi.stubGlobal("document", fakeDocument);
    usageStatus.mockReset().mockResolvedValue(snapshot);
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

  it("keeps initial failure unavailable, preserves stale data on later failure, and recovers", async () => {
    usageStatus.mockRejectedValueOnce(
      new Error("private upstream diagnostics"),
    );
    await mount();
    expect(result.data).toEqual({ available: false, stale: false });
    expect(result.loading).toBe(false);
    await act(async () => {
      await result.refresh();
    });
    expect(result.data).toEqual(snapshot);
    usageStatus.mockRejectedValueOnce(
      new Error("private upstream diagnostics"),
    );
    await act(async () => {
      await result.refresh();
    });
    expect(result.data).toEqual({ ...snapshot, stale: true });
    expect(JSON.stringify(result.data)).not.toContain("private");
    await act(async () => {
      await result.refresh();
    });
    expect(result.data).toEqual(snapshot);
  });

  it("keeps explicit unavailable and stale backend responses distinct", async () => {
    usageStatus.mockResolvedValueOnce({ ...snapshot, stale: true });
    await mount();
    expect(result.data?.stale).toBe(true);
    usageStatus.mockResolvedValueOnce({ available: false, stale: false });
    await act(async () => {
      await result.refresh();
    });
    expect(result.data).toEqual({ available: false, stale: false });
  });

  it("deduplicates refreshes and aborts in-flight work on unmount", async () => {
    let resolve!: (value: UsageStatusResponse) => void;
    usageStatus.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    await mount();
    let first: Promise<void> | undefined;
    let second: Promise<void> | undefined;
    await act(async () => {
      first = result.refresh();
      second = result.refresh();
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(first).toBe(second);
    expect(usageStatus).toHaveBeenCalledTimes(1);
    expect(result.loading).toBe(true);
    const signal = usageStatus.mock.calls[0][0];
    await act(async () => {
      renderer?.unmount();
    });
    renderer = undefined;
    expect(signal?.aborted).toBe(true);
    await act(async () => {
      resolve(snapshot);
      await first;
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(usageStatus).toHaveBeenCalledTimes(1);
  });

  it("pauses when hidden and rejects an old response after becoming visible", async () => {
    let resolveOld!: (value: UsageStatusResponse) => void;
    usageStatus.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolveOld = done;
        }),
    );
    await mount();
    const oldSignal = usageStatus.mock.calls[0][0];
    visibility = "hidden";
    await act(async () => {
      fakeDocument.dispatchEvent(new Event("visibilitychange"));
      await vi.advanceTimersByTimeAsync(120_000);
    });
    expect(oldSignal?.aborted).toBe(true);
    expect(result.loading).toBe(false);
    expect(usageStatus).toHaveBeenCalledTimes(1);
    visibility = "visible";
    await act(async () => {
      fakeDocument.dispatchEvent(new Event("visibilitychange"));
    });
    expect(result.data).toEqual(snapshot);
    await act(async () => {
      resolveOld({ available: false, stale: false });
    });
    expect(result.data).toEqual(snapshot);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(usageStatus).toHaveBeenCalledTimes(3);
  });
});
