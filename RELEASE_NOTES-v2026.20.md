# Release v2026.20 — Gravação de Chamada no Servidor, Controles de Chamada Recebida no Painel e Otimização de CPU do Codec MLow (−63%)

## 📋 Resumo da Release

Esta é uma versão grande, com três frentes:

1. **Gravação de chamada feita no ArendCalls (server-side)**: o servidor — que já é o relay das duas pontas de voz — grava a chamada, gera um **WAV estéreo** (atendente à esquerda, cliente à direita), sobe direto para o **Backblaze B2** e avisa o sistema consumidor por **webhook assinado (HMAC)**. O navegador do atendente **deixa de gravar, codificar e subir áudio** — tira esse peso de máquinas e conexões fracas.
2. **Controles de chamada recebida no painel**: um interruptor **global** para o painel exibir (ou não) chamadas recebidas, com **override por conta** para forçar a exibição, além do nome da instância no modal de chamada recebida. Permite usar o painel como console administrativo enquanto SSE/webhooks continuam disparando normalmente.
3. **Otimização de CPU do codec MLow (−63% no encode)**: o encoder recalculava tabelas trigonométricas e alocava memória a cada quadro de áudio. Passou a memoizar constantes e reaproveitar buffers — **sem nenhuma mudança na qualidade do áudio** (saída byte a byte idêntica, provado pelos testes). Mais chamadas simultâneas por VPS.
4. **Correção de build + ajustes de runtime**: o `main` não compilava de um checkout limpo (dois arquivos de `cmd/server/` estavam fora do git por um padrão solto no `.gitignore`). Corrigido. Docker sem CGO (binário 39 → 28 MB), `GOGC=200`, flag `-pprof` e log de runtime no boot.

---

## 🚀 Principais Mudanças

### 🎙️ Gravação de Chamada no Servidor (`internal/voip/recording` & `cmd/server`)

**Captura e arquivo**
- Pacote novo `internal/voip/recording`. O `Recorder` mixa as duas pernas de voz (PCM 16 kHz mono) num **WAV estéreo em streaming** — canal esquerdo = atendente (`bridge.OnBrowserPCM`), canal direito = cliente (`cm.OnPeerAudio`).
- Ponteiro do `Recorder` lido lock-free (`atomic.Pointer`) no hot path de áudio — a gravação é um ramo paralelo, **nunca adiciona latência nem descarta frame** da conversa ao vivo.
- Mixer por relógio de parede (tick de 20 ms): perna sem áudio (mute, hold, jitter) vira silêncio. Cobre hold/transferência/renegociação sem perder a gravação.
- Header do WAV remendado no `Close` — arquivo válido mesmo se o processo morrer no meio.

**Ciclo de vida**
- Começa **só quando a chamada é atendida** (estado `Active` / `OnRelayConnected`) — nunca no toque.
- Termina no fim da chamada, com **duração exata** medida pelas amostras do servidor.
- Teto de segurança de **40 min** (chamada esquecida fora do gancho não gera arquivo gigante).
- Gravação **< 5 s é descartada** (status `skipped`).

**Envio para o Backblaze B2**
- `PutObject` (API S3) com **AWS SigV4 escrito à mão** — sem SDK novo. Upload em **streaming** do arquivo (`UNSIGNED-PAYLOAD`, ~32 KB de buffer), independe do tamanho da gravação.
- Chave no bucket: `[{prefixo}/]recordings/{clinicId}/{YYYY}/{MM}/{callId}.wav`.
- **Verificação com `HeadObject`** (status + `Content-Length` == tamanho local) **antes** de apagar o WAV local.
- URL GET pré-assinada opcional (`urlTtlSeconds`).

**Fila persistente + pool de workers** (`recording_ctl.go` / `recording_handoff.go`)
- A tabela `call_recordings` é a **fila durável**. Um pool fixo de workers (`--recording-workers`, default **3**) drena um item por worker — 40 chamadas encerrando ≠ 40 uploads simultâneos.
- Set de `inflight` impede o mesmo `callId` ir para dois workers.
- No boot: linha presa em `recording` com WAV íntegro no disco é **salva** (duração estimada pelo tamanho), não falhada.

**Notificação para o sistema consumidor** (`recording_notify.go`)
- `POST {webhookUrl}` com corpo JSON e header `X-ArendCalls-Signature: sha256=<hmac>`:
  ```json
  {
    "callId": "…",
    "sessionId": "…",
    "clinicId": "…",
    "recordingKey": "recordings/{clinicId}/{YYYY}/{MM}/{callId}.wav",
    "recordingUrl": "…",           // omitido se urlTtlSeconds == 0
    "durationSeconds": 143,
    "channels": "stereo",
    "mimeType": "audio/wav",
    "startedAt": "2026-09-05T17:03:11Z",
    "endedAt":   "2026-09-05T17:05:34Z"
  }
  ```
- **Retry com backoff** persistido (1 min / 5 min / 15 min / 1 h), retomado no boot. Desiste em silêncio após 24 tentativas (a linha continua servível por `recording-info`).
- Dedupe pelo consumidor no `callId`.

