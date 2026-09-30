export interface ModelPriceStatusResponse {
  available: boolean;
  stale: boolean;
  checked_at_ms?: number;
  unpriced_models: string[];
  unpriced_count: number;
  model_count: number;
}
