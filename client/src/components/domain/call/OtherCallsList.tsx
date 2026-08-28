import { Phone, PhoneOff } from "lucide-react";
import { toast } from "sonner";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatCallDuration } from "@/utils/format";
import { endCall } from "@/services/calls";
import type { CallSummary } from "@/types/call";

export const OtherCallsList = ({ calls }: { calls: CallSummary[] }) => {
  if (calls.length === 0) return null;

  const handleEnd = async (sessionId: string, callId: string) => {
    try {
      await endCall(sessionId, callId);
      toast.success("Chamada encerrada.");
    } catch (e) {
      toast.error("Erro ao encerrar chamada: " + (e as Error).message);
    }
  };

  return (
    <section className="space-y-3">
      <h2 className="text-sm font-medium text-muted-foreground">Outras chamadas ativas</h2>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {calls.map((c) => (
          <Card key={c.callId} className="opacity-95">
            <CardContent className="flex items-center gap-3 p-3">
              <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <Phone className="h-4 w-4" />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{c.peerName || c.peer}</p>
                <p className="text-xs text-muted-foreground">
                  {c.direction === "inbound" ? "Recebida" : "Realizada"}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <Badge variant="muted">{formatCallDuration(c.startedAt, c.status)}</Badge>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-8 w-8 text-destructive hover:bg-destructive/10 hover:text-destructive"
                  onClick={() => handleEnd(c.sessionId, c.callId)}
                  title="Encerrar chamada"
                >
                  <PhoneOff className="h-4 w-4" />
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
};
