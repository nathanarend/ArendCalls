import { useState } from "react";
import { Disc3, Phone, AlertCircle } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { DeviceSelector } from "@/components/form/DeviceSelector";
import { useStartCall } from "@/hooks/useStartCall";
import { useDevices } from "@/stores/devices";
import { useSessions } from "@/stores/sessions";

export const Dialer = ({ sid }: { sid: string }) => {
  const [phone, setPhone] = useState("");
  const [record, setRecord] = useState(false);
  const micId = useDevices((s) => s.micId);
  const startCall = useStartCall(sid, micId);
  
  const sessions = useSessions((s) => s.sessions);
  const currentSession = sessions.find((s) => s.id === sid);
  const isConnected = currentSession?.state === "open";
  const isStopped = currentSession?.state === "stopped";

  const submit = () => {
    if (!phone.trim() || startCall.isPending || !isConnected) return;
    startCall.mutate({ phone: phone.trim(), record }, { onSuccess: () => setPhone("") });
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center justify-between">
          <span>Discador</span>
          {isStopped && (
            <span className="text-xs font-normal text-amber-500 flex items-center gap-1">
              <AlertCircle className="w-3.5 h-3.5" /> Instância Parada (Clique em Ligar no cabeçalho)
            </span>
          )}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <DeviceSelector />
        <div className="flex flex-wrap items-center gap-2">
          <Input
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
            placeholder="+55 11 99999 9999"
            inputMode="tel"
            disabled={!isConnected}
            className="min-w-[200px] flex-1"
          />
          <Button
            type="button"
            variant={record ? "default" : "outline"}
            size="sm"
            onClick={() => setRecord((v) => !v)}
            disabled={!isConnected}
            aria-pressed={record}
          >
            <Disc3 className="h-4 w-4" />
            Gravar
          </Button>
          <Button onClick={submit} disabled={startCall.isPending || !phone.trim() || !isConnected}>
            <Phone className="h-4 w-4" />
            {startCall.isPending ? "Chamando…" : "Ligar"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
};
