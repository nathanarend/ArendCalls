import { useState, useEffect } from "react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { updateSession } from "@/services/sessions";
import type { SessionInfo } from "@/types/session";

export const EditSessionModal = ({ 
  session, 
  onClose 
}: { 
  session: SessionInfo | null; 
  onClose: () => void;
}) => {
  const [editName, setEditName] = useState("");

  useEffect(() => {
    if (session) {
      setEditName(session.name);
    }
  }, [session]);

  const handleEdit = async () => {
    if (!session || !editName.trim()) return;
    try {
      await updateSession(session.id, editName.trim());
      onClose();
    } catch (e) {
      toast.error((e as Error).message);
    }
  };

  return (
    <Dialog open={!!session} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Editar Conta</DialogTitle>
          <DialogDescription>
            Altere o nome da conta selecionada.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Label htmlFor="edit-name">Novo nome</Label>
            <Input
              id="edit-name"
              value={editName}
              onChange={(e) => setEditName(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleEdit()}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancelar</Button>
          <Button onClick={handleEdit} disabled={!editName.trim()}>
            Salvar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
