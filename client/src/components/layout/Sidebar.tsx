import { useState } from "react";
import { Plus, Trash2, Pencil, BookOpen, Settings } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";
import { setActiveSession, useSessions } from "@/stores/sessions";
import { deleteSession } from "@/services/sessions";
import type { SessionInfo, SessionState } from "@/types/session";

import { NewSessionModal } from "@/components/domain/session/NewSessionModal";
import { EditSessionModal } from "@/components/domain/session/EditSessionModal";
import { SessionSettingsModal } from "@/components/domain/session/SessionSettingsModal";

const dotClass: Record<SessionState, string> = {
  open: "bg-emerald-500",
  qr: "bg-amber-500",
  stopped: "bg-zinc-400 dark:bg-zinc-500",
  connecting: "bg-blue-500",
  logged_out: "bg-rose-500",
};

export const Sidebar = ({ onNavigate }: { onNavigate?: () => void }) => {
  const sessions = useSessions((s) => s.sessions);
  const activeId = useSessions((s) => s.activeId);
  const [toDelete, setToDelete] = useState<SessionInfo | null>(null);
  
  // Dialogs state
  const [showNewDialog, setShowNewDialog] = useState(false);
  const [editingSession, setEditingSession] = useState<SessionInfo | null>(null);
  const [settingsSession, setSettingsSession] = useState<SessionInfo | null>(null);

  const remove = async (id: string) => {
    try {
      await deleteSession(id);
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  const updateLocalSettingsSession = (updatedSession: SessionInfo) => {
    setSettingsSession(updatedSession);
  };

  return (
    <div className="flex h-full flex-col gap-2 p-3">
      <div className="flex items-center justify-between px-2 pt-1">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Contas</p>
        <div className="flex items-center gap-2">
          <button 
            onClick={() => {
              setActiveSession("api-docs"); // special ID for docs
              onNavigate?.();
            }}
            className="text-muted-foreground hover:text-foreground transition-colors"
            title="Documentação da API"
          >
            <BookOpen className="h-4 w-4" />
          </button>
        </div>
      </div>
      <div className="flex-1 space-y-1 overflow-y-auto">
        {sessions.map((s) => (
          <div
            key={s.id}
            role="button"
            tabIndex={0}
            onClick={() => {
              setActiveSession(s.id);
              onNavigate?.();
            }}
            className={cn(
              "group flex cursor-pointer items-center gap-2 rounded-md px-2 py-2 text-sm",
              s.id === activeId ? "bg-accent text-accent-foreground" : "hover:bg-muted",
            )}
          >
            <span className={cn("h-2 w-2 shrink-0 rounded-full", dotClass[s.state])} />
            <div className="min-w-0 flex-1">
              <p className="truncate font-medium">{s.name}</p>
              {s.jid && <p className="truncate text-xs text-muted-foreground">{s.jid.split("@")[0]}</p>}
            </div>
            
            <div className="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setSettingsSession(s);
                }}
                className="text-muted-foreground transition-colors hover:text-amber-500 p-1"
                aria-label={`Configurações da conta ${s.name}`}
              >
                <Settings className="h-4 w-4" />
              </button>
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setEditingSession(s);
                }}
                className="text-muted-foreground transition-colors hover:text-primary p-1"
                aria-label={`Editar ${s.name}`}
              >
                <Pencil className="h-4 w-4" />
              </button>
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setToDelete(s);
                }}
                className="text-muted-foreground transition-colors hover:text-destructive p-1"
                aria-label={`Excluir ${s.name}`}
              >
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
          </div>
        ))}
        {sessions.length === 0 && <p className="px-2 text-sm text-muted-foreground">Nenhuma conta ainda.</p>}
      </div>
      <Button variant="outline" className="w-full" onClick={() => setShowNewDialog(true)}>
        <Plus className="h-4 w-4" />
        Nova conta
      </Button>

      <ConfirmDialog
        open={!!toDelete}
        onOpenChange={(o) => !o && setToDelete(null)}
        title="Excluir conta?"
        description={toDelete ? `${toDelete.name} será desconectada e removida.` : undefined}
        confirmLabel="Excluir"
        destructive
        onConfirm={() => {
          if (toDelete) void remove(toDelete.id);
        }}
      />

      <NewSessionModal 
        open={showNewDialog} 
        onOpenChange={setShowNewDialog} 
        onNavigate={onNavigate} 
      />

      <EditSessionModal 
        session={editingSession} 
        onClose={() => setEditingSession(null)} 
      />

      <SessionSettingsModal 
        session={settingsSession} 
        onClose={() => setSettingsSession(null)} 
        onUpdateSession={updateLocalSettingsSession} 
      />
    </div>
  );
};
