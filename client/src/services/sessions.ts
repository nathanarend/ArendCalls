import { apiGet, apiPost, apiPatch, apiDelete } from "@/lib/api";
import { getClientId } from "@/lib/client-id";
import type { SessionInfo } from "@/types/session";

export const listSessions = () =>
  apiGet<{ sessions: SessionInfo[] }>("/api/sessions").then((r) => r.sessions ?? []);

export const createSession = (name: string) =>
  apiPost<{ id: string }>("/api/sessions", { name });

export const updateSession = (id: string, name: string) =>
  apiPatch<{ status: string }>(`/api/sessions/${id}`, { name });

export const updateWebhookUrl = (id: string, webhook_url: string) =>
  apiPatch<{ status: string }>(`/api/sessions/${id}/webhook`, { webhook_url });

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
