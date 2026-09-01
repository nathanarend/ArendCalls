# PATCH: EndCall — stanza de terminate não chegava à outra ponta

## Sintoma
O celular da pessoa ficava preso em **"Reconectando-se…"** após o atendente encerrar a chamada
pelo TalkDash. O TalkDash recebia `204 No Content` e mostrava sucesso, mas a chamada
continuava viva no WhatsApp.

## Causa Raiz
Dois bugs independentes que se somavam:

### Bug 1 — `callmanager.go`: EndCall engolia a falha de envio
Quando o socket do WhatsApp estava reconectando (janela de ~3s comum em conexões móveis),
o `SendNode` falhava silenciosamente. O código só logava um `Warn` e destruía a mídia
local retornando `nil` — a outra ponta nunca recebia o stanza `<terminate>`.

### Bug 2 — `httpapi.go`: HTTP 204 independente do resultado
- O `204` e o `broker.endCall` estavam **fora** do `if ok`, então mesmo quando a chamada
  nem era encontrada, o servidor respondia "encerrado com sucesso" e disparava o webhook
  `call-ended`.
- O erro do `EndCall` era descartado com `_ = ...`.

---

## Diffs Aplicados

### `internal/voip/call/callmanager.go`

```diff
-	sendCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
-	defer cancel()
-	if err := m.sock.SendNode(sendCtx, node); err != nil {
-		m.log.Warn("failed to send call terminate stanza", "call_id", call.CallID, "err", err)
-	}
-
-	if m.OnEnded != nil {
-		m.OnEnded(ended)
-	}
-	m.cleanupMedia()
-	return nil
+	// Tentativa 1
+	sendCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
+	err := m.sock.SendNode(sendCtx, node)
+	cancel()
+
+	var sendErr error
+	if err != nil {
+		m.log.Warn("failed to send call terminate stanza (attempt 1/2)", "call_id", call.CallID, "err", err)
+		// Aguarda 500ms e tenta novamente — cobre o caso mais comum de socket em reconexão
+		time.Sleep(500 * time.Millisecond)
+		sendCtx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
+		err2 := m.sock.SendNode(sendCtx2, node)
+		cancel2()
+		if err2 != nil {
+			m.log.Error("failed to send call terminate stanza (attempt 2/2)", "call_id", call.CallID, "err", err2)
+			sendErr = err2
+		}
+	}
+
+	if m.OnEnded != nil {
+		m.OnEnded(ended)
+	}
+	// A mídia é sempre limpa — não é possível manter a chamada viva sem sinalização.
+	m.cleanupMedia()
+	return sendErr
```

### `cmd/server/httpapi.go`

```diff
-	if ok {
-		_ = ac.cm.EndCall(r.Context(), core.EndCallReasonUserEnded)
-	}
-	sess.removeCall(id)
-	s.broker.endCall(id, string(core.EndCallReasonUserEnded))
-	w.WriteHeader(http.StatusNoContent)
+	if !ok {
+		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no such call"})
+		return
+	}
+	if err := ac.cm.EndCall(r.Context(), core.EndCallReasonUserEnded); err != nil {
+		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to send terminate: " + err.Error()})
+		return
+	}
+	sess.removeCall(id)
+	s.broker.endCall(id, string(core.EndCallReasonUserEnded))
+	w.WriteHeader(http.StatusNoContent)
```

---

## Tabela de Respostas HTTP após o Patch

| Cenário                          | Antes | Depois |
|----------------------------------|-------|--------|
| Chamada não encontrada           | 204   | 404    |
| `EndCall` falha (socket/timeout) | 204   | 502    |
| Sucesso real                     | 204   | 204    |

---

## Como Validar em Produção

1. **Caminho feliz**: ligar, atender no celular, desligar pelo TalkDash →
   deve cair na hora, sem "Reconectando-se…", TalkDash recebe `204`.

2. **Caminho de erro**: derrube a rede do container por ~5s e desligue durante
   a janela de reconexão → TalkDash deve receber `502` e exibir erro ao atendente,
   em vez de mostrar sucesso falso.

3. **Chamada inexistente**: enviar `DELETE` com um `callId` inválido →
   deve retornar `404` (antes retornava `204`).

---

## Deploy

O deploy usa a imagem `nathanarend/arendcalls:latest` pelo EasyPanel.
Fazer build e push de uma nova tag antes de atualizar no painel:

```bash
docker build -t nathanarend/arendcalls:endcall-fix .
docker push nathanarend/arendcalls:endcall-fix
```

Depois trocar a imagem no EasyPanel para `nathanarend/arendcalls:endcall-fix`.
