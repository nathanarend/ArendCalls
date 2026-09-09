# Release v2026.21 — Correção: áudio de entrada mudo ao atender chamada no painel

## 📋 Resumo

Patch em cima da v2026.20. Corrige um bug que estava presente **desde a
v2026.19**: ao atender uma chamada **recebida pelo painel**, ela aparecia como se
fosse de outro operador (só visualização, sem controles) e **o áudio de entrada
não tocava no navegador** — o microfone saía normalmente para o outro lado, mas a
voz do interlocutor não chegava ao painel.

## 🐛 O bug

A v2026.19 (`6ad126f`, "transparent concurrency protection") passou a definir o
**dono** de uma chamada atendida como uma string aleatória `claim-<hex>`,
ignorando o `X-Client-Id` do navegador que atendeu.

O painel decide o que é "minha chamada" comparando `call.owner === getClientId()`.
Como o dono virou um valor aleatório, essa comparação **nunca** dava certo para
chamadas atendidas no painel:

- a chamada era renderizada como chamada de terceiros (lista passiva `OtherCallsList`),
  sem `<audio>` e sem botões;
- o `useAcceptCall` ainda montava o WebRTC, então o microfone funcionava (painel → interlocutor);
- mas a reprodução do áudio de entrada depende do card da chamada renderizar → **mudo no painel**.

Sintoma relatado: *"a ligação fica só nas chamadas ativas, como se fosse outro
usuário; no celular escuto, no painel não sai nada."*

## 🔧 A correção

`cmd/server/httpapi.go` — `doAccept`:

```go
owner := clientID(r)
if owner == "" {
    // atendimento via API pura, sem X-Client-Id → claim sintético
    b := make([]byte, 8)
    _, _ = rand.Read(b)
    owner = "claim-" + hex.EncodeToString(b)
}
if !s.broker.setOwner(id, owner) {
    // outra requisição já assumiu → 409
}
```

- O navegador que atende volta a ser o dono da chamada → `isMine` fica verdadeiro
  → o card renderiza → áudio de entrada volta.
- A proteção contra atendimento concorrente **continua igual**: `broker.setOwner`
  é compare-and-set, a primeira requisição tranca e qualquer atendimento
  simultâneo de outro cliente recebe `409 Conflict`.
- O dono é preservado nas mudanças de estado (`OnStateChange` copia
  `existing.Owner`), então o `call-list` seguinte já carrega o dono correto.

## 🧪 Testes

- `go build ./...`, `go vet ./...`, `go test ./...` (sem cache) — tudo passa.
- Sem impacto em chamadas de saída nem na otimização de CPU do MLow (v2026.20).

## 🐳 Docker

- `nathanarend/arendcalls:v2026.21`
- `nathanarend/arendcalls:latest`

### Atualizar em produção

```bash
docker pull nathanarend/arendcalls:latest
docker restart arendcalls_server
```

Depois: ligar de um número externo para um número do painel → o modal
"Chamada Recebida" aparece → **Atender** → áudio deve funcionar nos dois sentidos,
e a chamada aparece como sua (com botões de encerrar/espera).

## 📝 GitHub Release

https://github.com/nathanarend/ArendCalls/releases/new?tag=v2026.21
