import { useState } from "react";
import { toast } from "sonner";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { setPanelInbound } from "@/services/sessions";
import type { SessionInfo } from "@/types/session";

export const GeneralConfigSection = ({
  session,
  onUpdateSession,
}: {
  session: SessionInfo;
  onUpdateSession: (updated: SessionInfo) => void;
}) => {
  const [panelInbound, setPanelInboundState] = useState(session.panelInbound === true);
  const [isSavingPanel, setIsSavingPanel] = useState(false);

  const handleTogglePanelInbound = async (enabled: boolean) => {
    const prev = panelInbound;
    setPanelInboundState(enabled);
    setIsSavingPanel(true);
    try {
      await setPanelInbound(session.id, enabled);
      onUpdateSession({ ...session, panelInbound: enabled });
    } catch {
      setPanelInboundState(prev);
      toast.error("Erro ao salvar a preferência de atendimento pelo painel.");
    } finally {
      setIsSavingPanel(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label className="text-xs font-semibold text-muted-foreground uppercase">ID da Conta (session_id / sid)</Label>
        <div className="flex gap-2">
          <Input value={session.id} readOnly className="font-mono bg-muted/30" />
          <Button
            variant="secondary"
            onClick={() => {
              navigator.clipboard.writeText(session.id);
              toast.success("ID da conta copiado!");
            }}
          >
            Copiar ID
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">
          Substitua o <code>{"{sid}"}</code> na rota da API por este código.
        </p>
      </div>

      <label className="flex items-start gap-2.5 text-sm border-t border-dashed pt-4">
        <input
          type="checkbox"
          className="mt-0.5 h-4 w-4 rounded border-border accent-primary"
          checked={panelInbound}
          disabled={isSavingPanel}
          onChange={(e) => handleTogglePanelInbound(e.target.checked)}
        />
        <span>
          Forçar exibição das chamadas recebidas desta conta no painel
          <span className="block text-xs text-muted-foreground">
            Bypass de teste: mostra as chamadas <strong>desta conta</strong> aqui mesmo com o switch global
            de recebimento <strong>desligado</strong>. Nunca esconde — quem esconde é o switch global.
          </span>
        </span>
      </label>
    </div>
  );
};
