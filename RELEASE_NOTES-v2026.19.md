# Release v2026.19 — Resiliência de Sinalização VoIP (Retry EndCall/RejectCall), Fix Atendimento no Celular e Proteção Multi-Operador

## 📋 Resumo da Release
Esta versão traz correções essenciais de robustez na sinalização VoIP, encerramento de chamadas e sincronização de múltiplos operadores conectados ao mesmo painel/CRM:

1. **Retentativa e Propagação Real de Erros no Encerramento (`EndCall` e `RejectCall`)**: Elimina o sintoma de *"Reconectando-se..."* quando a chamada era desligada durante reconexões temporárias do socket WhatsApp.
2. **Correção de Chamada Atendida no Celular que Continuava Tocando no Painel (90s)**: Tratamento de eventos `CallAccept` sem `call-id` explícito via fallback de JID/LID e tolerância a chamadas em ordem não-linear na máquina de estados.
3. **Exclusividade Automática contra Atendimento Duplo**: O servidor gera internamente um identificador único para cada requisição de `/accept`, garantindo que a primeira requisição tranca a chamada e qualquer tentativa concorrente simultânea receba `409 Conflict` (*"claimed by another client"*), sem exigir que a aplicação envie cabeçalhos ou parâmetros extras.

---

## 🚀 Principais Mudanças

### 🛠️ Backend Go (`internal/voip/call` & `cmd/server`)
- **`internal/voip/call/callmanager.go`**:
  - `EndCall` e `RejectCall` agora executam até 2 tentativas automáticas com 500ms de intervalo para envio dos stanzas `<terminate>` e `<reject>` via `SendNode`, retornando o erro real em caso de falha em vez de `nil` silencioso.
  - A limpeza de mídia local (`cleanupMedia()`) é garantida em todos os caminhos.
- **`cmd/server/httpapi.go`**:
  - `doEndCall` e `doReject` agora retornam status HTTP semânticos:
    - `404 Not Found` se a chamada não for encontrada no servidor.
    - `502 Bad Gateway` se o stanza falhar no envio ao WhatsApp (evitando falso positivo no CRM).
    - `204 No Content` / `200 OK` apenas quando a sinalização for bem-sucedida.
  - `doAccept` gera internamente uma chave de exclusividade única a cada atendimento, protegendo a chamada contra atendimento concorrente de forma 100% transparente.
- **`cmd/server/session.go`**:
  - No evento `*events.CallAccept`, implementado fallback de busca por JID do peer (resolvendo `@lid` para `@s.whatsapp.net`), garantindo que o aceite seja processado mesmo quando o pacote do WhatsApp vier sem `call-id` explícito.
- **`internal/voip/call/callmanager_signaling.go`**:
  - `HandleCallAccept` não descarta mais eventos quando `ExtractNodeInfo` for nulo, garantindo a evolução do estado para chamadas de protocolo simplificado.
- **`internal/voip/call/callstate.go`**:
  - `TransitionRemoteAccepted` agora é idempotente para o estado `Connecting` e aceita transições diretas de `IncomingRinging`.

---

## 🧪 Testes e Validação
- Suíte completa de testes automatizados executada e validada (`go test ./...`).
- Build de produção do Frontend validado com sucesso (`npm run build`).

---

## 🐳 Docker Images (DockerHub)
- `nathanarend/arendcalls:v2026.19`
- `nathanarend/arendcalls:latest`

### Como Atualizar em Produção:
```bash
docker pull nathanarend/arendcalls:latest
docker restart arendcalls_server
```

---

## 📝 Link Direto para a Release no GitHub
Acesse [https://github.com/nathanarend/ArendCalls/releases/new?tag=v2026.19](https://github.com/nathanarend/ArendCalls/releases/new?tag=v2026.19) para publicar a release com as notas acima.
