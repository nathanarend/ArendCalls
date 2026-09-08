export interface RouteInfo {
  method: string;
  path: string;
  purpose: string;
  payload?: any;
  response?: any;
  requiresSid?: boolean;
}

export const adminRoutes: RouteInfo[] = [
  { method: "GET", path: "/api/sessions", purpose: "Listar todas as contas (id, nome, jid, status, pareamento)" },
  { method: "POST", path: "/api/sessions", purpose: "Criar uma conta e iniciar o pareamento QR", payload: { name: "Minha Nova Conta" } },
  { method: "GET", path: "/api/system/metrics", purpose: "Obter telemetria em tempo real do ArendCalls e recursos da VPS (RAM, CPU, Uptime, Disco)" },
  { method: "GET", path: "/api/panel-settings", purpose: "Ler o switch global 'o painel mostra chamadas recebidas'", response: { inboundCalls: true } },
  { method: "PATCH", path: "/api/panel-settings", purpose: "Ligar/desligar globalmente a exibição de chamadas recebidas no painel. Uma conta com override ligado ainda aparece (OU lógico).", payload: { inboundCalls: false }, response: { status: "ok", inboundCalls: false } },
  { method: "GET", path: "/api/events", purpose: "Eventos Server-Sent globais e disparo de Webhooks em paralelo" },
];

