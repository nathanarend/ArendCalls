export interface ProcessMetrics {
  uptimeSeconds: number;
  uptimeFormatted: string;
  memoryAllocMb: number;
  memorySysMb: number;
  memoryHeapAllocMb: number;
  memoryHeapInuseMb: number;
  memoryRssMb: number;
  cpuPercent: number;
  goroutines: number;
  numGc: number;
  numCpu: number;
  goVersion: string;
  activeCalls: number;
  totalSessions: number;
  connectedSessions: number;
}

export interface HostMetrics {
  memTotalMb: number;
  memUsedMb: number;
  memFreeMb: number;
  memAvailableMb: number;
  memUsagePercent: number;
  load1: number;
  load5: number;
  load15: number;
  cpuCores: number;
  diskTotalGb: number;
  diskUsedGb: number;
  diskFreeGb: number;
  diskUsagePercent: number;
}

export interface SystemMetricsResponse {
  process: ProcessMetrics;
  host: HostMetrics;
  timestamp: number;
}
