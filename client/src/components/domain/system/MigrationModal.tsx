import { useState } from "react";
import { AlertTriangle, CheckCircle2, Circle, Loader2, XCircle } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { cn } from "@/lib/utils";
import { isSourceBlocked, isTargetActive, openMigrationModal, refreshMigration, useMigration } from "@/stores/migration";
import {
  finalizeMigrationSource,
  migrationErrorText,
  startMigrationImport,
  unlockMigrationSource,
} from "@/services/migration";
import type { MigrationStatus } from "@/types/migration";

const STEPS = ["Bloquear a origem", "Baixar e conferir", "Importar", "Conectar as contas", "Confirmar na origem"];

// Índice do passo atual do destino; STEPS.length = tudo concluído.
const currentStep = (s: MigrationStatus): number => {
  switch (s.phase) {
    case "preparing":
      if (s.progress.step.startsWith("Bloqueando")) return 0;
      if (s.progress.step.startsWith("Importando")) return 2;
      return s.manifest ? 1 : 0;
    case "imported":
      return 2;
    case "connecting":
      return 3;
    case "committing":
      return 4;
    case "done":
      return STEPS.length;
    default:
      return -1;
  }
};

const formatBytes = (n: number): string => {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
};

const formatTime = (ms: number): string => new Date(ms).toLocaleString("pt-BR");

export const MigrationModal = () => {
  const open = useMigration((s) => s.modalOpen);
  const status = useMigration((s) => s.status);

  return (
    <Dialog open={open} onOpenChange={(o) => openMigrationModal(o)}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Migrar de outra instância</DialogTitle>
          <DialogDescription>
            Clona o banco inteiro de outro servidor ArendCalls para este: contas (com as chaves do WhatsApp, sem
            escanear QR), webhooks, configurações e gravações pendentes.
          </DialogDescription>
        </DialogHeader>
        {status && isSourceBlocked(status) ? (
          <SourceView status={status} />
        ) : status && isTargetActive(status) ? (
          <TargetProgress status={status} />
        ) : (
          <>
            {status?.role === "target" && status.phase && <TargetProgress status={status} />}
            <ImportForm />
          </>
        )}
      </DialogContent>
    </Dialog>
  );
};

