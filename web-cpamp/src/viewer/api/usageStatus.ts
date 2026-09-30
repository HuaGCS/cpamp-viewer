export type UsageStatusOperation =
  "archive" | "verify" | "delete" | "compact" | "maintenance";

export interface ViewerUsageStatus {
  raw_event_count: number;
  raw_archived_event_count?: number;
  raw_deleted_event_count: number;
  raw_min_timestamp_ms?: number;
  raw_max_timestamp_ms?: number;
  migration_ready: boolean;
  hourly_aggregate_ready: boolean;
  storage: {
    database_bytes: number;
    wal_bytes: number;
    shm_bytes: number;
    total_bytes: number;
    reclaimable_bytes: number;
  };
  active_operation?: UsageStatusOperation;
  active_status?: string;
}

export interface UsageStatusResponse {
  available: boolean;
  stale: boolean;
  checked_at_ms?: number;
  status?: ViewerUsageStatus;
}
