import { useState } from "react";
import { Link as LinkIcon, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { updateWebhookUrl } from "@/services/sessions";
import type { SessionInfo } from "@/types/session";

export const WebhookConfigSection = ({
  session,
  onUpdateSession,
}: {
  session: SessionInfo;
  onUpdateSession: (updated: SessionInfo) => void;
}) => {
  const [webhookUrl, setWebhookUrl] = useState(session.webhookUrl || "");
  const [isSaving, setIsSaving] = useState(false);

  const handleSave = async () => {
    setIsSaving(true);
    try {
      await updateWebhookUrl(session.id, webhookUrl);
      toast.success("Webhook salvo com sucesso!");
      onUpdateSession({ ...session, webhookUrl });
    } catch {
      toast.error("Erro ao salvar webhook.");
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <div className="space-y-4">
      <p className="text-sm font-semibold flex items-center gap-2">
        <LinkIcon className="w-4 h-4" />
        Webhook (Eventos em tempo real)
      </p>
      <p className="text-xs text-muted-foreground">
        Especifique uma URL HTTP para receber os eventos (Ringing, Accepted, Terminated) desta conta via POST. Isso anula a URL global do sistema.
      </p>
      <div className="flex flex-col gap-2">
        <Input
          value={webhookUrl}
          onChange={(e) => setWebhookUrl(e.target.value)}
          placeholder="https://seu-sistema.com/api/webhook"
          className="font-mono text-sm"
        />
        <Button variant="secondary" onClick={handleSave} disabled={isSaving}>
          {isSaving ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
          Salvar URL de Webhook
        </Button>
      </div>
    </div>
  );
};
