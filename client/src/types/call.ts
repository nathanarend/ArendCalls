export type CallStatus = "starting" | "ringing" | "connected" | "ended";

export type CallSummary = {
  sessionId: string;
  callId: string;
  owner: string | null;
  direction: "outbound" | "inbound";
  peer: string;
  peerName?: string;
  startedAt: number;
  status: CallStatus;
};

export type IncomingPayload = { sessionId: string; callId: string; peer: string; peerName?: string; offeredAt: number };
