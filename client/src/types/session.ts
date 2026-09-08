export type SessionState = "connecting" | "qr" | "open" | "stopped" | "logged_out";

export type SessionInfo = {
  id: string;
  name: string;
  jid: string;
  state: SessionState;
  paired: boolean;
  webhookUrl?: string;
};

// RecordingConfig mirrors the redacted payload of GET /api/sessions/{sid}/recording-config.
// Secrets are never returned — b2AppKeySet / webhookSecretSet only say whether one is stored.
// Whether a call is recorded is decided per call by the `record` field on POST /calls,
// not here; this is only the destination (B2 bucket + Mocho webhook).
export type RecordingConfig = {
  complete: boolean;
  b2Endpoint: string;
  b2Region: string;
  b2Bucket: string;
  b2KeyId: string;
  b2AppKeySet: boolean;
  b2Prefix: string;
  webhookUrl: string;
  webhookSecretSet: boolean;
  urlTtlSeconds: number;
  recordInbound: boolean;
};

// Fields accepted by PATCH /api/sessions/{sid}/recording-config. All optional:
// omitted fields keep their stored value; secrets sent as "" are cleared.
export type RecordingConfigPatch = Partial<{
  b2Endpoint: string;
  b2Region: string;
  b2Bucket: string;
  b2KeyId: string;
  b2AppKey: string;
  b2Prefix: string;
  webhookUrl: string;
  webhookSecret: string;
  urlTtlSeconds: number;
  recordInbound: boolean;
}>;

export type RecordingStatus = "recording" | "uploading" | "ready" | "failed" | "skipped";

export type RecordingItem = {
  callId: string;
  status: RecordingStatus;
  direction: string;
  peer: string;
  durationMs: number;
  b2Key: string;
  notified: boolean;
  uploadAttempts: number;
  notifyAttempts: number;
  error: string;
  startedAt: number;
  endedAt: number;
};

export type RecordingsOverview = {
  configured: boolean;
  missing: string[];
  stats: Partial<Record<RecordingStatus, number>>;
  diskUsageBytes: number;
  recordings: RecordingItem[];
};
