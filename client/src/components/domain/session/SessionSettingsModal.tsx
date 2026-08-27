import { useState, useEffect } from "react";
import { Loader2, Link as LinkIcon } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { updateWebhookUrl } from "@/services/sessions";
import type { SessionInfo } from "@/types/session";

export const SessionSettingsModal = ({
  session,
  onClose,
  onUpdateSession
}: {
  session: SessionInfo | null;
  onClose: () => void;
  onUpdateSession: (updated: SessionInfo) => void;
}) => {
  const [webhookUrl, setWebhookUrl] = useState("");
  const [isSavingWebhook, setIsSavingWebhook] = useState(false);

  useEffect(() => {
    if (session) {
      setWebhookUrl(session.webhookUrl || "");
    }
  }, [session]);

  const handleUpdateWebhook = async () => {
    if (!session) return;
    setIsSavingWebhook(true);
    try {
      await updateWebhookUrl(session.id, webhookUrl);
      toast.success("Webhook salvo com sucesso!");
      onUpdateSession({ ...session, webhookUrl });
    } catch {
      toast.error("Erro ao salvar webhook.");
    } finally {
      setIsSavingWebhook(false);
    }
  };

  return (
    <Dialog open={!!session} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Configurações: {session?.name}</DialogTitle>
          <DialogDescription>
            Visualize informações de identificação e configure a URL de Webhook desta conta.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          {session && (
            <div className="space-y-2 border-b pb-4">
              <Label className="text-xs font-semibold text-muted-foreground uppercase">ID da Conta (session_id / sid)</Label>
              <div className="flex gap-2">
                <Input value={session.id} readOnly className="font-mono bg-muted/30" />
                <Button variant="secondary" onClick={() => {
                  navigator.clipboard.writeText(session.id);
                  toast.success("ID da conta copiado!");
                }}>Copiar ID</Button>
              </div>
              <p className="text-xs text-muted-foreground">
                Substitua o <code>{'{sid}'}</code> na rota da API por este código.
              </p>
            </div>
          )}

          <div className="pt-4 border-t border-border mt-4">
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
                <Button 
                  variant="secondary" 
                  onClick={handleUpdateWebhook}
                  disabled={isSavingWebhook}
                >
                  {isSavingWebhook ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
                  Salvar URL de Webhook
                </Button>
              </div>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};
