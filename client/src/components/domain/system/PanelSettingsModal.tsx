import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useSessions } from "@/stores/sessions";
import { setPanelInboundCalls } from "@/services/sessions";
import { type AudioTransport, useAudioTransport } from "@/stores/audioTransport";

const TRANSPORT_OPTIONS: { value: AudioTransport; label: string; help: string }[] = [
  { value: "webrtc", label: "WebRTC (padrão)", help: "Exige UDP direto até o servidor." },
  { value: "websocket", label: "WebSocket", help: "Funciona atrás de proxy/tunnel, como o Cloudflare Tunnel." },
];

export const PanelSettingsModal = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) => {
  const inboundCalls = useSessions((s) => s.panelInboundCalls);
  const [saving, setSaving] = useState(false);
  const transport = useAudioTransport((s) => s.transport);
  const setTransport = useAudioTransport((s) => s.setTransport);

  const toggle = async (enabled: boolean) => {
    const prev = inboundCalls;
    useSessions.setState({ panelInboundCalls: enabled });
    setSaving(true);
    try {
      await setPanelInboundCalls(enabled);
    } catch {
      useSessions.setState({ panelInboundCalls: prev });
      toast.error("Erro ao salvar a configuração global.");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Configurações do Painel</DialogTitle>
          <DialogDescription>Preferências globais deste painel de operação.</DialogDescription>
        </DialogHeader>
        <label className="flex items-start gap-2.5 text-sm py-2">
          <input
            type="checkbox"
            className="mt-0.5 h-4 w-4 rounded border-border accent-primary"
            checked={inboundCalls}
            disabled={saving}
            onChange={(e) => toggle(e.target.checked)}
          />
          <span>
            Mostrar chamadas recebidas no painel
            {saving && <Loader2 className="ml-2 inline h-3.5 w-3.5 animate-spin" />}
            <span className="block text-xs text-muted-foreground">
              Desligado: nenhuma conta toca ou abre o modal de chamada recebida aqui — os eventos SSE/webhook
              continuam saindo normalmente para os apps externos tratarem. Uma conta específica pode furar essa
              regra pelo bypass em <strong>Configurações da conta</strong> (OU lógico).
            </span>
          </span>
        </label>
        <fieldset className="space-y-2 border-t border-border pt-3 text-sm">
          <legend className="pb-1 font-medium">Transporte de áudio (neste navegador)</legend>
          {TRANSPORT_OPTIONS.map((o) => (
            <label key={o.value} className="flex items-start gap-2.5">
              <input
                type="radio"
                name="audio-transport"
                className="mt-0.5 h-4 w-4 accent-primary"
                checked={transport === o.value}
                onChange={() => setTransport(o.value)}
              />
              <span>
                {o.label}
                <span className="block text-xs text-muted-foreground">{o.help}</span>
              </span>
            </label>
          ))}
          <p className="text-xs text-muted-foreground">Vale a partir da próxima chamada.</p>
        </fieldset>
      </DialogContent>
    </Dialog>
  );
};
