export type HistoryRow = {
  callId: string;
  peer: string;
  peerName?: string;
  direction: string;
  startedAt: number;
  endedAt: number | null;
  endReason: string | null;
};
