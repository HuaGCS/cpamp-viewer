import type { ReactNode } from "react";
import { Button } from "@/components/ui/Button";
import { IconRefreshCw } from "@/components/ui/icons";
import { useHeaderRefresh } from "@/hooks/useHeaderRefresh";
import { useLanguageStore } from "@/stores/useLanguageStore";
import { useViewerUsageStatus } from "@/viewer/hooks/useViewerUsageStatus";
import {
  formatUsageBytes,
  formatUsageCount,
  usageStatusCopy,
  usageStatusTime,
} from "@/viewer/model/usageStatus";
import styles from "./ViewerUsageStatusPage.module.scss";

function ValueRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className={styles.row}>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

export function ViewerUsageStatusPage() {
  const language = useLanguageStore((state) => state.language);
  const copy = usageStatusCopy(language);
  const { data, loading, refresh } = useViewerUsageStatus();
  useHeaderRefresh(refresh);
  const status = data?.available ? data.status : undefined;
  const metrics: Array<[string, number | undefined]> = [
    [copy.online, status?.raw_event_count],
    [copy.archived, status?.raw_archived_event_count],
    [copy.deleted, status?.raw_deleted_event_count],
  ];
  const checked = usageStatusTime(data?.checked_at_ms, language);
  const dateValue = (value: number | undefined) => {
    const time = usageStatusTime(value, language);
    return time ? <time dateTime={time.iso}>{time.label}</time> : copy.unknown;
  };
  const readiness = (ready: boolean) => (
    <span
      className={`${styles.badge} ${ready ? styles.ready : styles.pending}`}
    >
      {ready ? copy.ready : copy.pending}
    </span>
  );

  return (
    <section className={styles.page} aria-busy={loading}>
      <header className={styles.header}>
        <div>
          <h1 className={styles.title}>{copy.title}</h1>
          <p className={styles.description}>{copy.description}</p>
        </div>
        <Button
          variant="secondary"
          disabled={loading}
          onClick={() => void refresh()}
        >
          <span className={styles.refreshLabel}>
            <IconRefreshCw size={16} />
            {loading ? copy.refreshing : copy.refresh}
          </span>
        </Button>
      </header>

      {status && data?.stale ? (
        <p role="status" className={styles.warning}>
          {copy.stale}
        </p>
      ) : null}

      {status ? (
        <>
          <div className={styles.metrics}>
            {metrics.map(([label, value]) => (
              <article className={styles.card} key={label}>
                <h2 className={styles.metricLabel}>{label}</h2>
                <p className={styles.metricValue}>
                  {formatUsageCount(value, language)}
                </p>
              </article>
            ))}
          </div>
          <p className={styles.note}>{copy.recordsNote}</p>
          <div className={styles.grid}>
            <article className={styles.card}>
              <h2 className={styles.cardTitle}>{copy.range}</h2>
              {status.raw_event_count === 0 ? (
                <p className={styles.description}>{copy.emptyRange}</p>
              ) : (
                <dl className={styles.values}>
                  <ValueRow label={copy.earliest}>
                    {dateValue(status.raw_min_timestamp_ms)}
                  </ValueRow>
                  <ValueRow label={copy.latest}>
                    {dateValue(status.raw_max_timestamp_ms)}
                  </ValueRow>
                </dl>
              )}
            </article>
            <article className={styles.card}>
              <h2 className={styles.cardTitle}>{copy.readiness}</h2>
              <dl className={styles.values}>
                <ValueRow label={copy.migration}>
                  {readiness(status.migration_ready)}
                </ValueRow>
                <ValueRow label={copy.aggregate}>
                  {readiness(status.hourly_aggregate_ready)}
                </ValueRow>
              </dl>
            </article>
            <article className={styles.card}>
              <h2 className={styles.cardTitle}>{copy.storage}</h2>
              <dl className={styles.values}>
                <ValueRow label={copy.total}>
                  <strong>
                    {formatUsageBytes(status.storage.total_bytes, language)}
                  </strong>
                </ValueRow>
                <ValueRow label={copy.database}>
                  {formatUsageBytes(status.storage.database_bytes, language)}
                </ValueRow>
                <ValueRow label={copy.wal}>
                  {formatUsageBytes(status.storage.wal_bytes, language)}
                </ValueRow>
                <ValueRow label={copy.shm}>
                  {formatUsageBytes(status.storage.shm_bytes, language)}
                </ValueRow>
                <ValueRow label={copy.reclaimable}>
                  {formatUsageBytes(status.storage.reclaimable_bytes, language)}
                </ValueRow>
              </dl>
              <p className={styles.note}>{copy.storageNote}</p>
            </article>
            <article className={styles.card}>
              <h2 className={styles.cardTitle}>{copy.activity}</h2>
              <p className={styles.activity}>
                {status.active_operation
                  ? Object.prototype.hasOwnProperty.call(
                      copy.operations,
                      status.active_operation,
                    )
                    ? copy.operations[status.active_operation]
                    : copy.operations.maintenance
                  : status.active_status
                    ? copy.pendingTask
                    : copy.idle}
              </p>
              {status.active_status ? (
                <dl className={styles.values}>
                  <ValueRow label={copy.state}>
                    {Object.prototype.hasOwnProperty.call(
                      copy.states,
                      status.active_status,
                    )
                      ? copy.states[status.active_status]
                      : copy.unknown}
                  </ValueRow>
                </dl>
              ) : null}
            </article>
          </div>
        </>
      ) : (
        <p role="status" className={styles.empty}>
          {loading && !data ? copy.loading : copy.unavailable}
        </p>
      )}

      {checked ? (
        <p className={styles.checked}>
          {copy.checked} <time dateTime={checked.iso}>{checked.label}</time>
        </p>
      ) : null}
    </section>
  );
}
