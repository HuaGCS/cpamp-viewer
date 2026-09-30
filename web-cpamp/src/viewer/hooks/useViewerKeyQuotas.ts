import { useCallback, useEffect, useRef, useState } from "react";
import { useHeaderRefresh } from "@/hooks/useHeaderRefresh";
import { viewerApi } from "@/viewer/viewerApi";
import type { KeyQuotaResponse } from "@/viewer/api/types";

export function useViewerKeyQuotas() {
  const [data, setData] = useState<KeyQuotaResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const mounted = useRef(false);
  const controller = useRef<AbortController | null>(null);
  const pending = useRef<Promise<void> | null>(null);

  const refresh = useCallback((): Promise<void> => {
    if (!mounted.current || document.visibilityState === "hidden") {
      return Promise.resolve();
    }
    if (pending.current) return pending.current;

    const requestController = new AbortController();
    controller.current = requestController;
    setLoading(true);
    const request = Promise.resolve()
      .then(() => viewerApi.keyQuotas(requestController.signal))
      .then((response) => {
        if (!mounted.current || controller.current !== requestController) return;
        setData(response);
        setFailed(false);
      })
      .catch(() => {
        if (
          mounted.current &&
          controller.current === requestController &&
          !requestController.signal.aborted
        ) {
          setFailed(true);
        }
      })
      .finally(() => {
        if (!mounted.current || controller.current !== requestController) return;
        controller.current = null;
        pending.current = null;
        setLoading(false);
      });
    pending.current = request;
    return request;
  }, []);

  useEffect(() => {
    mounted.current = true;
    let timer: ReturnType<typeof setInterval> | undefined;
    const stop = () => {
      if (timer !== undefined) clearInterval(timer);
      timer = undefined;
    };
    const start = () => {
      stop();
      if (document.visibilityState !== "hidden") {
        void refresh();
        timer = setInterval(() => void refresh(), 30_000);
      }
    };
    const visibilityChanged = () => {
      if (document.visibilityState === "hidden") {
        stop();
        controller.current?.abort();
        controller.current = null;
        pending.current = null;
        setLoading(false);
      } else {
        start();
      }
    };
    const kickoff = setTimeout(start, 0);
    document.addEventListener("visibilitychange", visibilityChanged);
    return () => {
      mounted.current = false;
      clearTimeout(kickoff);
      stop();
      document.removeEventListener("visibilitychange", visibilityChanged);
      controller.current?.abort();
      controller.current = null;
      pending.current = null;
    };
  }, [refresh]);

  useHeaderRefresh(refresh);

  return { data, loading, failed, stale: failed || data?.stale === true, refresh };
}