export const sessionRoutes: RouteInfo[] = [
  { method: "PATCH", path: "/api/sessions/{sid}", purpose: "Renomear uma conta existente", payload: { name: "Novo Nome da Conta" }, requiresSid: true },
  { method: "PATCH", path: "/api/sessions/{sid}/webhook", purpose: "Definir URL de Webhook de eventos (Ringing/Accepted/Terminated) específica desta conta", payload: { webhook_url: "https://seu-crm.com/api/webhook" }, requiresSid: true },
  { method: "PATCH", path: "/api/sessions/{sid}/panel-inbound", purpose: "Override por conta: força mostrar as chamadas recebidas desta conta no painel mesmo com o switch global desligado (bypass de teste). Nunca esconde — quem esconde é o switch global (/api/panel-settings). OU lógico: global OU override.", payload: { enabled: true }, response: { status: "ok", panelInbound: true }, requiresSid: true },
  { method: "DELETE", path: "/api/sessions/{sid}", purpose: "Fazer logout e remover uma conta", requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/logout", purpose: "Desconectar uma conta (manter para re-parear)", requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/pair", purpose: "Re-parear uma conta (emitir novo QR)", requiresSid: true },
  {
    method: "POST",
    path: "/api/sessions/{sid}/calls",
    purpose:
      "Iniciar uma chamada de saída. record: true liga a gravação no servidor desta chamada (exige a config de gravação da conta completa — ver seção 3). clinicId define a pasta {clinicId} na chave do bucket.",
    payload: { phone: "5511999999999", duration_ms: 30000, record: true, clinicId: "clinica-123" },
    response: { call: { callId: "1A2B3C..." } },
    requiresSid: true,
  },
  { method: "POST", path: "/api/sessions/{sid}/calls/{id}/webrtc", purpose: "Trocar o SDP WebRTC do navegador", payload: { sdp_offer: "v=0\no=- 123456..." }, requiresSid: true },
  {
    method: "POST",
    path: "/api/sessions/{sid}/calls/{id}/accept",
    purpose:
      "Aceitar uma chamada de entrada. record: true grava esta chamada recebida (clinicId define a pasta no bucket). Sem record, grava só se 'gravar chamadas recebidas' estiver ligado na config de gravação da conta.",
    payload: { record: true, clinicId: "clinica-123" },
    response: { call: { callId: "1A2B3C..." } },
    requiresSid: true,
  },
  { method: "POST", path: "/api/sessions/{sid}/calls/{id}/reject", purpose: "Rejeitar uma chamada de entrada", requiresSid: true },
  { method: "DELETE", path: "/api/sessions/{sid}/calls/{id}", purpose: "Encerrar uma chamada ativa", requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/check-number", purpose: "Verifica se números de telefone (internacional) possuem WhatsApp", payload: { numbers: ["+5511999999999"] }, requiresSid: true },
  { method: "GET", path: "/api/sessions/{sid}/history", purpose: "Histórico recente de chamadas (até 50 registros)", requiresSid: true },
  { method: "GET", path: "/api/sessions/{sid}/events", purpose: "Eventos Server-Sent (SSE) exclusivos desta conta", requiresSid: true },
];

export const recordingRoutes: RouteInfo[] = [
  {
    method: "PATCH",
    path: "/api/sessions/{sid}/recording-config",
    purpose:
      "Destino da gravação DESTA conta (individual por conta; configurado no painel do ArendCalls, os apps não passam nada disso). B2 e webhook são obrigatórios juntos: envie os 6 campos (b2Endpoint, b2Bucket, b2KeyId, b2AppKey, webhookUrl, webhookSecret) ou tudo vazio para limpar. Setup pela metade retorna 400 com a lista missing. PATCH parcial preserva o que não for enviado; um segredo enviado como \"\" é apagado. recordInbound: grava toda chamada recebida atendida desta conta.",
    payload: {
      b2Endpoint: "https://s3.us-east-005.backblazeb2.com",
      b2Region: "us-east-005",
      b2Bucket: "gravacoes-clinica",
      b2KeyId: "0045abc...",
      b2AppKey: "K004...",
      b2Prefix: "clinica-x",
      webhookUrl: "https://api.seusistema.com/webhooks/gravacao-pronta",
      webhookSecret: "segredo-hmac-compartilhado",
      urlTtlSeconds: 0,
      recordInbound: false,
    },
    response: { sessionId: "SUA_SESSION_ID", complete: true, b2Bucket: "gravacoes-clinica", b2AppKeySet: true, webhookSecretSet: true, urlTtlSeconds: 0, recordInbound: false },
    requiresSid: true,
  },
  {
    method: "GET",
    path: "/api/sessions/{sid}/recording-config",
    purpose: "Ler a configuração de gravação da conta. Segredos nunca são retornados — apenas b2AppKeySet / webhookSecretSet. complete indica se o destino está pronto para entregar gravações.",
    response: { sessionId: "SUA_SESSION_ID", complete: false, b2Endpoint: "", b2Region: "", b2Bucket: "", b2KeyId: "", b2AppKeySet: false, b2Prefix: "", webhookUrl: "", webhookSecretSet: false, urlTtlSeconds: 0, recordInbound: false },
    requiresSid: true,
  },
  {
    method: "GET",
    path: "/api/sessions/{sid}/recordings",
    purpose:
      "Monitoramento: as gravações recentes da conta (até 50), contagem por status, uso de disco local em bytes e se o destino está configurado. Status possíveis: recording, uploading, ready, failed, skipped (< 5s).",
    response: {
      configured: true,
      missing: [],
      stats: { uploading: 1, ready: 12, failed: 0, skipped: 2 },
      diskUsageBytes: 3932160,
      recordings: [
        {
          callId: "1A2B3C...",
          status: "ready",
          direction: "outbound",
          peer: "5511999999999@s.whatsapp.net",
          durationMs: 143000,
          b2Key: "clinica-x/recordings/clinica-123/2026/09/1A2B3C....wav",
          notified: true,
          uploadAttempts: 1,
          notifyAttempts: 1,
          error: "",
          startedAt: 1757000000000,
          endedAt: 1757000143000,
        },
      ],
    },
    requiresSid: true,
  },
  {
    method: "GET",
    path: "/api/sessions/{sid}/calls/{id}/recording-info",
    purpose: "Metadados da gravação de UMA chamada — para o app consumidor reconciliar quando o webhook 'gravação pronta' se perdeu. 404 se a chamada não foi gravada.",
    response: {
      callId: "1A2B3C...",
      sessionId: "SUA_SESSION_ID",
      clinicId: "clinica-123",
      status: "ready",
      recordingKey: "clinica-x/recordings/clinica-123/2026/09/1A2B3C....wav",
      recordingUrl: "https://s3.us-west-004.backblazeb2.com/...&X-Amz-Signature=...",
      durationSeconds: 143,
      channels: "stereo",
      mimeType: "audio/wav",
      startedAt: "2026-09-06T21:00:00Z",
      endedAt: "2026-09-06T21:02:23Z",
      error: "",
    },
    requiresSid: true,
  },
];
