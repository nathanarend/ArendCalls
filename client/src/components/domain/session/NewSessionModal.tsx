import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { createSession } from "@/services/sessions";
import { setActiveSession } from "@/stores/sessions";

export const NewSessionModal = ({ 
  open, 
  onOpenChange, 
  onNavigate 
}: { 
  open: boolean; 
  onOpenChange: (open: boolean) => void; 
  onNavigate?: () => void; 
}) => {
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");

  const handleNew = async () => {
    if (!newName.trim()) return;
    setCreating(true);
    try {
      const { id } = await createSession(newName.trim());
      setActiveSession(id);
      onOpenChange(false);
      setNewName("");
      onNavigate?.();
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => {
      onOpenChange(o);
      if (!o) setNewName("");
    }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Nova Conta</DialogTitle>
          <DialogDescription>
            Dê um nome para identificar esta conta de WhatsApp.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Label htmlFor="name">Nome da conta</Label>
            <Input
              id="name"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              placeholder="Ex: Pessoal, Trabalho..."
              onKeyDown={(e) => e.key === "Enter" && handleNew()}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>Cancelar</Button>
          <Button onClick={handleNew} disabled={creating || !newName.trim()}>
            {creating ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : null}
            Criar Conta
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
