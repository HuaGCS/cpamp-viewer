import { describe, expect, it } from "vitest";
import { buildViewerMonitoringInitialQuery } from "@/viewer/model/monitoringPage";

describe("ViewerMonitoringPage drilldown query", () => {
  it("accepts only safe Viewer ids for original Monitoring query keys", () => {
    const state = buildViewerMonitoringInitialQuery(
      "?from_ms=1720000000000&to_ms=1720003600000" +
        "&api_key_hash=view_0123456789ab" +
        "&auth_file=view_111111111111" +
        "&project_id=view_222222222222" +
        "&account=view_333333333333" +
        "&request_type=responses&status=failed&model=gpt-test",
    );

    expect(state).toMatchObject({
      timeRange: "custom",
      selectedApiKeyId: "view_0123456789ab",
      selectedAuthFile: "view_111111111111",
      selectedProjectId: "view_222222222222",
      selectedAccount: "view_333333333333",
      selectedRequestType: "responses",
      selectedStatus: "failed",
      selectedModel: "gpt-test",
      openRealtime: true,
    });
  });

  it("drops legacy raw hashes instead of forwarding them", () => {
    const rawHash = "a".repeat(64);
    const state = buildViewerMonitoringInitialQuery(
      `?api_key_hash=${rawHash}`,
    );

    expect(state.selectedApiKeyId).toBe("all");
    expect(state.openRealtime).toBe(false);
  });
});
