import { UsageAnalyticsSurface } from "./UsageAnalyticsSurface";
import { useUsageAnalytics } from "./useUsageAnalytics";

export function UsageAnalyticsPage() {
  return <UsageAnalyticsSurface useAnalytics={useUsageAnalytics} />;
}