const ImportForm = () => {
  const [url, setUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [allowInsecure, setAllowInsecure] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const isHttp = url.trim().toLowerCase().startsWith("http://");

  const start = async () => {
    setBusy(true);
    try {
      await startMigrationImport(url.trim(), apiKey.trim(), allowInsecure);
      await refreshMigration();
    } catch (e) {
      toast.error(migrationErrorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4 border-t border-border pt-4">
      <div className="space-y-1.5">
        <Label htmlFor="mig-url">URL da instância de origem</Label>
        <Input id="mig-url" placeholder="https://calls.servidor-antigo.com" value={url} onChange={(e) => setUrl(e.target.value)} />
      </div>
      <div className="space-y-1.5">
        <Label htmlFor="mig-key">API_KEY da origem</Label>
        <Input id="mig-key" type="password" autoComplete="off" value={apiKey} onChange={(e) => setApiKey(e.target.value)} />
        <p className="text-xs text-muted-foreground">A variável API_KEY do servidor de origem. Fica guardada aqui só até a migração terminar.</p>
      </div>
      {isHttp && (
        <label className="flex items-start gap-2.5 text-sm">
          <input
            type="checkbox"
            className="mt-0.5 h-4 w-4 accent-primary"
            checked={allowInsecure}
            onChange={(e) => setAllowInsecure(e.target.checked)}
          />
          <span>
            Permitir http
            <span className="block text-xs text-muted-foreground">
              As chaves das contas trafegam em texto puro. Só em rede privada/VPN.
            </span>
          </span>
        </label>
      )}
      <div className="space-y-1 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-xs">
        <p className="flex items-center gap-1.5 font-medium text-amber-700 dark:text-amber-400">
          <AlertTriangle className="h-3.5 w-3.5" /> Antes de começar
        </p>
        <ul className="list-disc space-y-0.5 pl-4 text-muted-foreground">
          <li>O clone <strong>substitui tudo</strong> nesta instância. Ela não pode ter contas pareadas.</li>
          <li>A origem desconecta as contas (sem logout) durante a migração. Nenhuma chamada pode estar em andamento lá.</li>
          <li>Se der certo, a origem apaga as próprias chaves e fica desativada. Se falhar, ela volta a operar sozinha.</li>
          <li>Use a mesma versão do ArendCalls nos dois lados (ou esta mais nova).</li>
          <li>As variáveis de ambiente não migram: API_KEY, ADMIN_PASSWORD_HASH e RECORDING_CONFIG_KEY são as deste servidor.</li>
        </ul>
      </div>
      <Button className="w-full" disabled={busy || !url.trim() || !apiKey.trim() || (isHttp && !allowInsecure)} onClick={() => setConfirm(true)}>
        {busy && <Loader2 className="h-4 w-4 animate-spin" />}
        Iniciar migração
      </Button>
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="Iniciar migração?"
        description="A origem vai desconectar todas as contas agora, e o banco desta instância será substituído."
        confirmLabel="Migrar"
        destructive
        onConfirm={() => void start()}
      />
    </div>
  );
};

const TargetProgress = ({ status }: { status: MigrationStatus }) => {
  const step = currentStep(status);
  const failed = status.phase === "aborted" || status.phase === "failed";
  const rollingBack = status.phase === "rolling_back";
  const { bytesDone, bytesTotal } = status.progress;
  const pct = bytesTotal > 0 ? Math.min(100, Math.round((bytesDone / bytesTotal) * 100)) : 0;

  return (
    <div className="space-y-4">
      <div className="text-xs text-muted-foreground">
        Origem: <span className="font-mono">{status.peerUrl}</span>
        {status.startedAt > 0 && <> · início {formatTime(status.startedAt)}</>}
      </div>

      {!failed && !rollingBack && (
        <ol className="space-y-1.5 text-sm">
          {STEPS.map((label, i) => (
            <li key={label} className="flex items-center gap-2">
              {i < step ? (
                <CheckCircle2 className="h-4 w-4 text-emerald-500" />
              ) : i === step ? (
                <Loader2 className="h-4 w-4 animate-spin text-primary" />
              ) : (
                <Circle className="h-4 w-4 text-muted-foreground/50" />
              )}
              <span className={cn(i > step && "text-muted-foreground")}>{label}</span>
            </li>
          ))}
        </ol>
      )}

      {bytesTotal > 0 && status.running && (
        <div className="space-y-1">
          <div className="h-1.5 overflow-hidden rounded bg-muted">
            <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
          </div>
          <p className="text-xs text-muted-foreground">
            {status.progress.step} — {formatBytes(bytesDone)} de {formatBytes(bytesTotal)}
          </p>
        </div>
      )}

      {status.phase === "done" && (
        <p className="flex items-start gap-2 rounded-md bg-emerald-500/10 p-3 text-sm text-emerald-700 dark:text-emerald-400">
          <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
          Migração concluída. {status.report?.connectedOnB ?? 0} de {status.report?.pairedSessions ?? 0} conta(s) conectaram aqui; a
          origem apagou as chaves dela.
        </p>
      )}
      {(failed || rollingBack || (status.error && status.phase === "committing")) && (
        <p
          className={cn(
            "flex items-start gap-2 rounded-md p-3 text-sm",
            failed || rollingBack ? "bg-destructive/10 text-destructive" : "bg-amber-500/10 text-amber-700 dark:text-amber-400",
          )}
        >
          <XCircle className="mt-0.5 h-4 w-4 shrink-0" />
          {status.error}
        </p>
      )}

      <MigrationLog status={status} />
    </div>
  );
};

const MigrationLog = ({ status }: { status: MigrationStatus }) => {
  const log = status.log ?? [];
  if (log.length === 0) return null;
  return (
    <details className="text-xs" open={status.phase === "failed"}>
      <summary className="cursor-pointer text-muted-foreground">Registro ({log.length})</summary>
      <ul className="mt-2 max-h-48 space-y-1 overflow-y-auto font-mono">
        {log.map((l, i) => (
          <li
            key={i}
            className={cn(
              l.level === "error" && "text-destructive",
              l.level === "warn" && "text-amber-600 dark:text-amber-400",
            )}
          >
            <span className="text-muted-foreground">{new Date(l.at).toLocaleTimeString("pt-BR")}</span> {l.msg}
          </li>
        ))}
      </ul>
    </details>
  );
};

const SourceView = ({ status }: { status: MigrationStatus }) => {
  const [confirm, setConfirm] = useState<"unlock" | "finalize" | null>(null);
  const [busy, setBusy] = useState(false);

  const run = async (fn: () => Promise<unknown>, ok: string) => {
    setBusy(true);
    try {
      await fn();
      toast.success(ok);
      await refreshMigration();
    } catch (e) {
      toast.error(migrationErrorText(e));
    } finally {
      setBusy(false);
    }
  };

  if (status.phase === "committed") {
    return (
      <div className="space-y-3">
        <p className="rounded-md bg-muted p-3 text-sm">
          Esta instância foi migrada para <span className="font-mono">{status.peerUrl}</span> em {formatTime(status.updatedAt)}. As
          chaves do WhatsApp foram apagadas daqui e ela está desativada — use o novo servidor.
        </p>
        <MigrationLog status={status} />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <p className="rounded-md bg-amber-500/10 p-3 text-sm text-amber-800 dark:text-amber-300">
        Esta instância é a <strong>origem</strong> de uma migração para <span className="font-mono">{status.peerUrl}</span>. As contas
        estão desconectadas (sem logout) e nada conecta até o destino confirmar ou desistir.
      </p>
      <p className="text-xs text-muted-foreground">
        Normalmente nada precisa ser feito aqui: o destino confirma ou desbloqueia sozinho. Use os botões só se o destino não
        conseguir falar com esta instância.
      </p>
      <div className="flex flex-col gap-2 sm:flex-row">
        <Button variant="outline" className="flex-1" disabled={busy} onClick={() => setConfirm("unlock")}>
          Desbloquear (cancelar)
        </Button>
        <Button variant="destructive" className="flex-1" disabled={busy || !status.manifest} onClick={() => setConfirm("finalize")}>
          Finalizar manualmente
        </Button>
      </div>
      <MigrationLog status={status} />
      <ConfirmDialog
        open={confirm === "unlock"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title="Desbloquear a origem?"
        description="Só faça isso se o destino NÃO estiver usando as contas (falhou antes de importar ou já desfez o clone). Se o destino estiver conectado, as duas instâncias vão disputar as mesmas sessões."
        confirmLabel="Desbloquear"
        destructive
        onConfirm={() => void run(() => unlockMigrationSource(status.id), "Origem desbloqueada; reconectando as contas")}
      />
      <ConfirmDialog
        open={confirm === "finalize"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title="Finalizar manualmente?"
        description="Só se o destino concluiu e está operando, mas não conseguiu confirmar para cá. Apaga as chaves do WhatsApp desta instância (sem logout). Não tem volta."
        confirmLabel="Apagar chaves e finalizar"
        destructive
        onConfirm={() =>
          void run(() => finalizeMigrationSource(status.id, status.manifest!.dbSha256), "Migração finalizada nesta origem")
        }
      />
    </div>
  );
};
