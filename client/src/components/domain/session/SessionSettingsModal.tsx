import { Bot, Disc3, Link as LinkIcon, Settings2 } from "lucide-react";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { AgentConfigSection } from "@/components/domain/session/AgentConfigSection";
import { GeneralConfigSection } from "@/components/domain/session/GeneralConfigSection";
import { RecordingConfigSection } from "@/components/domain/session/RecordingConfigSection";
import { WebhookConfigSection } from "@/components/domain/session/WebhookConfigSection";
import type { SessionInfo } from "@/types/session";

const triggerIconClass = "hidden w-4 h-4 sm:block";

// forceMount: trocar de aba não pode apagar o que foi digitado e ainda não salvo.
const panelClass = "min-h-0 flex-1 overflow-y-auto p-1 data-[state=inactive]:hidden";

export const SessionSettingsModal = ({
  session,
  onClose,
  onUpdateSession,
}: {
  session: SessionInfo | null;
  onClose: () => void;
  onUpdateSession: (updated: SessionInfo) => void;
}) => (
  <Dialog open={!!session} onOpenChange={(o) => !o && onClose()}>
    <DialogContent className="sm:max-w-xl h-[min(85vh,640px)] grid-rows-[auto_minmax(0,1fr)]">
      <DialogHeader>
        <DialogTitle>Configurações: {session?.name}</DialogTitle>
        <DialogDescription>Cada aba salva as próprias alterações.</DialogDescription>
      </DialogHeader>
      {session && (
        <Tabs key={session.id} defaultValue="general" className="flex min-h-0 flex-col gap-4">
          <TabsList className="grid shrink-0 grid-cols-4">
            <TabsTrigger value="general">
              <Settings2 className={triggerIconClass} />
              Geral
            </TabsTrigger>
            <TabsTrigger value="webhook">
              <LinkIcon className={triggerIconClass} />
              Webhook
            </TabsTrigger>
            <TabsTrigger value="agent">
              <Bot className={triggerIconClass} />
              Agente
            </TabsTrigger>
            <TabsTrigger value="recording">
              <Disc3 className={triggerIconClass} />
              Gravação
            </TabsTrigger>
          </TabsList>
          <TabsContent value="general" forceMount className={panelClass}>
            <GeneralConfigSection session={session} onUpdateSession={onUpdateSession} />
          </TabsContent>
          <TabsContent value="webhook" forceMount className={panelClass}>
            <WebhookConfigSection session={session} onUpdateSession={onUpdateSession} />
          </TabsContent>
          <TabsContent value="agent" forceMount className={panelClass}>
            <AgentConfigSection sessionId={session.id} />
          </TabsContent>
          <TabsContent value="recording" forceMount className={panelClass}>
            <RecordingConfigSection sessionId={session.id} />
          </TabsContent>
        </Tabs>
      )}
    </DialogContent>
  </Dialog>
);
