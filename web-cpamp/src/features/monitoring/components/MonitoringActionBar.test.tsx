import { createRef } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router-dom";
import type { TFunction } from "i18next";
import { describe, expect, it, vi } from "vitest";
import styles from "../MonitoringCenterPage.module.scss";
import { MonitoringActionBar } from "./MonitoringActionBar";

const t = ((key: string) => {
  const messages: Record<string, string> = {
    "common.action": "Actions",
    "common.loading": "Loading",
    "dashboard.viewer_read_only": "Read-only",
    "monitoring.open_logs": "Logs",
    "nav.account_actions": "Account actions",
    "usage_stats.export": "Export",
    "usage_stats.import": "Import",
    "usage_stats.model_price_settings": "Model prices",
  };
  return messages[key] ?? key;
}) as unknown as TFunction;

describe("MonitoringActionBar Viewer mode", () => {
  it("keeps the original action layout while disabling write routes", () => {
    const markup = renderToStaticMarkup(
      <MemoryRouter>
        <MonitoringActionBar
          usageTransferAvailable
          usageExporting={false}
          usageImporting={false}
          loggingToFile
          modelPricesAvailable
          usageImportInputRef={createRef<HTMLInputElement>()}
          t={t}
          onUsageExport={vi.fn()}
          onUsageImportClick={vi.fn()}
          onUsageImportChange={vi.fn()}
          statusSummary={<span>Connected</span>}
          readOnly
          readOnlyMessage="Read-only"
          allowReadOnlyExport
        />
      </MemoryRouter>,
    );

    expect(markup).toContain(styles.actionBar);
    expect(markup).toContain("Export");
    expect(markup).toContain("Import");
    expect(markup).toContain("Model prices");
    expect(markup).toContain("Account actions");
    expect(markup).toContain("Logs");
    expect(markup).toContain("Connected");
    expect(markup).not.toContain('href="/model-prices"');
    expect(markup).not.toContain('href="/monitoring/account-actions"');
    expect(markup).not.toContain('href="/logs"');
    expect(markup.match(/disabled=""/g)).toHaveLength(5);
  });
});
