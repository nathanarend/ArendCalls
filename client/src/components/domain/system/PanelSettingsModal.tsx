import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useSessions } from "@/stores/sessions";
import { setPanelInboundCalls } from "@/services/sessions";

export const PanelSettingsModal = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) => {
  const inboundCalls = useSessions((s) => s.panelInboundCalls);
  const [saving, setSaving] = useState(false);

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
      </DialogContent>
    </Dialog>
  );
};
