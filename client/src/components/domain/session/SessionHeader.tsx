import { useState } from "react";
import { Loader2, Play, Square, RefreshCcw, LogOut, QrCode } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { logoutSession, pairSession, startSession, stopSession, restartSession } from "@/services/sessions";
import type { SessionInfo, SessionState } from "@/types/session";

const statusLabel: Record<SessionState, string> = {
  open: "Conectado",
  qr: "Escanear QR",
  stopped: "Parado",
  connecting: "Conectando…",
  logged_out: "Desconectado",
};

const statusVariant: Record<SessionState, "success" | "secondary" | "muted" | "destructive"> = {
  open: "success",
  qr: "secondary",
  stopped: "muted",
  connecting: "secondary",
  logged_out: "destructive",
};

export const SessionHeader = ({ session }: { session: SessionInfo }) => {
  const [busy, setBusy] = useState(false);

  const run = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      await fn();
      toast.success("Ação executada com sucesso!");
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto flex max-w-3xl flex-wrap items-center justify-between gap-3">
      <div className="flex min-w-0 items-center gap-2">
        <h1 className="truncate text-xl font-semibold tracking-tight">{session.name}</h1>
        <Badge variant={statusVariant[session.state]}>{statusLabel[session.state]}</Badge>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button 
          variant="outline" 
          size="sm" 
          disabled={busy || session.state === "open"} 
          onClick={() => run(() => startSession(session.id))}
          className="rounded-full px-3.5 text-xs font-semibold gap-1.5 shadow-xs border-indigo-200/70 dark:border-indigo-900/40 bg-indigo-50/40 dark:bg-indigo-950/20 text-indigo-600 dark:text-indigo-400 hover:bg-indigo-100/70 hover:text-indigo-700 transition-all duration-150 active:scale-95"
        >
          <Play className="h-3 w-3 fill-current text-indigo-500" />
          Ligar
        </Button>

        <Button 
          variant="outline" 
          size="sm" 
          disabled={busy || session.state === "stopped"} 
          onClick={() => run(() => stopSession(session.id))}
          className="rounded-full px-3.5 text-xs font-semibold gap-1.5 shadow-xs border-zinc-200 dark:border-zinc-800 bg-zinc-50/50 dark:bg-zinc-900/40 text-zinc-700 dark:text-zinc-300 hover:bg-zinc-100/80 hover:text-zinc-900 transition-all duration-150 active:scale-95"
        >
          <Square className="h-3 w-3 fill-current text-zinc-500" />
          Parar
        </Button>

        <Button 
          variant="outline" 
          size="sm" 
          disabled={busy} 
          onClick={() => run(() => restartSession(session.id))}
          className="rounded-full px-3.5 text-xs font-semibold gap-1.5 shadow-xs border-border/80 bg-background/90 hover:bg-muted/80 hover:border-border transition-all duration-150 active:scale-95"
        >
          <RefreshCcw className={`h-3 w-3 ${busy ? "animate-spin" : ""}`} />
          Reiniciar
        </Button>

        {session.paired ? (
          <Button 
            variant="outline" 
            size="sm" 
            disabled={busy} 
            onClick={() => run(() => logoutSession(session.id))}
            className="rounded-full px-3.5 text-xs font-semibold gap-1.5 shadow-xs border-rose-200/70 dark:border-rose-900/40 bg-rose-50/40 dark:bg-rose-950/20 text-rose-600 dark:text-rose-400 hover:bg-rose-100/80 hover:text-rose-700 hover:border-rose-300/80 transition-all duration-150 active:scale-95"
          >
            <LogOut className="h-3 w-3" />
            Desconectar
          </Button>
        ) : (
          <Button 
            size="sm" 
            disabled={busy} 
            onClick={() => run(() => pairSession(session.id))}
            className="rounded-full px-4 text-xs font-semibold gap-1.5 shadow-sm transition-all duration-150 active:scale-95"
          >
            {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <QrCode className="h-3.5 w-3.5" />}
            Reconectar
          </Button>
        )}
      </div>
    </div>
  );
};
