import { useEffect, useState } from "react";
import { Disc3, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { getRecordingConfig, updateRecordingConfig } from "@/services/sessions";
import type { RecordingConfig, RecordingConfigPatch } from "@/types/session";

type RecForm = {
  b2Endpoint: string;
  b2Region: string;
  b2Bucket: string;
  b2KeyId: string;
  b2Prefix: string;
  webhookUrl: string;
  urlTtlSeconds: number;
  recordInbound: boolean;
};

const emptyRecForm: RecForm = {
  b2Endpoint: "",
  b2Region: "",
  b2Bucket: "",
  b2KeyId: "",
  b2Prefix: "",
  webhookUrl: "",
  urlTtlSeconds: 0,
  recordInbound: false,
};

const toRecForm = (c: RecordingConfig): RecForm => ({
  b2Endpoint: c.b2Endpoint,
  b2Region: c.b2Region,
  b2Bucket: c.b2Bucket,
  b2KeyId: c.b2KeyId,
  b2Prefix: c.b2Prefix,
  webhookUrl: c.webhookUrl,
  urlTtlSeconds: c.urlTtlSeconds,
  recordInbound: c.recordInbound,
});

export const RecordingConfigSection = ({ sessionId }: { sessionId: string }) => {
  const [rec, setRec] = useState<RecordingConfig | null>(null);
  const [recForm, setRecForm] = useState<RecForm>(emptyRecForm);
  const [b2AppKey, setB2AppKey] = useState("");
  const [recWebhookSecret, setRecWebhookSecret] = useState("");
  const [isSavingRec, setIsSavingRec] = useState(false);

  useEffect(() => {
    getRecordingConfig(sessionId)
      .then((c) => {
        setRec(c);
        setRecForm(toRecForm(c));
      })
      .catch(() => toast.error("Erro ao carregar configuração de gravação."));
  }, [sessionId]);

  const setRecField = <K extends keyof RecForm>(key: K, value: RecForm[K]) =>
    setRecForm((f) => ({ ...f, [key]: value }));

  const handleSaveRecording = async () => {
    setIsSavingRec(true);
    try {
      const patch: RecordingConfigPatch = { ...recForm };
      if (b2AppKey.trim() !== "") patch.b2AppKey = b2AppKey.trim();
      if (recWebhookSecret.trim() !== "") patch.webhookSecret = recWebhookSecret.trim();
      const updated = await updateRecordingConfig(sessionId, patch);
      setRec(updated);
      setRecForm(toRecForm(updated));
      setB2AppKey("");
      setRecWebhookSecret("");
      toast.success(updated.complete ? "Gravação configurada!" : "Configuração de gravação limpa.");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "";
      if (msg.includes(" 400 ")) {
        toast.error("B2 e webhook são obrigatórios juntos — preencha os dois (endpoint, bucket, key id, app key, URL e segredo) ou deixe tudo vazio.");
      } else {
        toast.error("Erro ao salvar configuração de gravação.");
      }
    } finally {
      setIsSavingRec(false);
    }
  };

  return (
    <div className="space-y-4">
      <p className="text-sm font-semibold flex items-center gap-2">
        <Disc3 className="w-4 h-4" />
        Gravação de Chamadas (Servidor)
        {rec && (
          <span
            className={
              "ml-auto text-xs font-medium rounded-full px-2 py-0.5 " +
              (rec.complete
                ? "bg-primary/15 text-primary"
                : "bg-amber-500/15 text-amber-600 dark:text-amber-400")
            }
          >
            {rec.complete ? "configurada" : "incompleta"}
          </span>
        )}
      </p>
      <p className="text-xs text-muted-foreground">
        Quando uma chamada é iniciada com <code>record: true</code> no <code>POST /calls</code>,
        o ArendCalls grava em WAV estéreo (atendente à esquerda, cliente à direita),
        envia para o bucket Backblaze B2 abaixo e avisa o app consumidor por webhook assinado.
        <strong className="text-foreground"> B2 e webhook são obrigatórios juntos</strong> — preencha os
        dois ou deixe tudo vazio.
        {rec ? null : " Carregando..."}
      </p>

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex flex-col gap-1 sm:col-span-2">
          <Label className="text-xs text-muted-foreground">Endpoint S3 do B2</Label>
          <Input
            value={recForm.b2Endpoint}
            onChange={(e) => setRecField("b2Endpoint", e.target.value)}
            placeholder="https://s3.us-west-004.backblazeb2.com"
            className="font-mono text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">Região (opcional)</Label>
          <Input
            value={recForm.b2Region}
            onChange={(e) => setRecField("b2Region", e.target.value)}
            placeholder="us-west-004"
            className="font-mono text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">Bucket</Label>
          <Input
            value={recForm.b2Bucket}
            onChange={(e) => setRecField("b2Bucket", e.target.value)}
            placeholder="gravacoes-clinica"
            className="font-mono text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">Key ID</Label>
          <Input
            value={recForm.b2KeyId}
            onChange={(e) => setRecField("b2KeyId", e.target.value)}
            placeholder="0045abc..."
            className="font-mono text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">
            Application Key {rec?.b2AppKeySet ? "(definida — deixe em branco para manter)" : ""}
          </Label>
          <Input
            type="password"
            value={b2AppKey}
            onChange={(e) => setB2AppKey(e.target.value)}
            placeholder={rec?.b2AppKeySet ? "••••••••••" : "K004..."}
            className="font-mono text-sm"
          />
        </div>
        <div className="flex flex-col gap-1 sm:col-span-2">
          <Label className="text-xs text-muted-foreground">Prefixo/pasta no bucket (opcional)</Label>
          <Input
            value={recForm.b2Prefix}
            onChange={(e) => setRecField("b2Prefix", e.target.value)}
            placeholder="clinica-x"
            className="font-mono text-sm"
          />
          <span className="text-xs text-muted-foreground">
            Chave final: <code>{(recForm.b2Prefix ? recForm.b2Prefix.replace(/^\/+|\/+$/g, "") + "/" : "")}recordings/&#123;clinicId&#125;/AAAA/MM/&#123;callId&#125;.wav</code>
          </span>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 border-t border-dashed pt-3">
        <div className="flex flex-col gap-1 sm:col-span-2">
          <Label className="text-xs text-muted-foreground">Webhook "gravação pronta"</Label>
          <Input
            value={recForm.webhookUrl}
            onChange={(e) => setRecField("webhookUrl", e.target.value)}
            placeholder="https://api.seusistema.com/webhooks/gravacao-pronta"
            className="font-mono text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">
            Segredo HMAC {rec?.webhookSecretSet ? "(definido — em branco mantém)" : ""}
          </Label>
          <Input
            type="password"
            value={recWebhookSecret}
            onChange={(e) => setRecWebhookSecret(e.target.value)}
            placeholder={rec?.webhookSecretSet ? "••••••••••" : "segredo compartilhado"}
            className="font-mono text-sm"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label className="text-xs text-muted-foreground">Validade da URL assinada (segundos)</Label>
          <Input
            type="number"
            min={0}
            value={recForm.urlTtlSeconds}
            onChange={(e) => setRecField("urlTtlSeconds", Math.max(0, Number(e.target.value) || 0))}
            className="font-mono text-sm"
          />
          <span className="text-xs text-muted-foreground">0 = enviar só a chave; o app consumidor assina a URL.</span>
        </div>
      </div>

      <Button
        variant="secondary"
        onClick={handleSaveRecording}
        disabled={isSavingRec || !rec}
      >
        {isSavingRec ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
        Salvar configuração de gravação
      </Button>
    </div>
  );
};
