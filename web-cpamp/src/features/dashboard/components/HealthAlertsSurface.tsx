import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import styles from "./HealthAlertsCard.module.scss";

export interface HealthAlertsChannelRow {
  key: string;
  label: string;
  title?: string;
  tone: string;
  successRate: number;
}

export interface HealthAlertsFailureTooltip {
  statusText: string;
  summary: string;
  diagnostics: string[];
}

export interface HealthAlertsFailureRow {
  key: string;
  timestampMs: number;
  model: string;
  source: string;
  sourceTitle?: string;
  durationText: string;
  tooltip?: HealthAlertsFailureTooltip | null;
}

interface HealthAlertsSurfaceProps {
  loading: boolean;
  channelRows: HealthAlertsChannelRow[];
  failureRows: HealthAlertsFailureRow[];
}

export function HealthAlertsSurface({
  loading,
  channelRows,
  failureRows,
}: HealthAlertsSurfaceProps) {
  const { t, i18n } = useTranslation();
  const formatPercent = useMemo(
    () =>
      new Intl.NumberFormat(i18n.language, {
        style: "percent",
        maximumFractionDigits: 1,
      }).format,
    [i18n.language],
  );

  return (
    <>
      <section className={styles.dataCard}>
        <div className={styles.cardHeader}>
          <h3>{t("dashboard.channel_health_status")}</h3>
        </div>
        <div className={styles.list}>
          {channelRows.map((row) => (
            <div key={row.key} className={styles.listItem}>
              <span
                className={`${styles.statusDot} ${styles[row.tone] || ""}`}
              />
              <span className={styles.label} title={row.title}>
                {row.label}
              </span>
              <span className={styles.value}>
                {formatPercent(row.successRate)}
              </span>
            </div>
          ))}
          {channelRows.length === 0 ? (
            <div className={styles.empty}>
              {loading ? "..." : t("dashboard.no_channel_health_data")}
            </div>
          ) : null}
        </div>
      </section>

      <section className={styles.dataCard}>
        <div className={styles.cardHeader}>
          <h3>{t("dashboard.recent_failed_requests")}</h3>
        </div>
        <div className={styles.list}>
          {failureRows.map((row) => (
            <div key={row.key} className={styles.failureItem}>
              <div className={styles.failureMeta}>
                <span className={styles.time}>
                  {new Date(row.timestampMs).toLocaleTimeString(i18n.language)}
                </span>
                <span className={styles.model}>{row.model}</span>
              </div>
              <div className={styles.failureDetail}>
                <span
                  className={
                    row.tooltip ? styles.failureSourceWithTooltip : undefined
                  }
                  tabIndex={row.tooltip ? 0 : undefined}
                  title={row.tooltip ? undefined : row.sourceTitle}
                >
                  <span className={styles.failureSourceText}>{row.source}</span>
                  {row.tooltip ? (
                    <span role="tooltip" className={styles.failureTooltip}>
                      {row.tooltip.statusText ? (
                        <span className={styles.failureTooltipStatus}>
                          {row.tooltip.statusText}
                        </span>
                      ) : null}
                      {row.tooltip.summary ? (
                        <span className={styles.failureTooltipBody}>
                          {row.tooltip.summary}
                        </span>
                      ) : null}
                      {row.tooltip.diagnostics.map((item) => (
                        <span key={item} className={styles.failureTooltipBody}>
                          {item}
                        </span>
                      ))}
                    </span>
                  ) : null}
                </span>
                <span>{row.durationText}</span>
              </div>
            </div>
          ))}
          {failureRows.length === 0 ? (
            <div className={styles.empty}>
              {loading ? "..." : t("dashboard.no_recent_failures")}
            </div>
          ) : null}
        </div>
      </section>
    </>
  );
}
