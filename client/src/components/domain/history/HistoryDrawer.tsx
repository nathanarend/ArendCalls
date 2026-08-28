import { useState } from "react";
import { History } from "lucide-react";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { EmptyState } from "@/components/shared/EmptyState";
import { useHistory } from "@/hooks/useHistory";

export const HistoryDrawer = ({ sid }: { sid: string }) => {
  const [open, setOpen] = useState(false);
  const { data: rows = [] } = useHistory(sid, open);

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button 
          variant="outline" 
          size="sm"
          className="rounded-full px-3.5 text-xs font-semibold gap-1.5 shadow-xs border-border/80 bg-background/90 hover:bg-muted/80 hover:border-border transition-all duration-150 active:scale-95"
        >
          <History className="h-3.5 w-3.5 text-muted-foreground" />
          Histórico
        </Button>
      </SheetTrigger>
      <SheetContent side="right" className="w-full p-0 sm:max-w-md">
        <SheetHeader className="p-6 pb-4">
          <SheetTitle>Histórico de chamadas</SheetTitle>
        </SheetHeader>
        <Separator />
        <ScrollArea className="h-[calc(100vh-5.5rem)] px-6 py-4">
          {rows.length === 0 ? (
            <EmptyState title="Nenhuma chamada anterior" description="As chamadas que você fizer ou receber aparecerão aqui." />
          ) : (
            <ul className="space-y-2">
              {rows.map((r) => (
                <li key={r.callId} className="rounded-lg border p-3">
                  <p className="font-medium">{r.peerName || r.peer}</p>
                  <p className="text-xs text-muted-foreground">
                    {r.direction === "outbound" ? "Efetuada" : "Recebida"} · {new Date(r.startedAt).toLocaleString()}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </ScrollArea>
      </SheetContent>
    </Sheet>
  );
};
