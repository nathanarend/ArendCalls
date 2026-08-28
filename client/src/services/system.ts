import { apiGet } from "@/lib/api";
import type { SystemMetricsResponse } from "@/types/system";

export const getSystemMetrics = (): Promise<SystemMetricsResponse> => {
  return apiGet<SystemMetricsResponse>("/api/system/metrics");
};