**Ativação da gravação (por chamada)**
- **Saída**: `record: true` no corpo do `POST /api/sessions/{sid}/calls` (o campo `clinicId` também é aceito e persistido).
- **Entrada (inbound)**: `record: true` no `POST /api/sessions/{sid}/calls/{id}/accept`, **ou** o default `recordInbound` da configuração de gravação da sessão.

**Configuração no painel + SQLite**
- Tabela nova `recording_config` (**por sessão**): `b2Endpoint`, `b2Region`, `b2Bucket`, `b2KeyId`, `b2AppKey`, `b2Prefix`, `webhookUrl`, `webhookSecret`, `urlTtlSeconds`, `recordInbound`.
- **Segredos criptografados** com AES-256-GCM — chave na env `RECORDING_CONFIG_KEY`. **Sem a env var, os segredos vão em texto puro no `wacalls.db` + warning no log** (aceitável só em dev).
- B2 e webhook são **mutuamente obrigatórios**: o `PATCH` recusa (`400`, com lista `missing`) configuração pela metade — ou os 6 campos (endpoint, bucket, key id, app key, webhook URL, webhook secret), ou tudo vazio. Sem configuração completa, o WAV fica no disco aguardando (não é perdido).
- **Sem limite de disco**: grava sempre. `GET .../recordings` expõe `diskUsageBytes` como sinal de saúde.
- Painel: seção "Gravação" no modal de configurações da conta + drawer "Gravações" (ícone `Disc3`) com estado de cada gravação, contagem por status, uso de disco local e aviso se o destino está incompleto.

**Rotas novas (autenticadas por API Key / cookie de admin)**
| Método | Rota | Finalidade |
|---|---|---|
| `GET` | `/api/sessions/{sid}/recording-config` | Ler config de gravação da sessão (segredos redigidos) |
| `PATCH` | `/api/sessions/{sid}/recording-config` | Definir config (parcial; all-or-nothing nos 6 campos) |
| `GET` | `/api/sessions/{sid}/recordings` | Monitoramento: estado por gravação, contagem por status, uso de disco |
| `GET` | `/api/sessions/{sid}/calls/{id}/recording-info` | Reconciliação: mesmo payload do webhook, para o consumidor recuperar notificações perdidas |

---

### 📞 Controles de Chamada Recebida no Painel (`cmd/server` & `client/src`)

- **Interruptor global** (`panel_settings`, singleton, default **ligado**): o painel exibe chamadas recebidas ou não. Rotas `GET`/`PATCH /api/panel-settings`. O valor viaja no payload SSE da lista de sessões como `panelInboundCalls`.
- **Override por conta** (`sessions.panel_inbound`, default **desligado**): força a exibição da chamada recebida daquela conta **mesmo com o interruptor global desligado**. É um bypass de teste — nunca esconde. Rota `PATCH /api/sessions/{sid}/panel-inbound`.
- Regra do modal de chamada recebida: `exibir = panelInboundCalls || session.panelInbound === true`.
- **Nome da instância no modal de chamada recebida** — numa VPS com muitas contas, dá para ver qual está tocando.
- **Modal "Configurações do Painel"** novo (ícone `PhoneIncoming` na barra lateral) com o interruptor global.
- Documentação da API embutida atualizada. Exemplos de webhook **genéricos** (`app consumidor` / `api.seusistema.com`) — sem URLs específicas de nenhum consumidor.

---

### ⚡ Otimização de CPU do Codec MLow (`internal/voip/media/mlow`)

Perfil de CPU do encoder apontou `math.Cos`/`math.Sin` recomputados por quadro (**45% do CPU do encoder**) e ~1740 alocações por quadro. Quatro mudanças, **todas com saída byte a byte idêntica**:

- **`fft.go`** — o `fftRec` recalculava as raízes da unidade a cada chamada. Agora memoiza por tamanho de transformada. Cache **lock-free** (mapa copy-on-write atrás de `atomic.Pointer`) — o `sync.Map` original custava ~10% em `typehash`.
- **`celp_enc.go`** — a busca de codebook (FCB) rodava 12×/quadro e cada chamada alocava um scratch novo (~metade das alocações do encoder). Agora reaproveita o scratch pendurado no `CelpEncoder`, com `reset()` explícito.
- **`lpc.go`** — as tabelas de cosseno do DCT do LPC (~2 mil `math.Cos`, ~16 KB) eram reconstruídas a cada quadro apesar de dependerem só de constantes. Movidas para `sync.Once`.
- **`perc.go`** — os buffers da FFT inversa agora saem do pool que a FFT direta já usa.

**Resultado (`BenchmarkEncodeStream`, mix ~50% fala / ~50% silêncio):**

| | encode µs/quadro | alocações/quadro | bytes/quadro |
|---|---|---|---|
| Antes | 7529 | 1743 | 1027 KB |
| Depois | **~2750** | **595** | **373 KB** |

