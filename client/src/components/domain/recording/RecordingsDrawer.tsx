import { useState } from "react";
import { Disc3, AlertTriangle } from "lucide-react";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { EmptyState } from "@/components/shared/EmptyState";
import { useRecordings } from "@/hooks/useRecordings";
import type { RecordingItem, RecordingStatus } from "@/types/session";

const statusLabel: Record<RecordingStatus, string> = {
  recording: "Gravando",
  uploading: "Enviando",
  ready: "Pronta",
  failed: "Falhou",
  skipped: "Ignorada (<5s)",
};

const statusVariant: Record<RecordingStatus, "success" | "secondary" | "muted" | "destructive"> = {
  recording: "secondary",
  uploading: "secondary",
  ready: "success",
  failed: "destructive",
  skipped: "muted",
};

const fmtDuration = (ms: number) => {
  const s = Math.round(ms / 1000);
  return `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
};

const fmtBytes = (n: number) => {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
};

const RecordingRow = ({ r }: { r: RecordingItem }) => (
  <li className="rounded-lg border p-3 space-y-1">
    <div className="flex items-center justify-between gap-2">
      <span className="font-medium truncate">{r.peer || r.callId}</span>
      <Badge variant={statusVariant[r.status]}>{statusLabel[r.status]}</Badge>
    </div>
    <p className="text-xs text-muted-foreground">
      {r.direction === "outbound" ? "Efetuada" : "Recebida"}
      {r.durationMs > 0 && ` · ${fmtDuration(r.durationMs)}`}
      {r.startedAt > 0 && ` · ${new Date(r.startedAt).toLocaleString()}`}
    </p>
    {r.status === "ready" && (
      <p className="text-xs text-muted-foreground truncate">
        B2: <code>{r.b2Key}</code> · webhook {r.notified ? "avisado" : "pendente"}
      </p>
    )}
    {r.status === "failed" && (
      <p className="text-xs text-destructive">
        {r.error || "erro desconhecido"} · {r.uploadAttempts + r.notifyAttempts} tentativa(s)
      </p>
    )}
  </li>
);

export const RecordingsDrawer = ({ sid }: { sid: string }) => {
  const [open, setOpen] = useState(false);
  const { data } = useRecordings(sid, open);

  const recs = data?.recordings ?? [];
  const stats = data?.stats ?? {};

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="rounded-full px-3.5 text-xs font-semibold gap-1.5 shadow-xs border-border/80 bg-background/90 hover:bg-muted/80 hover:border-border transition-all duration-150 active:scale-95"
        >
          <Disc3 className="h-3.5 w-3.5 text-muted-foreground" />
          Gravações
        </Button>
      </SheetTrigger>
      <SheetContent side="right" className="w-full p-0 sm:max-w-md">
        <SheetHeader className="p-6 pb-4">
          <SheetTitle>Gravações da conta</SheetTitle>
        </SheetHeader>
        <Separator />
        <ScrollArea className="h-[calc(100vh-5.5rem)] px-6 py-4 space-y-3">
          {data && !data.configured && (
            <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-xs flex gap-2">
              <AlertTriangle className="h-4 w-4 shrink-0 text-amber-500" />
              <span>
                Destino de gravação incompleto. Configure B2 e webhook juntos em Configurações da conta.
                {data.missing.length > 0 && ` Falta: ${data.missing.join(", ")}.`}
              </span>
            </div>
          )}

          {data && (
            <div className="flex flex-wrap gap-1.5 text-xs">
              {(["uploading", "ready", "failed", "skipped"] as RecordingStatus[]).map((s) =>
                stats[s] ? (
                  <Badge key={s} variant={statusVariant[s]}>
                    {statusLabel[s]}: {stats[s]}
                  </Badge>
                ) : null,
              )}
              <span className="text-muted-foreground self-center">
                disco local: {fmtBytes(data.diskUsageBytes)}
              </span>
            </div>
          )}

          {recs.length === 0 ? (
            <EmptyState title="Nenhuma gravação" description="Chamadas iniciadas com record: true aparecem aqui." />
          ) : (
            <ul className="space-y-2">
              {recs.map((r) => (
                <RecordingRow key={r.callId} r={r} />
              ))}
            </ul>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
};
