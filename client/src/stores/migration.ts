import { create } from "zustand";
import { getMigrationStatus } from "@/services/migration";
import type { MigrationStatus } from "@/types/migration";

type State = {
  status: MigrationStatus | null;
  modalOpen: boolean;
};

export const useMigration = create<State>(() => ({ status: null, modalOpen: false }));

export const openMigrationModal = (open = true): void => {
  useMigration.setState({ modalOpen: open });
  if (open) void refreshMigration();
};

const TARGET_ACTIVE = new Set(["preparing", "imported", "connecting", "committing", "rolling_back"]);

export const isTargetActive = (s: MigrationStatus | null): boolean =>
  !!s && s.role === "target" && (s.running || TARGET_ACTIVE.has(s.phase));

export const isSourceBlocked = (s: MigrationStatus | null): boolean =>
  !!s && s.role === "source" && (s.phase === "locked" || s.phase === "committed");

export const refreshMigration = async (): Promise<void> => {
  try {
    useMigration.setState({ status: await getMigrationStatus() });
  } catch {
    // servidor antigo sem a rota, ou fora do ar: sem banner
  }
};

// Polling: rápido com a migração andando ou o modal aberto, lento no resto.
let wired = false;
export const ensureMigrationPolling = (): void => {
  if (wired) return;
  wired = true;
  const tick = async () => {
    await refreshMigration();
    const { status, modalOpen } = useMigration.getState();
    const fast = modalOpen || isTargetActive(status) || status?.phase === "locked";
    setTimeout(tick, fast ? 1500 : 15000);
  };
  void tick();
};
