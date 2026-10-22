import { getClientId } from "./client-id";
import { markUnauthorized } from "@/stores/auth";

const baseHeaders = (): HeadersInit => ({
  "X-Client-Id": getClientId(),
  "Content-Type": "application/json",
});

export const checkStatus = async (r: Response, path: string) => {
  if (r.status === 401) markUnauthorized();
  if (!r.ok) {
    const text = await r.text().catch(() => "");
    throw new Error(`${path} ${r.status} ${text}`);
  }
}

export const apiGet = async <T>(path: string): Promise<T> => {
  const r = await fetch(path, { headers: baseHeaders() });
  await checkStatus(r, path);
  return r.json() as Promise<T>;
};

export const apiPost = async <T>(path: string, body: unknown): Promise<T> => {
  const r = await fetch(path, { method: "POST", headers: baseHeaders(), body: JSON.stringify(body) });
  await checkStatus(r, path);
  return r.json() as Promise<T>;
};

export const apiPatch = async <T>(path: string, body: unknown): Promise<T> => {
  const r = await fetch(path, { method: "PATCH", headers: baseHeaders(), body: JSON.stringify(body) });
  await checkStatus(r, path);
  return r.json() as Promise<T>;
};

export const apiDelete = async (path: string): Promise<void> => {
  const r = await fetch(path, { method: "DELETE", headers: baseHeaders() });
  await checkStatus(r, path);
};
