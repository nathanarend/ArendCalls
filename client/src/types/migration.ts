// Espelha GET /api/migration/status (cmd/server/migration.go).
export type MigrationRole = "" | "source" | "target";

export type MigrationPhase =
  | ""
  // origem
  | "locked"
  | "committed"
  | "unlocked"
  // destino
  | "preparing"
  | "imported"
  | "connecting"
  | "committing"
  | "rolling_back"
  | "done"
  | "aborted"
  | "failed";

export type MigrationLogLine = { at: number; level: "info" | "warn" | "error"; msg: string };

export type MigrationStatus = {
  role: MigrationRole;
  id: string;
  phase: MigrationPhase;
  peerUrl: string;
  error: string;
  log: MigrationLogLine[] | null;
  startedAt: number;
  updatedAt: number;
  running: boolean;
  progress: { step: string; bytesDone: number; bytesTotal: number };
  report: {
    tables: Record<string, number>;
    files: number;
    pairedSessions: number;
    connectedOnB: number;
    notConnected?: string[];
    committedAt?: number;
    sourceCommitted: boolean;
  } | null;
  manifest?: {
    dbSize: number;
    dbSha256: string;
    tables: Record<string, number>;
    sessions: { id: string; name: string; jid: string }[];
    files: number;
    warnings?: string[];
  };
};
