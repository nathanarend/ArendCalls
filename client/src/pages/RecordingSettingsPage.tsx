import { useEffect, useState } from "react";
import { Loader2, Disc3 } from "lucide-react";
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

const emptyForm: RecForm = {
  b2Endpoint: "",
  b2Region: "",
  b2Bucket: "",
  b2KeyId: "",
  b2Prefix: "",
  webhookUrl: "",
  urlTtlSeconds: 0,
  recordInbound: false,
};

const toForm = (c: RecordingConfig): RecForm => ({
  b2Endpoint: c.b2Endpoint,
  b2Region: c.b2Region,
  b2Bucket: c.b2Bucket,
  b2KeyId: c.b2KeyId,
  b2Prefix: c.b2Prefix,
  webhookUrl: c.webhookUrl,
  urlTtlSeconds: c.urlTtlSeconds,
  recordInbound: c.recordInbound,
});

export const RecordingSettingsPage = () => {
  const [cfg, setCfg] = useState<RecordingConfig | null>(null);
  const [form, setForm] = useState<RecForm>(emptyForm);
  const [b2AppKey, setB2AppKey] = useState("");
  const [webhookSecret, setWebhookSecret] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    getRecordingConfig()
      .then((c) => {
        setCfg(c);
        setForm(toForm(c));
      })
      .catch(() => toast.error("Erro ao carregar a configuração de gravação."));
  }, []);

  const set = <K extends keyof RecForm>(key: K, value: RecForm[K]) =>
    setForm((f) => ({ ...f, [key]: value }));

  const save = async () => {
    setSaving(true);
    try {
      const patch: RecordingConfigPatch = { ...form };
      if (b2AppKey.trim() !== "") patch.b2AppKey = b2AppKey.trim();
      if (webhookSecret.trim() !== "") patch.webhookSecret = webhookSecret.trim();
      const updated = await updateRecordingConfig(patch);
      setCfg(updated);
      setForm(toForm(updated));
      setB2AppKey("");
      setWebhookSecret("");
      toast.success(updated.complete ? "Gravação configurada!" : "Configuração salva.");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "";
      toast.error(
        msg.includes(" 400 ")
          ? "B2 e webhook são obrigatórios juntos — preencha os dois (endpoint, bucket, key id, app key, URL e segredo) ou deixe tudo vazio."
          : "Erro ao salvar a configuração de gravação.",
      );
    } finally {
      setSaving(false);
    }
  };

  const finalKeyHint =
    (form.b2Prefix ? form.b2Prefix.replace(/^\/+|\/+$/g, "") + "/" : "") +
    "recordings/{clinicId}/AAAA/MM/{callId}.wav";

  return (
    <div className="flex h-full flex-col p-8 bg-background overflow-y-auto">
      <div className="max-w-2xl mx-auto w-full space-y-6">
        <div className="flex items-center gap-3">
          <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/15 text-primary">
            <Disc3 className="h-5 w-5" />
          </span>
          <div>
            <h1 className="text-2xl font-bold tracking-tight">Configurações de Gravação</h1>
            <p className="text-sm text-muted-foreground">
              Destino único da instância. Vale para todas as contas — os apps que fazem chamadas
              não passam nada disso.
            </p>
          </div>
          {cfg && (
            <span
              className={
                "ml-auto text-xs font-medium rounded-full px-2.5 py-1 " +
                (cfg.complete
                  ? "bg-primary/15 text-primary"
                  : "bg-amber-500/15 text-amber-600 dark:text-amber-400")
              }
            >
              {cfg.complete ? "configurada" : "incompleta"}
            </span>
          )}
        </div>

        <p className="text-xs text-muted-foreground">
          O ArendCalls grava a chamada em WAV estéreo (atendente à esquerda, cliente à direita),
          envia para o bucket Backblaze B2 abaixo e avisa o app consumidor por webhook assinado
          (HMAC-SHA256). <strong className="text-foreground">B2 e webhook são obrigatórios juntos</strong> —
          preencha os dois ou deixe tudo vazio. {cfg ? null : "Carregando…"}
        </p>

        <div className="rounded-lg border bg-card p-5 space-y-4">
          <p className="text-sm font-semibold">Bucket Backblaze B2</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1 sm:col-span-2">
              <Label className="text-xs text-muted-foreground">Endpoint S3</Label>
              <Input value={form.b2Endpoint} onChange={(e) => set("b2Endpoint", e.target.value)} placeholder="https://s3.us-east-005.backblazeb2.com" className="font-mono text-sm" />
            </div>
            <div className="flex flex-col gap-1">
              <Label className="text-xs text-muted-foreground">Região (opcional)</Label>
              <Input value={form.b2Region} onChange={(e) => set("b2Region", e.target.value)} placeholder="us-east-005" className="font-mono text-sm" />
            </div>
            <div className="flex flex-col gap-1">
              <Label className="text-xs text-muted-foreground">Bucket</Label>
              <Input value={form.b2Bucket} onChange={(e) => set("b2Bucket", e.target.value)} placeholder="gravacoes" className="font-mono text-sm" />
            </div>
            <div className="flex flex-col gap-1">
              <Label className="text-xs text-muted-foreground">Key ID</Label>
              <Input value={form.b2KeyId} onChange={(e) => set("b2KeyId", e.target.value)} placeholder="0045abc..." className="font-mono text-sm" />
            </div>
            <div className="flex flex-col gap-1">
              <Label className="text-xs text-muted-foreground">
                Application Key {cfg?.b2AppKeySet ? "(definida — em branco mantém)" : ""}
              </Label>
              <Input type="password" value={b2AppKey} onChange={(e) => setB2AppKey(e.target.value)} placeholder={cfg?.b2AppKeySet ? "••••••••••" : "K004..."} className="font-mono text-sm" />
            </div>
            <div className="flex flex-col gap-1 sm:col-span-2">
              <Label className="text-xs text-muted-foreground">Prefixo/pasta no bucket (opcional)</Label>
              <Input value={form.b2Prefix} onChange={(e) => set("b2Prefix", e.target.value)} placeholder="arendcalls" className="font-mono text-sm" />
              <span className="text-xs text-muted-foreground">
                Chave final: <code>{finalKeyHint}</code>
              </span>
            </div>
          </div>
          <p className="text-xs text-amber-600 dark:text-amber-400">
            A app key precisa de permissão de <strong>leitura e escrita</strong> no bucket (o
            ArendCalls faz PUT e depois HEAD para conferir).
          </p>
        </div>

        <div className="rounded-lg border bg-card p-5 space-y-4">
          <p className="text-sm font-semibold">Webhook "gravação pronta"</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1 sm:col-span-2">
              <Label className="text-xs text-muted-foreground">URL do receptor</Label>
              <Input value={form.webhookUrl} onChange={(e) => set("webhookUrl", e.target.value)} placeholder="https://seu-app.com/api/arendcalls/recording-ready" className="font-mono text-sm" />
            </div>
            <div className="flex flex-col gap-1">
              <Label className="text-xs text-muted-foreground">
                Segredo HMAC {cfg?.webhookSecretSet ? "(definido — em branco mantém)" : ""}
              </Label>
              <Input type="password" value={webhookSecret} onChange={(e) => setWebhookSecret(e.target.value)} placeholder={cfg?.webhookSecretSet ? "••••••••••" : "segredo compartilhado"} className="font-mono text-sm" />
            </div>
            <div className="flex flex-col gap-1">
              <Label className="text-xs text-muted-foreground">Validade da URL assinada (s)</Label>
              <Input type="number" min={0} value={form.urlTtlSeconds} onChange={(e) => set("urlTtlSeconds", Math.max(0, Number(e.target.value) || 0))} className="font-mono text-sm" />
              <span className="text-xs text-muted-foreground">0 = enviar só a chave; o app assina a URL.</span>
            </div>
          </div>
        </div>

        <div className="rounded-lg border bg-card p-5 space-y-3">
          <p className="text-sm font-semibold">Chamadas recebidas</p>
          <label className="flex items-start gap-2.5 text-sm">
            <input
              type="checkbox"
              className="mt-0.5 h-4 w-4 rounded border-border accent-primary"
              checked={form.recordInbound}
              onChange={(e) => set("recordInbound", e.target.checked)}
            />
            <span>
              Gravar toda chamada recebida atendida
              <span className="block text-xs text-muted-foreground">
                Sem isso, uma chamada recebida só grava se o app passar <code>record: true</code> no{" "}
                <code>POST .../accept</code>. Chamadas de saída são sempre por chamada
                (<code>record</code> no <code>POST /calls</code>).
              </span>
            </span>
          </label>
        </div>

        <Button onClick={save} disabled={saving || !cfg}>
          {saving ? <Loader2 className="w-4 h-4 mr-2 animate-spin" /> : null}
          Salvar configuração
        </Button>
      </div>
    </div>
  );
};
