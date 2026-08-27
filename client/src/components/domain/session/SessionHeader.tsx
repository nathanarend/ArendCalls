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
          className="text-xs font-bold gap-1.5"
        >
          <Play className="h-3.5 w-3.5 fill-current text-indigo-500" />
          Ligar
        </Button>

        <Button 
          variant="outline" 
          size="sm" 
          disabled={busy || session.state === "stopped"} 
          onClick={() => run(() => stopSession(session.id))}
          className="text-xs font-bold gap-1.5"
        >
          <Square className="h-3.5 w-3.5 fill-current text-zinc-500" />
          Parar
        </Button>

        <Button 
          variant="outline" 
          size="sm" 
          disabled={busy} 
          onClick={() => run(() => restartSession(session.id))}
          className="text-xs font-bold gap-1.5"
        >
          <RefreshCcw className={`h-3.5 w-3.5 ${busy ? "animate-spin" : ""}`} />
          Reiniciar
        </Button>

        {session.paired ? (
          <Button 
            variant="ghost" 
            size="sm" 
            disabled={busy} 
            onClick={() => run(() => logoutSession(session.id))}
            className="text-xs font-bold gap-1.5 text-rose-500 hover:text-rose-600 hover:bg-rose-500/10"
          >
            <LogOut className="h-3.5 w-3.5" />
            Desconectar
          </Button>
        ) : (
          <Button 
            size="sm" 
            disabled={busy} 
            onClick={() => run(() => pairSession(session.id))}
            className="text-xs font-bold gap-1.5"
          >
            {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <QrCode className="h-3.5 w-3.5" />}
            Reconectar
          </Button>
        )}
      </div>
    </div>
  );
};
