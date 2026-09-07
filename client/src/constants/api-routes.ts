export interface RouteInfo {
  method: string;
  path: string;
  purpose: string;
  payload?: any;
  requiresSid?: boolean;
}

export const adminRoutes: RouteInfo[] = [
  { method: "GET", path: "/api/sessions", purpose: "Listar todas as contas (id, nome, jid, status, pareamento)" },
  { method: "POST", path: "/api/sessions", purpose: "Criar uma conta e iniciar o pareamento QR", payload: { name: "Minha Nova Conta" } },
  { method: "GET", path: "/api/system/metrics", purpose: "Obter telemetria em tempo real do ArendCalls e recursos da VPS (RAM, CPU, Uptime, Disco)" },
  { method: "GET", path: "/api/events", purpose: "Eventos Server-Sent globais e disparo de Webhooks em paralelo" },
];

export const sessionRoutes: RouteInfo[] = [
  { method: "PATCH", path: "/api/sessions/{sid}", purpose: "Renomear uma conta existente", payload: { name: "Novo Nome da Conta" }, requiresSid: true },
  { method: "PATCH", path: "/api/sessions/{sid}/webhook", purpose: "Definir URL de Webhook específica desta conta", payload: { webhook_url: "https://seu-crm.com/api/webhook" }, requiresSid: true },
  { method: "DELETE", path: "/api/sessions/{sid}", purpose: "Fazer logout e remover uma conta", requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/logout", purpose: "Desconectar uma conta (manter para re-parear)", requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/pair", purpose: "Re-parear uma conta (emitir novo QR)", requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/calls", purpose: "Iniciar uma chamada de saída (record ativa a gravação no servidor; clinicId define a pasta no bucket)", payload: { phone: "5511999999999", duration_ms: 30000, record: true, clinicId: "clinica-123" }, requiresSid: true },
  { method: "GET", path: "/api/sessions/{sid}/recording-config", purpose: "Ler a configuração de gravação da conta (segredos redigidos)", requiresSid: true },
  { method: "PATCH", path: "/api/sessions/{sid}/recording-config", purpose: "Destino da gravação: bucket B2 + webhook 'gravação pronta' (PATCH parcial). Ativar/desativar é por chamada, via record no POST /calls", payload: { b2Endpoint: "https://s3.us-west-004.backblazeb2.com", b2Bucket: "gravacoes", b2KeyId: "0045abc", b2AppKey: "K004...", webhookUrl: "https://mocho.example/api/calls/recording-ready", webhookSecret: "segredo", urlTtlSeconds: 900 }, requiresSid: true },
  { method: "GET", path: "/api/sessions/{sid}/calls/{id}/recording-info", purpose: "Status/metadados da gravação de uma chamada (para reconciliação pelo Mocho)", requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/calls/{id}/webrtc", purpose: "Trocar o SDP WebRTC do navegador", payload: { sdp_offer: "v=0\no=- 123456..." }, requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/calls/{id}/accept", purpose: "Aceitar uma chamada de entrada", requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/calls/{id}/reject", purpose: "Rejeitar uma chamada de entrada", requiresSid: true },
  { method: "DELETE", path: "/api/sessions/{sid}/calls/{id}", purpose: "Encerrar uma chamada ativa", requiresSid: true },
  { method: "POST", path: "/api/sessions/{sid}/check-number", purpose: "Verifica se números de telefone (internacional) possuem WhatsApp", payload: { numbers: ["+5511999999999"] }, requiresSid: true },
  { method: "GET", path: "/api/sessions/{sid}/history", purpose: "Histórico recente de chamadas (até 50 registros)", requiresSid: true },
  { method: "GET", path: "/api/sessions/{sid}/events", purpose: "Eventos Server-Sent (SSE) exclusivos desta conta", requiresSid: true },
];
