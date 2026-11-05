import { useState } from "react";
import { ChevronDown, ChevronUp, Copy } from "lucide-react";
import { toast } from "sonner";
import { adminRoutes, sessionRoutes, agentRoutes, recordingRoutes, RouteInfo } from "@/constants/api-routes";

export const ApiDocs = () => {
  const [expandedIndex, setExpandedIndex] = useState<string | null>(null);

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text);
    toast.success("Copiado para a área de transferência!");
  };

  const renderExamples = (route: RouteInfo) => {
    const baseUrl = window.location.origin;
    const pathWithKeyPlaceholder = route.requiresSid
      ? route.path.replace("{sid}", "SUA_SESSION_ID")
      : route.path;

    const curlHeader = `curl -X ${route.method} "${baseUrl}${pathWithKeyPlaceholder}" \\\n  -H "Authorization: Bearer SUA_CHAVE_API"${route.payload ? ` \\\n  -H "Content-Type: application/json" \\\n  -d '${JSON.stringify(route.payload, null, 2)}'` : ""}`;

    const curlUrl = `curl -X ${route.method} "${baseUrl}${pathWithKeyPlaceholder}?apikey=SUA_CHAVE_API"${route.payload ? ` \\\n  -H "Content-Type: application/json" \\\n  -d '${JSON.stringify(route.payload, null, 2)}'` : ""}`;

    const fetchCode = `fetch("${baseUrl}${pathWithKeyPlaceholder}", {
  method: "${route.method}",
  headers: {
    "Authorization": "Bearer SUA_CHAVE_API",
    "Content-Type": "application/json"
  }${route.payload ? `,\n  body: JSON.stringify(${JSON.stringify(route.payload, null, 2).replace(/\n/g, "\n  ")})` : ""}
})
.then(res => res.json())
.then(console.log);`;

    return (
      <div className="bg-muted/40 p-4 border-t border-border space-y-4 text-xs font-mono">
        {route.payload && (
          <div>
            <span className="text-muted-foreground font-semibold uppercase block mb-1">Payload (JSON):</span>
            <pre className="bg-background p-2 rounded border overflow-x-auto whitespace-pre">{JSON.stringify(route.payload, null, 2)}</pre>
          </div>
        )}
        {route.response && (
          <div>
            <span className="text-muted-foreground font-semibold uppercase block mb-1">Resposta (exemplo):</span>
            <pre className="bg-background p-2 rounded border overflow-x-auto whitespace-pre">{JSON.stringify(route.response, null, 2)}</pre>
          </div>
        )}
        <div>
          <div className="flex items-center justify-between mb-1">
            <span className="text-muted-foreground font-semibold uppercase">Exemplo cURL (Recomendado - Header):</span>
            <button onClick={() => handleCopy(curlHeader)} className="p-1 hover:bg-muted rounded text-muted-foreground transition-colors">
              <Copy className="w-3.5 h-3.5" />
            </button>
          </div>
          <pre className="bg-background p-2 rounded border overflow-x-auto whitespace-pre">{curlHeader}</pre>
        </div>
        <div>
          <div className="flex items-center justify-between mb-1">
            <span className="text-muted-foreground font-semibold uppercase">Exemplo cURL (Alternativo - Na URL):</span>
            <button onClick={() => handleCopy(curlUrl)} className="p-1 hover:bg-muted rounded text-muted-foreground transition-colors">
              <Copy className="w-3.5 h-3.5" />
            </button>
          </div>
          <pre className="bg-background p-2 rounded border overflow-x-auto whitespace-pre">{curlUrl}</pre>
        </div>
        <div>
          <div className="flex items-center justify-between mb-1">
            <span className="text-muted-foreground font-semibold uppercase">Exemplo Javascript (Fetch):</span>
            <button onClick={() => handleCopy(fetchCode)} className="p-1 hover:bg-muted rounded text-muted-foreground transition-colors">
              <Copy className="w-3.5 h-3.5" />
            </button>
          </div>
          <pre className="bg-background p-2 rounded border overflow-x-auto whitespace-pre">{fetchCode}</pre>
        </div>
      </div>
    );
  };

  const renderTable = (routesList: RouteInfo[], sectionKey: string) => (
    <div className="rounded-md border bg-card overflow-hidden">
      <table className="w-full text-sm text-left">
        <thead className="bg-muted text-muted-foreground">
          <tr>
            <th className="px-4 py-3 font-medium w-[100px]">Método</th>
            <th className="px-4 py-3 font-medium">Rota</th>
            <th className="px-4 py-3 font-medium">Propósito</th>
            <th className="px-4 py-3 font-medium w-[40px]"></th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {routesList.map((route, i) => {
            const key = `${sectionKey}-${i}`;
            const isExpanded = expandedIndex === key;
            return (
              <>
                <tr 
                  key={`row-${key}`} 
                  onClick={() => setExpandedIndex(isExpanded ? null : key)}
                  className="hover:bg-muted/50 cursor-pointer transition-colors"
                >
                  <td className="px-4 py-4 font-medium">
                    <span className={`px-2 py-1 rounded text-[10px] font-semibold tracking-wide uppercase ${
                      route.method === 'GET' ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300' :
                      route.method === 'POST' ? 'bg-green-100 text-green-700 dark:bg-green-900/50 dark:text-green-300' :
                      route.method === 'PATCH' ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300' :
                      'bg-red-100 text-red-700 dark:bg-red-900/50 dark:text-red-300'
                    }`}>
                      {route.method}
                    </span>
                  </td>
                  <td className="px-4 py-4 font-mono text-xs text-foreground font-semibold">{route.path}</td>
                  <td className="px-4 py-4 text-muted-foreground text-sm">{route.purpose}</td>
                  <td className="px-4 py-4 text-muted-foreground">
                    {isExpanded ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
                  </td>
                </tr>
                {isExpanded && (
                  <tr key={`details-${key}`}>
                    <td colSpan={4} className="p-0">
                      {renderExamples(route)}
                    </td>
                  </tr>
                )}
              </>
            );
          })}
        </tbody>
      </table>
    </div>
  );

  return (
    <div className="flex h-full flex-col p-8 bg-background overflow-y-auto">
      <div className="max-w-4xl mx-auto w-full space-y-8">
        <div className="bg-amber-500/10 border border-amber-500/20 rounded-lg p-4 space-y-2">
          <h3 className="font-semibold text-amber-500 text-sm flex items-center gap-2">
            💡 Onde eu acho o ID da minha conta (session_id / sid) ?
          </h3>
          <p className="text-xs text-muted-foreground leading-relaxed">
            É super simples! Passe o mouse em cima do nome do seu WhatsApp na barra lateral esquerda e clique na **Engrenagem** que vai aparecer. Lá dentro, no topo da janela, vai ter um campo chamado <strong>ID da Conta</strong> com um botão <strong>Copiar ID</strong>. Aquele código comprido é o seu <code>{'{sid}'}</code> para colocar no cURL/integrações!
          </p>
        </div>

        <div>
          <h1 className="text-3xl font-bold tracking-tight">Documentação da API</h1>
          <p className="text-muted-foreground mt-2">
            Clique em qualquer rota para expandir exemplos práticos de consumo.
          </p>
        </div>

        <div className="space-y-4">
          <h2 className="text-xl font-bold text-foreground">1. Rotas Administrativas (Acesso Global)</h2>
          <p className="text-sm text-muted-foreground">
            Essas rotas afetam todo o servidor. Elas só aceitam a **Chave Geral de Administrador** (ou o acesso nativo do Painel Web).
          </p>
          {renderTable(adminRoutes, "admin")}
        </div>

        <div className="space-y-4">
          <h2 className="text-xl font-bold text-foreground">2. Rotas Individuais (Acesso Específico de Conta)</h2>
          <p className="text-sm text-muted-foreground">
            Essas rotas agem dentro do escopo de um WhatsApp específico e exigem a identificação da conta através do <code>{'{sid}'}</code>.
          </p>
          {renderTable(sessionRoutes, "session")}
        </div>

        <div className="space-y-4">
          <h2 className="text-xl font-bold text-foreground">3. Gravação de Chamadas (Servidor)</h2>
          <p className="text-sm text-muted-foreground">
            O ArendCalls grava a chamada em <strong>WAV estéreo</strong> (atendente à esquerda, cliente à direita),
            sobe para o <strong>Backblaze B2</strong> e avisa o seu sistema por webhook assinado.
          </p>
          <ul className="list-disc pl-5 text-sm text-muted-foreground space-y-1.5">
            <li>O destino (B2 + webhook) é <strong>individual por conta</strong>, configurado no painel do ArendCalls (engrenagem da conta → "Gravação de Chamadas"). <strong>Os apps não passam credenciais.</strong></li>
            <li><strong>Saída:</strong> grava por chamada — <code>record: true</code> no <code>POST /api/sessions/{'{sid}'}/calls</code>.</li>
            <li><strong>Entrada:</strong> <code>record: true</code> no <code>POST /api/sessions/{'{sid}'}/calls/{'{id}'}/accept</code>, ou a opção "gravar chamadas recebidas" na config da conta (grava todas).</li>
            <li>Só grava <strong>depois que a chamada é atendida</strong>. Gravações com menos de <strong>5 segundos</strong> são descartadas, e chamadas que não foram atendidas não geram gravação (status <code>skipped</code>, motivo em <code>error</code>).</li>
            <li>O envio passa por uma <strong>fila persistente</strong> com um pool de workers (padrão 3). Uma rajada de chamadas encerrando não vira uma rajada de uploads. Falhas são reenviadas com backoff (1min → 5min → 15min → 1h) e sobrevivem a reinício do servidor.</li>
            <li>O arquivo local só é apagado <strong>depois</strong> do B2 confirmar (existência + tamanho). Sem o destino configurado, o WAV fica no disco aguardando — nada é perdido.</li>
          </ul>
          {renderTable(recordingRoutes, "recording")}
        </div>

        <div className="mt-8 space-y-4 pt-6 border-t">
          <h2 className="text-xl font-semibold">Webhook "Gravação Pronta" (recebido pelo seu sistema)</h2>
          <p className="text-sm text-muted-foreground">
            Quando a gravação é confirmada no B2, o ArendCalls faz um <code>POST</code> na <code>webhookUrl</code> configurada
            (rota da seção 3) com o corpo abaixo. O corpo é assinado: o cabeçalho <code>X-ArendCalls-Signature: sha256=&lt;hmac&gt;</code> é
            o HMAC-SHA256 do corpo <em>bruto</em> usando o <code>webhookSecret</code>. Responda <code>2xx</code> para confirmar o recebimento;
            qualquer outra resposta faz o ArendCalls reenviar no backoff.
          </p>
          <div className="bg-muted/60 p-4 rounded-xl border border-border space-y-2 font-mono text-xs">
            <span className="text-muted-foreground font-semibold uppercase block">Corpo do POST:</span>
            <pre className="text-foreground overflow-x-auto whitespace-pre">{JSON.stringify({
              callId: "1A2B3C...",
              sessionId: "SUA_SESSION_ID",
              clinicId: "clinica-123",
              recordingKey: "clinica-x/recordings/clinica-123/2026/09/1A2B3C....wav",
              recordingUrl: "https://s3.us-west-004.backblazeb2.com/...&X-Amz-Signature=...",
              durationSeconds: 143,
              channels: "stereo",
              mimeType: "audio/wav",
              startedAt: "2026-09-06T21:00:00Z",
              endedAt: "2026-09-06T21:02:23Z",
            }, null, 2)}</pre>
          </div>
          <ul className="list-disc pl-5 text-sm text-muted-foreground space-y-2">
            <li><code>recordingUrl</code> só vem preenchida se <code>urlTtlSeconds &gt; 0</code>. Caso contrário, use a <code>recordingKey</code> e assine a URL do seu lado.</li>
            <li>O ArendCalls pode reenviar o mesmo evento (ex.: reinício entre o POST e a confirmação interna). <strong>Deduplique por <code>callId</code>.</strong></li>
            <li>Se o webhook se perdeu, consulte <code>GET /api/sessions/{'{sid}'}/calls/{'{id}'}/recording-info</code> para reconciliar.</li>
          </ul>
        </div>

        <div className="mt-8 space-y-4 pt-6 border-t">
          <h2 className="text-xl font-bold text-foreground">4. Agente de Voz (ElevenLabs)</h2>
          <p className="text-sm text-muted-foreground">
            Com o campo <code>agent</code> no <code>POST /api/sessions/{'{sid}'}/calls</code>, o ArendCalls disca e, quando o contato atende,
            um agente da ElevenLabs conversa com ele no lugar do atendente. Sem o campo, a chamada segue igual, com atendente no painel.
          </p>
          <ul className="list-disc pl-5 text-sm text-muted-foreground space-y-1.5">
            <li><strong>API key:</strong> por conta, no painel (engrenagem da conta → aba Agente) ou pela rota <code>PATCH .../agent-config</code>. Nunca é devolvida.</li>
            <li><strong>Agente:</strong> escolhido por chamada, pelo <code>agentId</code> (trecho <code>agent_...</code> da URL do agente na ElevenLabs). Nada fica salvo no ArendCalls.</li>
            <li><strong>Formato:</strong> o agente precisa estar em <strong>PCM 16 kHz</strong> na entrada e na saída; em outro formato, a conversa falha logo após o atendimento.</li>
            <li><strong>Antes de discar:</strong> o ArendCalls valida a key e o agente. Erro aqui volta na resposta do <code>POST</code> e ninguém é chamado. Toque sem resposta não gera conversa nem gasta créditos.</li>
            <li><strong>Durante:</strong> fala do contato por cima corta a fala do agente. Quando o agente encerra, a ligação termina depois da fala em andamento. Conectar o áudio pelo painel encerra o agente e mantém a ligação.</li>
            <li><strong>Gravação:</strong> com <code>record: true</code>, o agente fica no canal do atendente.</li>
            <li><strong>Uma chamada por número:</strong> com chamada de saída ativa para o mesmo número na conta, o <code>POST</code> responde <code>409</code> com o <code>callId</code> existente e não disca.</li>
            <li>Não envie <code>X-Client-Id</code> de um backend: ele identifica um atendente do painel, e um atendente só pode estar em uma chamada (também <code>409</code>).</li>
            <li>Limites: só chamadas de saída têm agente; em espera (<code>/hold</code>) o contato não ouve o agente.</li>
          </ul>
          <div className="bg-muted/60 p-4 rounded-xl border border-border space-y-2 font-mono text-xs">
            <span className="text-muted-foreground font-semibold uppercase block">Corpo do POST /calls com agente:</span>
            <pre className="text-foreground overflow-x-auto whitespace-pre">{JSON.stringify({
              phone: "5511999999999",
              agent: { agentId: "agent_xxx", dynamicVariables: { nome: "Maria" } },
            }, null, 2)}</pre>
          </div>
          <p className="text-sm text-muted-foreground">
            <code>dynamicVariables</code> é opcional e preenche <code>{'{{nome}}'}</code> no prompt do agente.
          </p>
          <div className="rounded-md border bg-card overflow-hidden">
            <table className="w-full text-sm text-left">
              <thead className="bg-muted text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium w-[80px]">Status</th>
                  <th className="px-4 py-2 font-medium">Quando</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border text-muted-foreground">
                {[
                  ["400", "Falta phone ou agent.agentId, key não configurada, ou ligação para o próprio número"],
                  ["401", "API key do ArendCalls errada"],
                  ["404", "Conta inexistente ou número sem WhatsApp"],
                  ["409", "Já há chamada de saída ativa para o número nesta conta"],
                  ["429", "Limite de chamadas simultâneas por conta (-max-calls no servidor)"],
                  ["500", "Falha ao verificar o número ou ao discar"],
                  ["502", "ElevenLabs recusou a key ou o agente, ou está indisponível; não discou"],
                  ["503", "Conta sem pareamento"],
                ].map(([code, text]) => (
                  <tr key={code}>
                    <td className="px-4 py-2 font-mono text-xs text-foreground">{code}</td>
                    <td className="px-4 py-2">{text}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="text-sm text-muted-foreground">
            Acompanhe pelo SSE ou pelo webhook da conta. O evento <code>call-agent</code> informa a conversa com o agente:
          </p>
          <div className="bg-muted/60 p-4 rounded-xl border border-border space-y-2 font-mono text-xs">
            <span className="text-muted-foreground font-semibold uppercase block">Evento call-agent:</span>
            <pre className="text-foreground overflow-x-auto whitespace-pre">{JSON.stringify({
              type: "call-agent",
              sessionId: "SUA_SESSION_ID",
              id: "1A2B3C...",
              agentId: "agent_xxx",
              status: "connected",
              conversationId: "conv_xxx",
              error: "",
            }, null, 2)}</pre>
          </div>
          <ul className="list-disc pl-5 text-sm text-muted-foreground space-y-1.5">
            <li><code>status</code>: <code>connected</code> (contato atendeu e a conversa abriu) ou <code>failed</code> (motivo em <code>error</code>).</li>
            <li><code>id</code> é o <code>callId</code> devolvido no <code>POST</code>. O fim da ligação chega em <code>call-ended</code>, com <code>reason</code>.</li>
            <li><code>conversationId</code> liga a chamada à conversa na ElevenLabs, que entrega transcrição e análise pelo post-call webhook dela.</li>
            <li>Para testar sem ligar, use <code>POST .../agent-test</code> (ou "Verificar conexão" na aba Agente). O teste vira uma conversa curta no histórico da ElevenLabs e gasta poucos créditos.</li>
          </ul>
          {renderTable(agentRoutes, "agent")}
        </div>

        <div className="mt-8 space-y-4 pt-6 border-t">
          <h2 className="text-xl font-semibold">Webhooks Ativos (Alternativa ao SSE)</h2>
          <p className="text-sm text-muted-foreground">
            Caso você não possa manter uma conexão contínua de SSE no seu backend, o ArendCalls pode "empurrar" os eventos ativamente para você.
            Para isso, você tem duas opções:
          </p>
          <ul className="list-disc pl-5 text-sm text-muted-foreground space-y-2">
            <li><strong>Interface Visual:</strong> Clique na <strong>Engrenagem</strong> ao lado do nome da conta na barra lateral e cole sua URL no campo de Webhook.</li>
            <li><strong>Via API REST:</strong> Use a rota <code>PATCH /api/sessions/{'{sid}'}/webhook</code> descrita acima se preferir configurar a URL programaticamente pelo seu sistema.</li>
          </ul>
          <p className="text-sm text-muted-foreground">
            O webhook recebe os eventos da conta, no mesmo formato do SSE: <code>call-status</code> (<code>starting</code>, <code>ringing</code>,
            <code> connected</code>), <code>call-agent</code> e <code>call-ended</code> (<code>reason</code>: <code>user_ended</code>, <code>declined</code>,
            <code> timeout</code>, <code>busy</code>, <code>cancelled</code>, <code>failed</code>, <code>do_not_disturb</code>, <code>unknown</code>), entre outros.
            O envio não é assinado nem reenviado em caso de falha: use um token secreto na URL e reconcilie por <code>GET /api/sessions/{'{sid}'}/history</code>.
          </p>
        </div>

        <div className="mt-8 space-y-4 pt-6 border-t">
          <h2 className="text-xl font-semibold">Transporte de Áudio: WebRTC ou WebSocket</h2>
          <p className="text-sm text-muted-foreground">
            O áudio entre navegador e servidor é sempre o mesmo PCM <strong>Int16 LE, 16 kHz, mono</strong>, e o cliente escolhe o
            transporte ao conectar. Os dois ficam disponíveis o tempo todo; nada no resto da API muda.
          </p>
          <ul className="list-disc pl-5 text-sm text-muted-foreground space-y-2">
            <li><strong>WebRTC</strong> (<code>POST .../webrtc</code>): DataChannel <code>pcm</code>. Exige UDP direto até o servidor (portas 50000-50100). Use com IP público.</li>
            <li>
              <strong>WebSocket</strong> (<code>GET .../audio</code>): uma mensagem binária por quadro, nos dois sentidos. Funciona atrás de proxy ou
              Cloudflare Tunnel. Como é TCP, perda de pacote vira atraso pontual em vez de picote.
            </li>
            <li>
              De outra origem (seu CRM), o navegador não pode mandar <code>Authorization</code> no WebSocket: o seu backend pede
              <code> POST .../audio-ticket</code> com a API key e o navegador conecta em <code>wss://.../audio?ticket=...</code>. O ticket vale
              30 s, uma vez, só para aquela chamada. Com Basic Auth no proxy, deixe a rota <code>/api/sessions/*/calls/*/audio</code> fora dele.
            </li>
            <li>
              Fechamento pelo servidor: <code>1000 call_ended</code> (a chamada terminou) ou <code>1000 replaced</code> (outra conexão de áudio
              assumiu, ex.: retomada após espera). Outra queda é anormal: o servidor espera <strong>15 s</strong> por uma reconexão (com ticket
              novo) antes de encerrar a chamada; em espera, não encerra. Ping a cada 20 s.
            </li>
            <li>No painel, a escolha fica em <strong>Configurações do Painel → Transporte de áudio</strong> (por navegador; o padrão Automático tenta WebRTC e cai para WebSocket).</li>
          </ul>
        </div>

        <div className="mt-8 space-y-4 pt-6 border-t">
          <h2 className="text-xl font-semibold">Segurança e Autenticação</h2>
          <p className="text-sm text-muted-foreground">
            A API utiliza autenticação por Chave de Super-Usuário (Global). Quando o servidor está atrás de um proxy reverso (como Traefik) com proteção de <strong>Basic Auth</strong>, é necessário enviar as credenciais do Traefik no cabeçalho <code>Authorization: Basic ...</code> ou flag <code>-u usuario:senha</code> e passar a chave do ArendCalls via <code>?apikey=...</code> ou cabeçalho <code>X-Api-Key: ...</code>.
          </p>
          <div className="bg-muted/60 p-4 rounded-xl border border-border space-y-2 font-mono text-xs">
            <span className="text-muted-foreground font-semibold uppercase block">Exemplo cURL com Traefik Basic Auth + ArendCalls API Key:</span>
            <pre className="text-foreground overflow-x-auto whitespace-pre">curl -s -u usuario:senha "https://seu-dominio.com/api/sessions?apikey=SUA_API_KEY"\n# Ou com header X-Api-Key:\ncurl -s -u usuario:senha -H "X-Api-Key: SUA_API_KEY" https://seu-dominio.com/api/sessions</pre>
          </div>
          <ul className="list-disc pl-5 text-sm text-muted-foreground space-y-2">
            <li>
              <strong>Chave Geral de Super-Usuário (API_KEY)</strong>: Definida ao iniciar o servidor (via variável de ambiente <code>API_KEY</code>). Permite acesso total e seguro a todas as rotas da API.
            </li>
            <li>
              <strong>Autenticação Traefik Basic Auth</strong>: Usuário e senha definidos nas labels do docker-compose para proteger o endpoint público.
            </li>
            <li>
              <strong>Painel de Controle Web</strong>: Autentica-se automaticamente via cookie de sessão seguro (<code>wacalls_admin_token</code>).
            </li>
          </ul>
        </div>
      </div>
    </div>
  );
};
