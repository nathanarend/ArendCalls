# Fix: Chamadas Caindo em ~60 Segundos (Multi-Device WhatsApp)

## Data
2026-08-31

## Problema

Chamadas de saída feitas pelo **Mocho CRM** eram encerradas após aproximadamente **60 segundos**, mesmo com a ligação ativa e o peer atendendo normalmente.

### Causa Raiz

O ArendCalls possui um timer anti-zombie de 60 segundos em `wireCall` (`session.go`) para encerrar chamadas que ficam travadas em estado de ringing sem ninguém atender. O problema é que, em ambientes com **WhatsApp Multi-Device**, o mesmo `call_id` é recebido por **múltiplas sessões simultâneas**:

- **Sessão A** (`690a8426...`) — é a sessão "dono" da chamada. Processa o accept corretamente, vai para `Active`.
- **Sessão B** (`dc0b6ada...`) — é uma sessão "espelho" (outro device pareado). Recebe a chamada como `IncomingRinging`, nunca é aceita localmente, e após 60s o seu timer dispara `cm.EndCall()`.

O `EndCall` da Sessão B envia um stanza `terminate` ao WhatsApp, que repassa para o peer, que encerra a chamada. A Sessão A recebe isso como `"call terminated by peer, reason=user_ended"` — parecendo que o usuário desligou voluntariamente.

### Evidência nos Logs

```
17:35:23 - remote accepted call    session=dc0b6ada  relay_connected=false
17:35:23 - call ACTIVE             session=690a8426  ← sessão correta, chamada ativa
17:35:23 - relay datachannel open  session=dc0b6ada  ← relay conectou, mas estado não avançou
17:36:19 - call ringing timeout    session=dc0b6ada  ← 60s depois, mata a chamada
17:36:19 - call terminated by peer session=690a8426  ← sessão ativa derrubada pelo "fantasma"
```

O relay conectou na Sessão B (`datachannel open`), mas `onRelayConnected()` no `callmanager_relay.go`
só aplicava `TransitionMediaConnected` se o estado fosse `CallStateConnecting`. A Sessão B estava em
`IncomingRinging`, então a transição era rejeitada, `stopTimeout()` nunca era chamado, e o timer disparava.

---

## Arquivos Alterados

### `internal/voip/call/callmanager.go`
Adicionado campo `OnRelayConnected func()` ao struct `CallManager`.
Permite que `wireCall` registre um callback a ser chamado quando o relay de mídia conecta,
independente do estado da máquina de estados.

### `internal/voip/call/callmanager_relay.go`
A função `onRelayConnected()` agora dispara o callback `OnRelayConnected` após desbloquear o mutex.
Isso notifica `wireCall` para cancelar o timer, mesmo quando o estado é `IncomingRinging` (sessão-espelho).

```go
// ANTES: só cancelava o timer via OnStateChange (que exige estado Connecting/Active)
// DEPOIS: cancela o timer diretamente quando o relay conecta
cb := m.OnRelayConnected
m.mu.Unlock()
if cb != nil {
    cb()
}
```

### `cmd/server/session.go` — função `wireCall`

**Mudança 1 — Novo callback `cm.OnRelayConnected`:**
```go
cm.OnRelayConnected = func() {
    s.log.Info("relay connected: cancelling ringing timeout", "call_id", callID)
    stopTimeout()
}
```
Quando o relay de mídia conecta em qualquer sessão, o timer é cancelado imediatamente.

**Mudança 2 — Timer aumentado de 60s para 90s:**
```go
// ANTES: time.AfterFunc(60*time.Second, ...)
// DEPOIS: time.AfterFunc(90*time.Second, ...)
```
Margem de segurança extra para chamadas que demoram mais para o peer atender.

**Mudança 3 — Verificação no disparo do timeout:**
```go
if s.mgr.broker.isCallConnected(callID) {
    s.log.Info("call ringing timeout: call already active in broker, skipping EndCall", ...)
    s.removeCall(callID)
    return
}
```
Se a chamada já está com status `connected` no broker (outra sessão já a aceitou),
o timeout remove a entrada local sem enviar `terminate` ao WhatsApp.

### `cmd/server/broker.go`

Adicionado método `isCallConnected(id string) bool` — segunda linha de defesa para o cenário multi-sessão.

### `internal/voip/call/callmanager.go` — alteração de `Query` para `SendNode` em `EndCall` e `RejectCall`
As stanzas de encerramento (`<terminate>`) e rejeição (`<reject>`) de chamadas são notificações assíncronas enviadas ao WhatsApp. O uso anterior de `m.sock.Query(sendCtx, node)` aguardava uma resposta de ACK síncrona com ID da Meta que o WhatsApp não envia para estes tipos de stanza, fazendo com que o envio travasse/expirasse via timeout e o outro aparelho ficasse travado em "reconectando-se". Alterado para `m.sock.SendNode(sendCtx, node)`.

---


## Deploy

O binário compilado com as correções está em `./server` (gerado em 2026-08-31).

```bash
# Build nova imagem Docker
docker build -t nathanarend/arendcalls:latest .
docker push nathanarend/arendcalls:latest

# No servidor de produção
docker pull nathanarend/arendcalls:latest
docker restart arendcalls_server
```

---

## Mudanças no Mocho CRM (paralelas)

- `src/lib/arendcalls-webrtc.ts` — timeout de reconexão 6s → 15s + callback `onReconnecting`
- `src/contexts/CallContext.tsx` — estado `isReconnecting` exposto no contexto
- `src/components/call/active-call-modal.tsx` — banner "Reconectando... A ligação continua ativa"
