import { useState, useEffect, useCallback } from "react";
import { 
  Activity, 
  Cpu, 
  HardDrive, 
  Layers, 
  PhoneCall, 
  RefreshCw, 
  Server, 
  Users, 
  Clock,
  Sparkles
} from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { getSystemMetrics } from "@/services/system";
import type { SystemMetricsResponse } from "@/types/system";

interface SystemMetricsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export const SystemMetricsModal = ({ open, onOpenChange }: SystemMetricsModalProps) => {
  const [metrics, setMetrics] = useState<SystemMetricsResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchMetrics = useCallback(async (isManual = false) => {
    if (isManual) setLoading(true);
    try {
      const data = await getSystemMetrics();
      setMetrics(data);
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      if (isManual) setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!open) {
      setMetrics(null);
      return;
    }

    // Fetch immediately upon opening
    void fetchMetrics(true);

    // Live polling every 2 seconds ONLY while the modal is open
    const interval = setInterval(() => {
      void fetchMetrics(false);
    }, 2000);

    return () => {
      clearInterval(interval);
    };
  }, [open, fetchMetrics]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl sm:max-w-2xl">
        <DialogHeader>
          <div className="flex items-center justify-between pr-6">
            <div className="flex items-center gap-2">
              <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-600 dark:bg-emerald-500/20 dark:text-emerald-400">
                <Activity className="h-4 w-4" />
              </span>
              <div>
                <DialogTitle className="text-base font-semibold">Recursos & VPS</DialogTitle>
                <DialogDescription className="text-xs">
                  Telemetria instantânea do ArendCalls e da máquina host
                </DialogDescription>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="gap-1.5 py-0.5 text-[11px] font-normal border-emerald-500/30 bg-emerald-500/5 text-emerald-600 dark:text-emerald-400">
                <span className="h-1.5 w-1.5 rounded-full bg-emerald-500 animate-pulse" />
                Tempo Real (2s)
              </Badge>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7"
                onClick={() => fetchMetrics(true)}
                disabled={loading}
                title="Atualizar agora"
              >
                <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
              </Button>
            </div>
          </div>
        </DialogHeader>

        {error && (
          <div className="rounded-md border border-destructive/50 bg-destructive/10 p-3 text-xs text-destructive">
            Erro ao obter telemetria: {error}
          </div>
        )}

        {!metrics && !error && (
          <div className="flex flex-col items-center justify-center py-10 text-muted-foreground">
            <RefreshCw className="h-6 w-6 animate-spin mb-2 text-primary" />
            <p className="text-sm">Lendo recursos da VPS...</p>
          </div>
        )}

        {metrics && (
          <div className="space-y-4 pt-1">
            {/* Seção 1: Processo ArendCalls */}
            <div>
              <div className="flex items-center justify-between mb-2">
                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
                  <Sparkles className="h-3.5 w-3.5 text-primary" />
                  Processo ArendCalls
                </h4>
                <span className="text-[11px] text-muted-foreground font-mono">
                  {metrics.process.goVersion}
                </span>
              </div>

              <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-5">
                <div className="rounded-lg border bg-card/60 p-3 transition-colors hover:bg-card">
                  <div className="flex items-center gap-1.5 text-muted-foreground mb-1">
                    <Cpu className="h-3.5 w-3.5 text-emerald-500" />
                    <span className="text-xs">CPU App</span>
                  </div>
                  <p className="text-lg font-bold tracking-tight text-foreground">
                    {metrics.process.cpuPercent.toFixed(1)}%
                  </p>
                  <p className="text-[10px] text-muted-foreground mt-0.5">
                    {metrics.process.cpuPercent > 0 ? `Total VPS (${metrics.process.numCpu} cores)` : "Modo ocioso"}
                  </p>
                </div>

                <div className="rounded-lg border bg-card/60 p-3 transition-colors hover:bg-card">
                  <div className="flex items-center gap-1.5 text-muted-foreground mb-1">
                    <HardDrive className="h-3.5 w-3.5" />
                    <span className="text-xs">RAM Processo</span>
                  </div>
                  <p className="text-lg font-bold tracking-tight text-foreground">
                    {metrics.process.memoryRssMb > 0 
                      ? `${metrics.process.memoryRssMb.toFixed(1)} MB`
                      : `${metrics.process.memoryAllocMb.toFixed(1)} MB`}
                  </p>
                  <p className="text-[10px] text-muted-foreground mt-0.5">
                    Heap: {metrics.process.memoryHeapAllocMb.toFixed(1)} MB
                  </p>
                </div>

                <div className="rounded-lg border bg-card/60 p-3 transition-colors hover:bg-card">
                  <div className="flex items-center gap-1.5 text-muted-foreground mb-1">
                    <Layers className="h-3.5 w-3.5" />
                    <span className="text-xs">Goroutines</span>
                  </div>
                  <p className="text-lg font-bold tracking-tight text-foreground">
                    {metrics.process.goroutines}
                  </p>
                  <p className="text-[10px] text-muted-foreground mt-0.5">
                    GCs: {metrics.process.numGc}
                  </p>
                </div>

                <div className="rounded-lg border bg-card/60 p-3 transition-colors hover:bg-card">
                  <div className="flex items-center gap-1.5 text-muted-foreground mb-1">
                    <Clock className="h-3.5 w-3.5" />
                    <span className="text-xs">Uptime</span>
                  </div>
                  <p className="text-base font-bold tracking-tight text-foreground truncate" title={metrics.process.uptimeFormatted}>
                    {metrics.process.uptimeFormatted}
                  </p>
                  <p className="text-[10px] text-muted-foreground mt-0.5">
                    Desde inicialização
                  </p>
                </div>

                <div className="col-span-2 sm:col-span-1 rounded-lg border bg-card/60 p-3 transition-colors hover:bg-card">
                  <div className="flex items-center gap-1.5 text-muted-foreground mb-1">
                    <PhoneCall className="h-3.5 w-3.5" />
                    <span className="text-xs">VoIP / Contas</span>
                  </div>
                  <p className="text-lg font-bold tracking-tight text-foreground">
                    {metrics.process.activeCalls}{" "}
                    <span className="text-xs font-normal text-muted-foreground">chamadas</span>
                  </p>
                  <p className="text-[10px] text-muted-foreground mt-0.5 flex items-center gap-1">
                    <Users className="h-2.5 w-2.5" />
                    {metrics.process.connectedSessions}/{metrics.process.totalSessions} online
                  </p>
                </div>
              </div>
            </div>

            {/* Seção 2: VPS / Máquina Host */}
            <div>
              <div className="flex items-center justify-between mb-2">
                <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
                  <Server className="h-3.5 w-3.5 text-blue-500" />
                  Servidor VPS (Host)
                </h4>
                <span className="text-[11px] text-muted-foreground font-mono">
                  {metrics.host.cpuCores} Cores de CPU
                </span>
              </div>

              <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-3">
                {/* Memória VPS */}
                <div className="rounded-lg border bg-card/60 p-3 space-y-2">
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-medium text-foreground flex items-center gap-1.5">
                      <HardDrive className="h-3.5 w-3.5 text-muted-foreground" />
                      Memória RAM
                    </span>
                    <span className="font-mono text-muted-foreground">
                      {metrics.host.memUsagePercent > 0 ? `${metrics.host.memUsagePercent.toFixed(1)}%` : "N/A"}
                    </span>
                  </div>
                  {metrics.host.memTotalMb > 0 ? (
                    <>
                      <div className="h-2 w-full overflow-hidden rounded-full bg-secondary">
                        <div
                          className={`h-full transition-all duration-500 ${
                            metrics.host.memUsagePercent > 85
                              ? "bg-rose-500"
                              : metrics.host.memUsagePercent > 70
                              ? "bg-amber-500"
                              : "bg-emerald-500"
                          }`}
                          style={{ width: `${Math.min(100, metrics.host.memUsagePercent)}%` }}
                        />
                      </div>
                      <div className="flex justify-between text-[10px] text-muted-foreground">
                        <span>Usada: {(metrics.host.memUsedMb / 1024).toFixed(2)} GB</span>
                        <span>Total: {(metrics.host.memTotalMb / 1024).toFixed(2)} GB</span>
                      </div>
                    </>
                  ) : (
                    <p className="text-xs text-muted-foreground">Disponível em Linux/Container</p>
                  )}
                </div>

                {/* CPU Load VPS */}
                <div className="rounded-lg border bg-card/60 p-3 space-y-2">
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-medium text-foreground flex items-center gap-1.5">
                      <Cpu className="h-3.5 w-3.5 text-muted-foreground" />
                      Carga de CPU
                    </span>
                    <span className="font-mono text-xs text-muted-foreground">
                      1m | 5m | 15m
                    </span>
                  </div>
                  <div className="flex items-baseline justify-between pt-1 font-mono text-sm font-semibold">
                    <span className={metrics.host.load1 > metrics.host.cpuCores ? "text-amber-500" : "text-foreground"}>
                      {metrics.host.load1.toFixed(2)}
                    </span>
                    <span className="text-muted-foreground text-xs">/</span>
                    <span className="text-foreground">
                      {metrics.host.load5.toFixed(2)}
                    </span>
                    <span className="text-muted-foreground text-xs">/</span>
                    <span className="text-foreground">
                      {metrics.host.load15.toFixed(2)}
                    </span>
                  </div>
                  <p className="text-[10px] text-muted-foreground">
                    Load Average normalizado p/ {metrics.host.cpuCores} núcleos
                  </p>
                </div>

                {/* Armazenamento em Disco */}
                <div className="rounded-lg border bg-card/60 p-3 space-y-2">
                  <div className="flex items-center justify-between text-xs">
                    <span className="font-medium text-foreground flex items-center gap-1.5">
                      <Server className="h-3.5 w-3.5 text-muted-foreground" />
                      Espaço em Disco
                    </span>
                    <span className="font-mono text-muted-foreground">
                      {metrics.host.diskUsagePercent > 0 ? `${metrics.host.diskUsagePercent.toFixed(0)}%` : "N/A"}
                    </span>
                  </div>
                  {metrics.host.diskTotalGb > 0 ? (
                    <>
                      <div className="h-2 w-full overflow-hidden rounded-full bg-secondary">
                        <div
                          className={`h-full transition-all duration-500 ${
                            metrics.host.diskUsagePercent > 90
                              ? "bg-rose-500"
                              : metrics.host.diskUsagePercent > 75
                              ? "bg-amber-500"
                              : "bg-blue-500"
                          }`}
                          style={{ width: `${Math.min(100, metrics.host.diskUsagePercent)}%` }}
                        />
                      </div>
                      <div className="flex justify-between text-[10px] text-muted-foreground">
                        <span>Livre: {metrics.host.diskFreeGb.toFixed(1)} GB</span>
                        <span>Total: {metrics.host.diskTotalGb.toFixed(1)} GB</span>
                      </div>
                    </>
                  ) : (
                    <p className="text-xs text-muted-foreground">Volume de dados local</p>
                  )}
                </div>
              </div>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
};
