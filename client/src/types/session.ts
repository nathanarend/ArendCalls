export type SessionState = "connecting" | "qr" | "open" | "stopped" | "logged_out";

export type SessionInfo = {
  id: string;
  name: string;
  jid: string;
  state: SessionState;
  paired: boolean;
  webhookUrl?: string;
};
