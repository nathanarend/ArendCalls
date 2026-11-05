import { useEffect, useState } from "react";
import { Bot, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { HelpHint } from "@/components/shared/HelpHint";
import { AgentTestPanel } from "@/components/domain/session/AgentTestPanel";
import { getAgentConfig, updateAgentConfig } from "@/services/sessions";
import type { AgentConfig } from "@/types/session";

export const AgentConfigSection = ({ sessionId }: { sessionId: string }) => {
  const [config, setConfig] = useState<AgentConfig | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [isSaving, setIsSaving] = useState(false);

  useEffect(() => {
    setConfig(null);
    setApiKey("");
    getAgentConfig(sessionId)
      .then(setConfig)
      .catch(() => toast.error("Erro ao carregar configuração do agente."));
  }, [sessionId]);

  const save = async (elevenLabsApiKey: string) => {
    setIsSaving(true);
    try {
      const updated = await updateAgentConfig(sessionId, { elevenLabsApiKey });
      setConfig(updated);
      setApiKey("");
      toast.success(updated.elevenLabsApiKeySet ? "API key salva!" : "API key removida.");
    } catch {
      toast.error("Erro ao salvar a API key.");
    } finally {
      setIsSaving(false);
    }
  };

  const isSet = config?.elevenLabsApiKeySet === true;

  return (
    <div className="space-y-4">
      <p className="text-sm font-semibold flex items-center gap-2">
        <Bot className="w-4 h-4" />
        Agente de Voz (ElevenLabs)
        <HelpHint label="Para que serve esta integração">
          Exclusiva do modo agente: vale só para ligações iniciadas com <code>agent</code> no <code>POST /calls</code>.
          Ligações com atendente no painel não usam a ElevenLabs nem consomem créditos dela.
        </HelpHint>
        {config && (
          <span
            className={
              "ml-auto text-xs font-medium rounded-full px-2 py-0.5 " +
              (isSet ? "bg-primary/15 text-primary" : "bg-muted text-muted-foreground")
            }
          >
            {isSet ? "configurado" : "não configurado"}
          </span>
        )}
      </p>
      <p className="text-xs text-muted-foreground">
        Quando uma chamada é iniciada com <code>agent</code> no <code>POST /calls</code>, o agente fala com o
        contato no lugar do atendente. Configure o agente com <code>PCM 16 kHz</code> na entrada e na saída.
      </p>
      <div className="flex flex-col gap-1">
        <Label className="text-xs text-muted-foreground">
          API Key {isSet ? "(definida — deixe em branco para manter)" : ""}
        </Label>
        <Input
          type="password"
          autoComplete="off"
          value={apiKey}
          onChange={(e) => setApiKey(e.target.value)}
          placeholder={isSet ? "••••••••••" : "sk_..."}
          className="font-mono text-sm"
        />
      </div>
      <div className="flex gap-2">
        <Button
          variant="secondary"
          onClick={() => save(apiKey.trim())}
          disabled={isSaving || !config || apiKey.trim() === ""}
        >
          {isSaving ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
          Salvar API key
        </Button>
        {isSet && (
          <Button variant="ghost" onClick={() => save("")} disabled={isSaving}>
            Remover
          </Button>
        )}
      </div>
      <AgentTestPanel sessionId={sessionId} keySet={isSet} />
    </div>
  );
};
