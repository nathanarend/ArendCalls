# Release v2026.18 — Fix Chamadas Caindo em ~60s (Multi-Device WhatsApp) e Envio Assíncrono Terminate/Reject

## 📋 Resumo da Release
Esta versão corrige dois problemas críticos no motor VoIP e sinalização do WhatsApp em ambientes de produção com **WhatsApp Multi-Device** e **CRM / WebRTC**:

1. **Correção de Queda de Chamadas em ~60s (Multi-Device WhatsApp)**: Chamadas ativas eram encerradas involuntariamente após 60 segundos porque instâncias espelho (segundo dispositivo pareado) não cancelavam seu timer anti-zombie ao conectar o relay WebRTC.
2. **Correção no Envio do Desligamento (`SendNode` em vez de `Query`)**: Encerramentos (`EndCall`) e recusas (`RejectCall`) travavam por 3s em timeout de ACK síncrono e deixavam o aparelho remoto em estado de *"reconectando-se"* até ser derrubado pela rede.

---

## 🚀 Principais Mudanças

### 🛠️ Backend Go (`wacalls/cmd/server` & `internal/voip/call`)
- **`internal/voip/call/callmanager.go`**:
  - Alterado o envio de stanzas `<terminate>` e `<reject>` de `m.sock.Query(ctx, node)` para `m.sock.SendNode(ctx, node)`. As stanzas de encerramento são notificações assíncronas do WhatsApp que não recebem resposta de ACK com ID da Meta.
  - Adicionado o callback `OnRelayConnected` na struct `CallManager`.
- **`internal/voip/call/callmanager_relay.go`**:
  - A função `onRelayConnected()` dispara o callback `OnRelayConnected` para notificar a reconexão/ativação do DataChannel WebRTC, mesmo para chamadas em estado `IncomingRinging`.
- **`cmd/server/session.go`**:
  - Registrado `cm.OnRelayConnected` para cancelar o timer `stopTimeout()` imediatamente após o primeiro relay de mídia conectar.
  - Aumentado o timeout padrão de ringing de 60s para 90s.
  - Adicionada verificação `s.mgr.broker.isCallConnected(callID)` antes de executar o timeout, garantindo que sessões secundárias do mesmo número não derrubem uma chamada que já foi atendida por outra instância.
- **`cmd/server/broker.go`**:
  - Adicionado método thread-safe `isCallConnected(id string) bool` para consultar o estado global das chamadas.

---

## 🐳 Docker Images (DockerHub)
- `nathanarend/arendcalls:v2026.18`
- `nathanarend/arendcalls:latest`

### Como Atualizar em Produção:
```bash
docker pull nathanarend/arendcalls:latest
docker restart arendcalls_server
```

---

## 📝 Link Direto para a Release no GitHub
Acesse [https://github.com/nathanarend/ArendCalls/releases/new?tag=v2026.18](https://github.com/nathanarend/ArendCalls/releases/new?tag=v2026.18) para publicar a release com as notas acima.
