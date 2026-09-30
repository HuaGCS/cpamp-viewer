import { useCallback, useEffect, useRef, useState } from "react";
import type { ModelPriceStatusResponse } from "@/viewer/api/modelPriceStatus";
import { viewerApi } from "@/viewer/viewerApi";

export function useViewerModelPriceStatus(enabled: boolean, refreshKey = 0) {
  const [data, setData] = useState<ModelPriceStatusResponse | null>(null);
  const active = useRef(false);
  const controller = useRef<AbortController | null>(null);
  const pending = useRef<Promise<void> | null>(null);

  const refresh = useCallback((): Promise<void> => {
    if (!active.current || document.visibilityState === "hidden") {
      return Promise.resolve();
    }
    if (pending.current) return pending.current;

    const requestController = new AbortController();
    controller.current = requestController;
    const request = Promise.resolve()
      .then(() => viewerApi.modelPriceStatus(requestController.signal))
      .then((response) => {
        if (
          active.current &&
          controller.current === requestController &&
          !requestController.signal.aborted
        ) {
          setData(response);
        }
      })
      .catch(() => {
        if (
          active.current &&
          controller.current === requestController &&
          !requestController.signal.aborted
        ) {
          setData((previous) =>
            previous ? { ...previous, stale: true } : null,
          );
        }
      })
      .finally(() => {
        if (controller.current !== requestController) return;
        controller.current = null;
        pending.current = null;
      });
    pending.current = request;
    return request;
  }, []);

  useEffect(() => {
    if (!enabled) return;
    active.current = true;
    let timer: ReturnType<typeof setInterval> | undefined;
    const stop = () => {
      if (timer !== undefined) clearInterval(timer);
      timer = undefined;
    };
    const cancel = () => {
      controller.current?.abort();
      controller.current = null;
      pending.current = null;
    };
    const start = () => {
      stop();
      if (document.visibilityState !== "hidden") {
        void refresh();
        timer = setInterval(() => void refresh(), 60_000);
      }
    };
    const visibilityChanged = () => {
      if (document.visibilityState === "hidden") {
        stop();
        cancel();
      } else {
        start();
      }
    };
    const kickoff = setTimeout(start, 0);
    document.addEventListener("visibilitychange", visibilityChanged);
    return () => {
      active.current = false;
      clearTimeout(kickoff);
      stop();
      cancel();
      document.removeEventListener("visibilitychange", visibilityChanged);
    };
  }, [enabled, refresh, refreshKey]);

  return { data, refresh };
}
