import { useEffect } from "react";
import { act, create, type ReactTestRenderer } from "react-test-renderer";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ModelPriceStatusResponse } from "@/viewer/api/modelPriceStatus";
import { useViewerModelPriceStatus } from "./useViewerModelPriceStatus";

(
  globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
).IS_REACT_ACT_ENVIRONMENT = true;

const { modelPriceStatus } = vi.hoisted(() => ({
  modelPriceStatus:
    vi.fn<(signal?: AbortSignal) => Promise<ModelPriceStatusResponse>>(),
}));
vi.mock("@/viewer/viewerApi", () => ({ viewerApi: { modelPriceStatus } }));

const snapshot: ModelPriceStatusResponse = {
  available: true,
  stale: false,
  checked_at_ms: 1_789_898_400_000,
  unpriced_models: ["model-a"],
  unpriced_count: 1,
  model_count: 10,
};

describe("model price status lifecycle", () => {
  let renderer: ReactTestRenderer | undefined;
  let latest: ReturnType<typeof useViewerModelPriceStatus>;
  let visibility: "visible" | "hidden";
  let fakeDocument: EventTarget;

  function Harness({ enabled = true, refreshKey = 0 }) {
    const result = useViewerModelPriceStatus(enabled, refreshKey);
    useEffect(() => {
      latest = result;
    }, [result]);
    return null;
  }

  async function mount(enabled = true) {
    await act(async () => {
      renderer = create(<Harness enabled={enabled} />);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
  }

  async function setVisibility(value: "visible" | "hidden") {
    visibility = value;
    await act(async () => {
      fakeDocument.dispatchEvent(new Event("visibilitychange"));
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
    modelPriceStatus.mockReset();
    modelPriceStatus.mockResolvedValue(snapshot);
  });

  afterEach(async () => {
    if (renderer) {
      await act(async () => {
        renderer?.unmount();
      });
    }
    renderer = undefined;
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("fetches only on enabled routes and accepts a header refresh key", async () => {
    await mount(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000);
    });
    expect(modelPriceStatus).not.toHaveBeenCalled();
    await act(async () => {
      renderer?.update(<Harness enabled />);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(modelPriceStatus).toHaveBeenCalledTimes(1);
    await act(async () => {
      renderer?.update(<Harness enabled refreshKey={1} />);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(modelPriceStatus).toHaveBeenCalledTimes(2);
    await act(async () => {
      renderer?.update(<Harness enabled={false} refreshKey={2} />);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000);
    });
    expect(modelPriceStatus).toHaveBeenCalledTimes(2);
  });

  it("polls every 60 seconds only while the page is visible", async () => {
    visibility = "hidden";
    await mount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000);
    });
    expect(modelPriceStatus).not.toHaveBeenCalled();
    await setVisibility("visible");
    expect(modelPriceStatus).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });
    expect(modelPriceStatus).toHaveBeenCalledTimes(2);
    await setVisibility("hidden");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120_000);
    });
    expect(modelPriceStatus).toHaveBeenCalledTimes(2);
  });

  it("stays silent on an initial failure and preserves stale data on later failure", async () => {
    modelPriceStatus.mockRejectedValueOnce(new Error("unavailable"));
    await mount();
    expect(latest.data).toBeNull();
    await act(async () => {
      await latest.refresh();
    });
    expect(latest.data).toEqual(snapshot);
    modelPriceStatus.mockRejectedValueOnce(new Error("unavailable"));
    await act(async () => {
      await latest.refresh();
    });
    expect(latest.data).toEqual({ ...snapshot, stale: true });
    await act(async () => {
      await latest.refresh();
    });
    expect(latest.data).toEqual(snapshot);
  });

  it("deduplicates in-flight polls and cancels them on unmount", async () => {
    let resolve!: (value: ModelPriceStatusResponse) => void;
    modelPriceStatus.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    await mount();
    let first: Promise<void> | undefined;
    let second: Promise<void> | undefined;
    await act(async () => {
      first = latest.refresh();
      second = latest.refresh();
      await vi.advanceTimersByTimeAsync(120_000);
    });
    expect(first).toBe(second);
    expect(modelPriceStatus).toHaveBeenCalledTimes(1);
    const signal = modelPriceStatus.mock.calls[0][0];
    await act(async () => {
      renderer?.unmount();
    });
    renderer = undefined;
    expect(signal?.aborted).toBe(true);
    await act(async () => {
      resolve(snapshot);
      await first;
      await vi.advanceTimersByTimeAsync(120_000);
    });
    expect(modelPriceStatus).toHaveBeenCalledTimes(1);
  });

  it("rejects old responses after disabling and reenabling the notice", async () => {
    let resolveOld!: (value: ModelPriceStatusResponse) => void;
    modelPriceStatus.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve;
        }),
    );
    await mount();
    const oldSignal = modelPriceStatus.mock.calls[0][0];
    await act(async () => {
      renderer?.update(<Harness enabled={false} />);
    });
    expect(oldSignal?.aborted).toBe(true);
    await act(async () => {
      renderer?.update(<Harness enabled />);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(latest.data).toEqual(snapshot);
    await act(async () => {
      resolveOld({ ...snapshot, unpriced_count: 99 });
    });
    expect(latest.data).toEqual(snapshot);
  });
});
