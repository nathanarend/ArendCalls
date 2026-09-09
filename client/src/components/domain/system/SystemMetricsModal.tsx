import { useState, useEffect, useCallback } from "react";
import {
  Activity,
  Cpu,
  HardDrive,
  HelpCircle,
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
import { useAudioDebug } from "@/stores/audioDebug";

interface SystemMetricsModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const fmt = (v: number, unit: string, dp = 0) => (v < 0 ? "n/a" : `${v.toFixed(dp)}${unit}`);

export const SystemMetricsModal = ({ open, onOpenChange }: SystemMetricsModalProps) => {
  const [metrics, setMetrics] = useState<SystemMetricsResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const audioDebug = useAudioDebug((s) => s.enabled);
  const setAudioDebug = useAudioDebug((s) => s.setEnabled);
  const audioSamples = useAudioDebug((s) => s.samples);
  const last = audioSamples[audioSamples.length - 1];
  const [showAudioHelp, setShowAudioHelp] = useState(false);

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

        {/* Seção 3: Debug de áudio (navegador) */}
        <div className="border-t pt-3">
          <div className="flex items-start gap-2.5 text-sm">
            <input
              type="checkbox"
              id="audio-debug-toggle"
              className="mt-0.5 h-4 w-4 rounded border-border accent-primary"
              checked={audioDebug}
              onChange={(e) => setAudioDebug(e.target.checked)}
            />
            <div className="flex-1">
              <div className="flex items-center gap-1.5">
                <label htmlFor="audio-debug-toggle">Debug de áudio (navegador)</label>
                <button
                  type="button"
                  onClick={() => setShowAudioHelp((v) => !v)}
                  className="text-muted-foreground hover:text-foreground"
                  title="O que cada número significa"
                  aria-label="Ajuda"
                >
                  <HelpCircle className="h-3.5 w-3.5" />
                </button>
              </div>
              <span className="block text-xs text-muted-foreground">
                Liga o log <code className="font-mono">[arendcalls]</code> no console e o painel abaixo, a cada 2&nbsp;s.
                É por navegador (fica salvo), seguro deixar em produção. Faça uma ligação para ver dados.
              </span>
            </div>
          </div>

          {showAudioHelp && (
            <dl className="mt-2 space-y-1.5 rounded-md border bg-muted/30 p-3 text-xs">
              {[
                ["Recepção · frames / 2s", "Pedacinhos de áudio do contato que chegaram no painel nos últimos 2 s. O normal é ~33. Cai muito quando o contato está calado (o celular para de mandar — isso é esperado)."],
                ["Recepção · silêncio no buffer", "Quanto tempo, na janela de 2 s, o painel ficou sem áudio para tocar e emitiu mudo. Nas pausas da fala do contato é normal. Alto ENQUANTO ele fala sem parar = problema real (picote)."],
                ["Recepção · buffer", "Quantos milissegundos de áudio estão guardados esperando para tocar. Fica baixo de propósito (sem buffer, para não atrasar a voz). 'mín na janela' é o menor valor visto nos 2 s."],
                ["Envio · frames / 2s", "Pedacinhos do microfone do atendente enviados nos últimos 2 s. O normal é ~250 e constante. Se oscilar muito, a captura está engasgando."],
                ["Envio · fila de envio", "Bytes do microfone parados na fila esperando ir pela rede. Perto de 0 é saudável. Crescendo = a rede/CPU não dá conta de mandar e a voz do atendente vai picotar do outro lado."],
                ["Navegador · thread de áudio", "Quanto da capacidade da placa de som está sendo usada por chamada de processamento. Perto de 100% = risco de falha. Só aparece no Chrome 123+; senão fica 'n/a'."],
                ["Navegador · starvation áudio", "Fração das vezes que o sistema de áudio não conseguiu processar a tempo. Qualquer valor acima de 0 é ruim. Só no Chrome 123+."],
                ["Navegador · heap JS", "Memória usada pelo código da página. Só um pedaço da memória total do navegador. Serve para ver se está crescendo sem parar (vazamento)."],
                ["Navegador · lag do event loop", "Quanto o cronômetro de 2 s atrasou. Perto de 0 = a aba está fluida. Alto = a página travou por um instante (CPU sobrecarregada)."],
                ["ok / com falhas", "Resumo automático da recepção. Marca 'com falhas' só se pelo menos 2 janelas recentes (fora as do início da chamada) tiveram um buraco de áudio maior que 300 ms. Os cliquezinhos de 20–70 ms na borda das falas não contam."],
              ].map(([t, d]) => (
                <div key={t}>
                  <dt className="font-semibold text-foreground">{t}</dt>
                  <dd className="text-muted-foreground">{d}</dd>
                </div>
              ))}
            </dl>
          )}

          {audioDebug && (
            <div className="mt-3 space-y-3">
              {!last ? (
                <p className="rounded-md border border-dashed bg-card/40 p-3 text-center text-xs text-muted-foreground">
                  Aguardando uma chamada ativa…
                </p>
              ) : (
                (() => {
                  const w = audioSamples.slice(-15); // janela recente (~30 s)
                  const nums = (f: (s: (typeof w)[number]) => number) => w.map(f);
                  const min = (a: number[]) => Math.min(...a);
                  const max = (a: number[]) => Math.max(...a);
                  const avg = (a: number[]) => Math.round(a.reduce((x, y) => x + y, 0) / a.length);
                  const peers = nums((s) => s.peerPerSec);
                  // "falha" = buraco real de áudio (> 300 ms), não os cliquezinhos de
                  // 20-70 ms na borda das falas. Ignora as 2 primeiras amostras
                  // (conexão da chamada). Só marca "com falhas" se recorrer (>= 2).
                  const DROP_MS = 300;
                  const settled = w.slice(2);
                  const dropWins = settled.filter((s) => s.underrunMs > DROP_MS).length;
                  const silentWins = w.filter((s) => s.underrunMs > 0).length;
                  const rxBad = dropWins >= 2;
                  const txBad = max(nums((s) => s.dcTxBufBytes)) > 4096;

                  const Card = ({
                    k,
                    v,
                    sub,
                    bad,
                  }: {
                    k: string;
                    v: string;
                    sub?: string;
                    bad?: boolean;
                  }) => (
                    <div className="rounded-lg border bg-card/60 p-2.5">
                      <div className="text-[10px] uppercase tracking-wide text-muted-foreground">{k}</div>
                      <div className={`text-base font-bold tracking-tight ${bad ? "text-rose-500" : "text-foreground"}`}>{v}</div>
                      {sub && <div className="text-[10px] text-muted-foreground">{sub}</div>}
                    </div>
                  );

                  return (
                    <>
                      {/* Recepção */}
                      <div>
                        <div className="mb-1.5 flex items-center justify-between">
                          <h4 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                            Recepção · contato → painel
                          </h4>
                          <span className={`text-[11px] ${rxBad ? "text-rose-500" : "text-emerald-600 dark:text-emerald-400"}`}>
                            {rxBad ? `com falhas · ${dropWins} buracos >${DROP_MS}ms` : "ok"} · últimos {w.length * 2}s
                          </span>
                        </div>
                        <div className="grid grid-cols-3 gap-2">
                          <Card
                            k="frames / 2s"
                            v={String(last.peerPerSec)}
                            sub={`${min(peers)}–${max(peers)} · méd ${avg(peers)} · ideal ~33`}
                          />
                          <Card
                            k="silêncio no buffer"
                            v={fmt(last.underrunMs, " ms")}
                            sub={`${silentWins}/${w.length} janelas c/ silêncio · máx ${fmt(max(nums((s) => s.underrunMs)), " ms")}`}
                            bad={last.underrunMs > DROP_MS}
                          />
                          <Card
                            k="buffer"
                            v={fmt(last.fillMs, " ms")}
                            sub={`mín ${fmt(last.minFillMs, " ms")} na janela`}
                          />
                        </div>
                        <p className="mt-1 text-[10px] text-muted-foreground">
                          Silêncio no buffer nas pausas da fala do contato é normal (DTX). Só é problema se acontecer com ele falando sem parar.
                        </p>
                      </div>

                      {/* Envio */}
                      <div>
                        <h4 className="mb-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                          Envio · painel → contato
                        </h4>
                        <div className="grid grid-cols-2 gap-2">
                          <Card
                            k="frames / 2s"
                            v={String(last.micPerSec)}
                            sub={`méd ${avg(nums((s) => s.micPerSec))} · ideal ~250`}
                          />
                          <Card
                            k="fila de envio"
                            v={`${last.dcTxBufBytes} B`}
                            sub={`máx ${max(nums((s) => s.dcTxBufBytes))} B · >0 crescendo = engasgo`}
                            bad={txBad}
                          />
                        </div>
                      </div>

                      {/* Navegador */}
                      <div>
                        <h4 className="mb-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                          Navegador
                        </h4>
                        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                          <Card k="thread de áudio" v={fmt(last.audioLoadPct, "%")} bad={last.audioLoadPct >= 90} />
                          <Card k="starvation áudio" v={fmt(last.audioUnderrunPct, "%", 1)} bad={last.audioUnderrunPct > 0} />
                          <Card k="heap JS" v={fmt(last.jsHeapMb, " MB", 1)} />
                          <Card k="lag do event loop" v={fmt(Math.max(0, last.loopLagMs), " ms")} bad={last.loopLagMs > 200} />
                        </div>
                      </div>

                      {/* Histórico */}
                      <div className="max-h-28 overflow-y-auto rounded-md border bg-muted/40 p-2 font-mono text-[10px] leading-relaxed text-muted-foreground">
                        {audioSamples
                          .slice()
                          .reverse()
                          .map((s) => (
                            <div key={s.ts} className="whitespace-nowrap">
                              {new Date(s.ts).toLocaleTimeString()}
                              {"  rx "}
                              <span className="text-foreground">{String(s.peerPerSec).padStart(2)}</span>
                              {" / silêncio "}
                              <span className={s.underrunMs > 0 ? "text-rose-500" : ""}>{String(s.underrunMs).padStart(4)}ms</span>
                              {" / buffer "}
                              {String(s.fillMs).padStart(3)}ms
                              {"   tx "}
                              {String(s.micPerSec).padStart(3)}
                              {" / fila "}
                              {s.dcTxBufBytes}B
                            </div>
                          ))}
                      </div>
                    </>
                  );
                })()
              )}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
};
