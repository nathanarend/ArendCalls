import { AlertTriangle, ArrowLeftRight, Loader2, Lock } from "lucide-react";
import { cn } from "@/lib/utils";
import { isSourceBlocked, isTargetActive, openMigrationModal, useMigration } from "@/stores/migration";

// Faixa sob o cabeçalho enquanto uma migração bloqueia ou exige atenção.
export const MigrationBanner = () => {
  const status = useMigration((s) => s.status);
  if (!status) return null;

  let tone = "";
  let icon = null;
  let text = "";
  if (isSourceBlocked(status)) {
    const committed = status.phase === "committed";
    tone = committed ? "bg-muted text-foreground" : "bg-amber-500/15 text-amber-800 dark:text-amber-300";
    icon = <Lock className="h-4 w-4 shrink-0" />;
    text = committed
      ? `Instância migrada para ${status.peerUrl} e desativada.`
      : `Bloqueada para migração → ${status.peerUrl}. Contas desconectadas até o destino concluir.`;
  } else if (isTargetActive(status)) {
    tone = "bg-primary/10 text-primary";
    icon = <Loader2 className="h-4 w-4 shrink-0 animate-spin" />;
    text = `Migração em andamento${status.progress.step ? `: ${status.progress.step}` : ""}`;
  } else if (status.role === "target" && status.phase === "failed") {
    tone = "bg-destructive/10 text-destructive";
    icon = <AlertTriangle className="h-4 w-4 shrink-0" />;
    text = "A migração precisa de atenção.";
  } else {
    return null;
  }

  return (
    <button
      onClick={() => openMigrationModal()}
      className={cn("flex w-full items-center gap-2 px-4 py-2 text-left text-sm sm:px-6", tone)}
    >
      {icon}
      <span className="flex-1">{text}</span>
      <ArrowLeftRight className="h-4 w-4 shrink-0 opacity-60" />
    </button>
  );
};
