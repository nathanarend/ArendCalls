import { apiGet, apiPost, apiPatch, apiDelete } from "@/lib/api";
import { getClientId } from "@/lib/client-id";
import type { SessionInfo, RecordingConfig, RecordingConfigPatch, RecordingsOverview } from "@/types/session";

export const listSessions = () =>
  apiGet<{ sessions: SessionInfo[] }>("/api/sessions").then((r) => r.sessions ?? []);

export const createSession = (name: string) =>
  apiPost<{ id: string }>("/api/sessions", { name });

export const updateSession = (id: string, name: string) =>
  apiPatch<{ status: string }>(`/api/sessions/${id}`, { name });

export const updateWebhookUrl = (id: string, webhook_url: string) =>
  apiPatch<{ status: string }>(`/api/sessions/${id}/webhook`, { webhook_url });

/** Override por conta: força mostrar as chamadas recebidas desta conta no painel
 *  mesmo com o switch global desligado (bypass p/ teste). Nunca esconde. */
export const setPanelInbound = (id: string, enabled: boolean) =>
  apiPatch<{ status: string; panelInbound: boolean }>(`/api/sessions/${id}/panel-inbound`, { enabled });

/** Switch global: o painel mostra chamadas recebidas ou não. */
export const getPanelSettings = () =>
  apiGet<{ inboundCalls: boolean }>(`/api/panel-settings`);

export const setPanelInboundCalls = (inboundCalls: boolean) =>
  apiPatch<{ status: string; inboundCalls: boolean }>(`/api/panel-settings`, { inboundCalls });

export const getRecordingConfig = (id: string) =>
  apiGet<RecordingConfig>(`/api/sessions/${id}/recording-config`);

export const updateRecordingConfig = (id: string, patch: RecordingConfigPatch) =>
  apiPatch<RecordingConfig>(`/api/sessions/${id}/recording-config`, patch);

export const listRecordings = (id: string) =>
  apiGet<RecordingsOverview>(`/api/sessions/${id}/recordings`);

export const deleteSession = (id: string) => apiDelete(`/api/sessions/${id}`);

const postVoid = async (path: string): Promise<void> => {
  const r = await fetch(path, {
    method: "POST",
    headers: { "X-Client-Id": getClientId(), "Content-Type": "application/json" },
    body: "{}",
  });
  if (!r.ok) throw new Error(`${path} ${r.status}`);
};

export const logoutSession = (id: string) => postVoid(`/api/sessions/${id}/logout`);

export const pairSession = (id: string) => postVoid(`/api/sessions/${id}/pair`);

export const startSession = (id: string) => postVoid(`/api/sessions/${id}/start`);

export const stopSession = (id: string) => postVoid(`/api/sessions/${id}/stop`);

export const restartSession = (id: string) => postVoid(`/api/sessions/${id}/restart`);
