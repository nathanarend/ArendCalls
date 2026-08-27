import { useState } from "react";
import { ChevronDown, ChevronUp, Copy } from "lucide-react";
import { toast } from "sonner";
import { adminRoutes, sessionRoutes, RouteInfo } from "@/constants/api-routes";

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
            <pre className="bg-background p-2 rounded border">{JSON.stringify(route.payload, null, 2)}</pre>
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
