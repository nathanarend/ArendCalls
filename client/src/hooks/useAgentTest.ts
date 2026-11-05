import { useEffect, useRef, useState } from "react";
import { apiErrorMessage } from "@/lib/api";
import { eventStream, type BrokerEvent } from "@/lib/event-stream";
import { endCall, startAgentCall } from "@/services/calls";
import { testAgent } from "@/services/sessions";
import type { AgentTestRequest } from "@/types/session";

type CallEvent = Extract<BrokerEvent, { type: "call-status" | "call-agent" | "call-ended" }>;

export type AgentVerifyState =
  | { kind: "idle" }
  | { kind: "running" }
  | { kind: "ok"; conversationId: string }
  | { kind: "error"; message: string };

export type AgentCallTone = "progress" | "success" | "error" | "neutral";

export type AgentCallView = {
  active: boolean;
  tone: AgentCallTone;
  message: string;
  conversationId: string;
} | null;

const END_REASONS: Record<string, string> = {
  declined: "Ligação recusada.",
  timeout: "Ligação não atendida.",
  busy: "Número ocupado.",
  cancelled: "Ligação cancelada.",
  do_not_disturb: "Contato em não perturbe.",
  failed: "A ligação falhou.",
  user_ended: "Ligação encerrada antes do agente conectar.",
};

const describeCall = (callId: string, events: CallEvent[]): AgentCallView => {
  const own = events.filter((e) => e.id === callId);
  const agent = own.filter((e) => e.type === "call-agent").at(-1);
  const connected = own.find((e) => e.type === "call-agent" && e.status === "connected");
  const conversationId = connected?.type === "call-agent" ? connected.conversationId : "";
  const ended = own.find((e) => e.type === "call-ended");

  if (agent?.type === "call-agent" && agent.status === "failed") {
    return { active: !ended, tone: "error", message: `O agente falhou: ${agent.error}`, conversationId };
  }
  if (ended?.type === "call-ended") {
    return connected
      ? { active: false, tone: "success", message: "Ligação encerrada. A integração funcionou.", conversationId }
      : { active: false, tone: "neutral", message: END_REASONS[ended.reason] ?? `Ligação encerrada (${ended.reason}).`, conversationId };
  }
  if (connected) {
    return { active: true, tone: "success", message: "Agente em conversa. A integração funcionou.", conversationId };
  }
  const status = own.filter((e) => e.type === "call-status").at(-1);
  const stage = status?.type === "call-status" ? status.status : "starting";
  return {
    active: true,
    tone: "progress",
    message: stage === "ringing" ? "Chamando…" : stage === "connected" ? "Conectando ao agente…" : "Iniciando a ligação…",
    conversationId,
  };
};

export const useAgentTest = (sessionId: string) => {
  const [verify, setVerify] = useState<AgentVerifyState>({ kind: "idle" });
  const [starting, setStarting] = useState(false);
  const [callId, setCallId] = useState<string | null>(null);
  const [callError, setCallError] = useState("");
  const [events, setEvents] = useState<CallEvent[]>([]);
  const listening = useRef(false);

  // Liga antes do POST: os primeiros eventos podem chegar antes da resposta com o callId.
  useEffect(
    () =>
      eventStream.on((ev) => {
        if (!listening.current) return;
        if ((ev.type === "call-status" || ev.type === "call-agent" || ev.type === "call-ended") && ev.sessionId === sessionId) {
          setEvents((list) => [...list, ev]);
        }
      }),
    [sessionId],
  );

  const runVerify = async (agent: AgentTestRequest) => {
    setVerify({ kind: "running" });
    try {
      const { conversationId } = await testAgent(sessionId, agent);
      setVerify({ kind: "ok", conversationId });
    } catch (e) {
      setVerify({ kind: "error", message: apiErrorMessage(e) });
    }
  };

  const startCall = async (phone: string, agent: AgentTestRequest) => {
    setStarting(true);
    setCallError("");
    setCallId(null);
    setEvents([]);
    listening.current = true;
    try {
      const { call } = await startAgentCall(sessionId, phone, agent);
      setCallId(call.callId);
    } catch (e) {
      listening.current = false;
      setCallError(apiErrorMessage(e));
    } finally {
      setStarting(false);
    }
  };

  const view = callId ? describeCall(callId, events) : null;

  const hangUp = async () => {
    if (!callId) return;
    try {
      await endCall(sessionId, callId);
    } catch (e) {
      setCallError(apiErrorMessage(e));
    }
  };

  return { verify, runVerify, starting, view, callError, startCall, hangUp };
};
