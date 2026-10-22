import { apiGet, apiPost } from "@/lib/api";
import type { MigrationStatus } from "@/types/migration";

export const getMigrationStatus = (): Promise<MigrationStatus> => apiGet<MigrationStatus>("/api/migration/status");

export const startMigrationImport = (sourceUrl: string, sourceApiKey: string, allowInsecure: boolean) =>
  apiPost<{ migrationId: string }>("/api/migration/import", { sourceUrl, sourceApiKey, allowInsecure });

export const unlockMigrationSource = (id: string) =>
  apiPost<{ status: string }>(`/api/migration/source/${id}/abort`, { reason: "desbloqueio manual pelo painel" });

export const finalizeMigrationSource = (id: string, dbSha256: string) =>
  apiPost<{ status: string }>(`/api/migration/source/${id}/commit`, { dbSha256 });

/** checkStatus lança "path status corpo": devolve só o campo error do JSON. */
export const migrationErrorText = (e: unknown): string => {
  const msg = (e as Error)?.message ?? String(e);
  const i = msg.indexOf("{");
  if (i >= 0) {
    try {
      const body = JSON.parse(msg.slice(i)) as { error?: string };
      if (body.error) return body.error;
    } catch {
      // corpo não era JSON
    }
  }
  return msg;
};
