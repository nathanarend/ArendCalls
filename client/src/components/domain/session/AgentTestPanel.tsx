import { useEffect, useRef, useState } from "react";
import { CheckCircle2, Loader2, PhoneOff, XCircle } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { HelpHint } from "@/components/shared/HelpHint";
import { type AgentCallTone, useAgentTest } from "@/hooks/useAgentTest";
import { cn } from "@/lib/utils";
import type { AgentTestRequest } from "@/types/session";

const TONE_CLASS: Record<AgentCallTone, string> = {
  progress: "text-muted-foreground",
  success: "text-primary",
  error: "text-destructive",
  neutral: "text-foreground",
};

const parseVariables = (text: string): AgentTestRequest["dynamicVariables"] | null => {
  if (text.trim() === "") return undefined;
  try {
    const parsed: unknown = JSON.parse(text);
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : null;
  } catch {
    return null;
  }
};

const ResultLine = ({ tone, message, detail }: { tone: AgentCallTone; message: string; detail?: string }) => (
  <p role="status" className={cn("flex items-start gap-1.5 text-xs", TONE_CLASS[tone])}>
    {tone === "progress" && <Loader2 className="mt-0.5 h-3.5 w-3.5 shrink-0 animate-spin" />}
    {tone === "success" && <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0" />}
    {tone === "error" && <XCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />}
    <span className="min-w-0 break-words">
      {message}
      {detail && <span className="block font-mono text-muted-foreground">{detail}</span>}
    </span>
  </p>
);

export const AgentTestPanel = ({ sessionId, keySet }: { sessionId: string; keySet: boolean }) => {
  const [agentId, setAgentId] = useState("");
  const [variables, setVariables] = useState("");
  const [phone, setPhone] = useState("");
  const { verify, runVerify, starting, view, callError, startCall, hangUp } = useAgentTest(sessionId);

  // A aba rola por dentro: o andamento da ligação não pode ficar fora da vista.
  const callStatusRef = useRef<HTMLDivElement>(null);
  const callMessage = callError || view?.message;
  useEffect(() => {
    if (callMessage) callStatusRef.current?.scrollIntoView({ block: "nearest" });
  }, [callMessage]);

  const parsedVariables = parseVariables(variables);
  const invalidVariables = parsedVariables === null;
  const request = (): AgentTestRequest => ({
    agentId: agentId.trim(),
    dynamicVariables: parsedVariables ?? undefined,
  });

  const canRun = keySet && agentId.trim() !== "" && !invalidVariables;
  const callActive = starting || view?.active === true;

  return (
    <div className="space-y-3 border-t border-dashed pt-4">
      <p className="flex items-center gap-2 text-sm font-semibold">
        Testar integração
        <HelpHint label="Como funciona o teste">
          Verificar confere a key, o agente e o formato de áudio, sem ligar para ninguém, e fica como uma conversa curta no
          histórico da ElevenLabs. Ligar faz uma ligação real, com o agente falando pelo WhatsApp, e consome créditos.
        </HelpHint>
      </p>
      {!keySet && <p className="text-xs text-muted-foreground">Salve a API key acima para testar.</p>}

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex flex-col gap-1">
          <Label htmlFor="agent-test-id" className="text-xs text-muted-foreground">Agent ID</Label>
          <Input
            id="agent-test-id"
            value={agentId}
            onChange={(e) => setAgentId(e.target.value)}
            placeholder="agent_..."
            autoComplete="off"
            className="font-mono text-sm"
          />
          <span className="text-xs text-muted-foreground">
            Na ElevenLabs, abra o agente: o ID é o trecho <code>agent_...</code> da URL.
          </span>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="agent-test-vars" className="text-xs text-muted-foreground">Variáveis em JSON (opcional)</Label>
          <Input
            id="agent-test-vars"
            value={variables}
            onChange={(e) => setVariables(e.target.value)}
            placeholder='{"nome":"Maria"}'
            autoComplete="off"
            aria-invalid={invalidVariables}
            className="font-mono text-sm"
          />
          {invalidVariables && <span className="text-xs text-destructive">Use um objeto JSON válido.</span>}
        </div>
      </div>

      <Button variant="secondary" onClick={() => runVerify(request())} disabled={!canRun || verify.kind === "running"}>
        {verify.kind === "running" ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
        Verificar conexão
      </Button>
      {verify.kind === "ok" && (
        <ResultLine tone="success" message="Conexão confirmada: key, agente e formato de áudio corretos." detail={verify.conversationId} />
      )}
      {verify.kind === "error" && <ResultLine tone="error" message={verify.message} />}

      <div className="flex flex-col gap-1">
        <Label htmlFor="agent-test-phone" className="text-xs text-muted-foreground">Telefone para a ligação de teste</Label>
        <div className="flex gap-2">
          <Input
            id="agent-test-phone"
            value={phone}
            onChange={(e) => setPhone(e.target.value)}
            placeholder="5511999999999"
            inputMode="tel"
            autoComplete="off"
            className="font-mono text-sm"
          />
          {view?.active ? (
            <Button variant="destructive" onClick={hangUp}>
              <PhoneOff className="mr-2 h-4 w-4" />
              Encerrar
            </Button>
          ) : (
            <Button variant="secondary" onClick={() => startCall(phone.trim(), request())} disabled={!canRun || phone.trim() === "" || callActive}>
              {starting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              Ligar
            </Button>
          )}
        </div>
      </div>
      <div ref={callStatusRef} className="space-y-3">
        {callError && <ResultLine tone="error" message={callError} />}
        {view && <ResultLine tone={view.tone} message={view.message} detail={view.conversationId} />}
      </div>
    </div>
  );
};
