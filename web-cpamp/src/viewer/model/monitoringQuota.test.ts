import { describe, expect, it } from "vitest";
import { buildViewerMonitoringQuotaEntry } from "./monitoringQuota";

describe("Viewer monitoring quota projection", () => {
  it.each(["meta", "devin", "codex", "xai"])(
    "preserves %s identity and an unknown remaining percentage",
    (provider) => {
      const entry = buildViewerMonitoringQuotaEntry(
        {
          id: "view_0123456789ab",
          provider,
          display_name: "Public account",
          status: "observed",
          windows: [
            {
              id: "window-1",
              label: "Observed quota",
              remaining_percent: null,
              used_percent: null,
            },
          ],
        },
        "en-US",
      );
      expect(entry.provider).toBe(provider);
      expect(entry.windows[0].remainingPercent).toBeNull();
      expect(entry.windows[0].resetLabel).toBe("--");
    },
  );

  it("keeps unsupported providers unknown instead of assigning Codex", () => {
    const entry = buildViewerMonitoringQuotaEntry(
      {
        id: "view_0123456789ab",
        provider: "future-provider",
        display_name: "Public account",
        status: "unknown",
        windows: [
          {
            id: "window-1",
            label: "Observed quota",
            remaining_percent: 0,
            used_percent: 100,
          },
        ],
      },
      "en-US",
    );
    expect(entry.provider).toBe("unknown");
    expect(entry.providerLabel).toBe("future-provider");
    expect(entry.windows[0].remainingPercent).toBe(0);
  });
});