**−63% de CPU no encode, −66% de alocações.** Em paralelo (8 threads): ~1900 → ~1150 µs por alívio de pressão de GC. Validação: `TestEntropyEncoderByteExact` byte-exact em 61 quadros, `TestSignalModeGroundTruth` `max_err` 1.19e-7 (era ~1e-4), round-trip de tom 0.8883, decode E2E 0.9867 — todos idênticos ao baseline. `go test -race` limpo, inclusive sob encode concorrente.

Novos benchmarks reproduzíveis em `internal/voip/media/mlow/bench_test.go`.

---

### 🛠️ Correção de Build + Runtime (`Dockerfile`, `.gitignore`, `cmd/server/main.go`)

- **`.gitignore` / build**: o padrão solto `server` no `.gitignore` sombreava o diretório inteiro `cmd/server/` — `cmd/server/metrics.go` (endpoint `GET /api/system/metrics`) e `cmd/server/httpapi_test.go` **nunca foram commitados** e um `git clone` do `main` **não compilava**. Os arquivos foram adicionados e os padrões de binário ancorados na raiz (`/server`, `/bridge`, `/arendcalls_bin`, `/wacalls`).
- **Dockerfile**: `modernc.org/sqlite` é Go puro — `CGO_ENABLED=1` + `gcc`/`musl-dev` eram peso morto. Agora `CGO_ENABLED=0` com `-trimpath -ldflags="-s -w"` → binário **estático, ~39 → ~28 MB**, build mais rápido.
- **`ENV GOGC=200`**: deixa o heap crescer ~3× antes de coletar — menos CPU em GC no hot path de áudio (~1,5× de pico de heap a mais). Comentário aponta o operador para `GOMEMLIMIT` (≈ 75% do limite de memória do container).
- **Flag `-pprof <addr>`**: `net/http/pprof` opcional num listener separado (default desligado; para `127.0.0.1`). Não afeta o servidor principal nem a autenticação.
- **Log `runtime` no boot**: `GOMAXPROCS` / `NumCPU` / `GOGC` / `GOMEMLIMIT` — descasamento de quota de CPU do container fica visível de imediato.
- Flags novas: `--recordings-dir` (default `./recordings`), `--recording-workers` (default `3`).

---

## 🗄️ Mudanças no Banco (`wacalls.db`)

Tabelas novas (criadas automaticamente no boot; migração `ALTER TABLE` guardada para bancos existentes):

- **`call_recordings`** — fila durável de gravações: status, chave B2, duração, tentativas de upload/notificação, timestamps.
- **`recording_config`** — configuração de gravação por sessão (credenciais B2 + webhook; segredos criptografados).
- **`panel_settings`** — singleton com o interruptor global de chamadas recebidas.
- **`sessions`** — coluna nova `panel_inbound`.

---

## 🔐 Variáveis de Ambiente Novas

| Variável | Efeito |
|---|---|
| `RECORDING_CONFIG_KEY` | Chave AES-256-GCM para os segredos de gravação no SQLite. **Sem ela, segredos em texto puro + warning.** |
| `GOGC` | Definida como `200` na imagem Docker. Ajustável. |
| `GOMEMLIMIT` | Não definida por padrão. Recomendado em produção (≈ 75% da RAM do container). |

---

## 🧪 Testes e Validação

- `go build ./...` e `go vet ./...` — OK.
- Suíte completa `go test ./...` (sem cache) — **tudo passa**, incluindo o pacote novo `internal/voip/recording` e os testes HTTP de `cmd/server`.
- `go test -race` em todos os pacotes VoIP + servidor, incluindo encode concorrente que exercita os caches compartilhados novos — **sem condição de corrida**.
- Build estilo Docker (`CGO_ENABLED=0 -trimpath -ldflags="-s -w"`) — OK, binário 27,9 MB.
- Smoke test do binário: sobe limpo (pool de gravação inicia, sessões restauram), autenticação `401`/`200`, `/api/system/metrics` e `/api/panel-settings` respondem `200`, rotas de gravação registradas, pprof `200`, desligamento gracioso OK.
- Build de produção do Frontend (`npm run build`) — OK.

---

## 🐳 Docker Images (DockerHub)

- `nathanarend/arendcalls:v2026.20`
- `nathanarend/arendcalls:latest`

### Como Atualizar em Produção

```bash
docker pull nathanarend/arendcalls:latest
# Se for usar gravação, adicionar ao ambiente do container:
#   -e RECORDING_CONFIG_KEY=<32+ bytes aleatórios>
#   (opcional) -e GOMEMLIMIT=1500MiB
docker restart arendcalls_server
```

Depois de subir, configurar o destino da gravação por sessão no painel
(**Configurações da conta → Gravação**) ou via
`PATCH /api/sessions/{sid}/recording-config`, e ativar por chamada com
`record: true` no `POST /calls` (ou no `accept`, ou pelo default `recordInbound`).

Nenhuma migração manual de banco é necessária — as tabelas novas são criadas no boot.

---

## 📝 Link Direto para a Release no GitHub

Acesse [https://github.com/nathanarend/ArendCalls/releases/new?tag=v2026.20](https://github.com/nathanarend/ArendCalls/releases/new?tag=v2026.20) para publicar a release com as notas acima.
