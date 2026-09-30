import { UsageAnalyticsSurface } from "@/features/usage-analytics/UsageAnalyticsSurface";
import { useViewerUsageAnalytics } from "@/viewer/hooks/useViewerUsageAnalytics";

export function ViewerUsageAnalyticsPage() {
  return <UsageAnalyticsSurface useAnalytics={useViewerUsageAnalytics} />;
}

export default ViewerUsageAnalyticsPage;
