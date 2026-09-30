import { useCallback, useEffect, useRef, useState } from "react";
import type { UsageStatusResponse } from "@/viewer/api/usageStatus";
import { viewerApi } from "@/viewer/viewerApi";

export function useViewerUsageStatus() {
  const [data, setData] = useState<UsageStatusResponse | null>(null);
  const [loading, setLoading] = useState(true);
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
    setLoading(true);
    const isCurrent = () =>
      active.current &&
      controller.current === requestController &&
      !requestController.signal.aborted;

    const request = Promise.resolve()
      .then(() => viewerApi.usageStatus(requestController.signal))
      .then((response) => {
        if (isCurrent()) setData(response);
      })
      .catch(() => {
        if (!isCurrent()) return;
        setData((previous) =>
          previous?.available && previous.status
            ? { ...previous, stale: true }
            : { available: false, stale: false },
        );
      })
      .finally(() => {
        if (!isCurrent()) return;
        setLoading(false);
        controller.current = null;
        pending.current = null;
      });
    pending.current = request;
    return request;
  }, []);

  useEffect(() => {
    active.current = true;
    const cancel = () => {
      controller.current?.abort();
      controller.current = null;
      pending.current = null;
    };
    const visibilityChanged = () => {
      if (document.visibilityState === "hidden") {
        cancel();
        setLoading(false);
      } else {
        void refresh();
      }
    };
    const kickoff = setTimeout(() => void refresh(), 0);
    const timer = setInterval(() => void refresh(), 60_000);
    document.addEventListener("visibilitychange", visibilityChanged);
    return () => {
      active.current = false;
      cancel();
      clearTimeout(kickoff);
      clearInterval(timer);
      document.removeEventListener("visibilitychange", visibilityChanged);
    };
  }, [refresh]);

  return { data, loading, refresh };
}
