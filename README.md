<div align="center">

# 📞 ArendCalls

**Chamadas de voz nativas do WhatsApp em puro Go e React 19, direto do seu navegador.**
*Fork corporativo de alta performance baseado no [WaCalls](https://github.com/jobasfernandes/wacalls)*

[![Docker](https://img.shields.io/badge/DockerHub-nathanarend%2Farendcalls-blue?logo=docker&logoColor=white)](https://hub.docker.com/r/nathanarend/arendcalls)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![whatsmeow](https://img.shields.io/badge/whatsmeow-VoIP-25D366?logo=whatsapp&logoColor=white)](https://github.com/tulir/whatsmeow)
[![pion](https://img.shields.io/badge/pion-WebRTC-FF6B6B)](https://github.com/pion/webrtc)
[![Licença](https://img.shields.io/badge/licen%C3%A7a-MIT-green.svg)](#licenca)

[Diferenciais](#diferenciais) · [Início Rápido (Docker)](#inicio-rapido) · [Como Funciona](#como-funciona) · [Arquitetura](#arquitetura) · [Rotas da API](#endpoints) · [Gravação](#gravacao) · [Autenticação](#seguranca)

</div>

---

<a id="sobre"></a>
## 💡 Sobre o Projeto

O **ArendCalls** permite conectar uma ou mais contas de WhatsApp via **QR code** e realizar e receber **chamadas de voz 1:1** diretamente de qualquer navegador na rede.

O áudio do microfone do navegador é transmitido como **PCM 16 kHz bruto via canal de dados WebRTC** até o servidor Go. O servidor codifica o áudio utilizando o codec **MLow** da Meta e o injeta na malha de **relays SRTP do WhatsApp**. No fluxo inverso, o áudio do interlocutor é decodificado e reproduzido em tempo real no navegador.

Todo o ecossistema VoIP roda **nativamente em puro Go**:
- Codec de voz MLow embutido (otimizado — **−63% de CPU no encode** desde a v2026.20, sem perda de qualidade)
- Empacotamento RTP/SRTP e STUN
- Transporte de relay WebRTC/SCTP
- Sinalização `<call>` integrada ao [**whatsmeow**](https://github.com/tulir/whatsmeow)
- **Gravação da chamada no servidor** (WAV estéreo) com upload direto para o Backblaze B2 — opcional, por chamada
- Cliente moderno construído em **React 19 + Tailwind CSS**
- **Sem necessidade de CGO, compiladores C ou DLLs externas** (binário estático)

---

<a id="diferenciais"></a>
## 🚀 Principais Recursos e Diferenciais

Este repositório (`ArendCalls`) traz diversas melhorias de engenharia e usabilidade em relação ao projeto original:

| Recurso | Detalhes |
|---|---|
| 🇧🇷 **Interface 100% em Português** | Telas, modais, mensagens de erro, alertas e documentação totalmente traduzidos (pt-BR). |
| 🎙️ **Gravação no Servidor** | O ArendCalls grava a chamada (WAV estéreo, atendente/cliente separados), sobe para o Backblaze B2 e avisa por webhook assinado (HMAC). O navegador do atendente não grava nem sobe áudio. Ativação por chamada. |
| 📴 **Controle de Chamadas Recebidas no Painel** | Interruptor global para o painel exibir (ou não) chamadas recebidas, com override por conta. Permite usar o painel como console administrativo enquanto SSE/webhooks seguem disparando. |
| ⏸️ **Modo Espera (Hold) Integrado** | Botão no painel de chamada para colocar o cliente em espera tocando música suave sem encerrar a ligação. |
| ⚡ **Codec MLow Otimizado** | Encode ~2,7× mais rápido (memoização de twiddles do FFT, pool de scratch da busca CELP) — saída de áudio byte a byte idêntica, mais chamadas simultâneas por VPS. |
| 🛡️ **Autenticação Unificada** | Chave mestra global (`API_KEY`) para segurança nas integrações de backend e cookies de sessão para o painel web. |
| 🏢 **Gerenciador Visual de Instâncias** | Criar, renomear e gerenciar múltiplas conexões de WhatsApp diretamente pela barra lateral. |
| 💾 **Persistência Inteligente** | Contas deslogadas são mantidas salvas no banco de dados SQLite (`logged_out`), preservando IDs e webhooks. |
| 📊 **Telemetria da VPS sob Demanda** | Modal com RAM, CPU, uptime, goroutines, disco e chamadas ativas — polling só enquanto aberto, zero overhead em background. |
| 📖 **Guia da API Integrado** | Documentação interativa embutida na própria interface com exemplos de cURL, Webhooks e Server-Sent Events (SSE). |
| 🔄 **Máquina de Estados Precisa** | Status claros e confiáveis: *Ligando...* (`starting`) ➔ *Chamando...* (`ringing` ao tocar) ➔ *Em chamada* (`active`). |
| 🐳 **Pronto para Produção (Docker)** | Imagem oficial e leve no DockerHub (`nathanarend/arendcalls:latest`), binário estático sem CGO, com suporte nativo a Traefik. |

---

<a id="inicio-rapido"></a>
## 🐳 Início Rápido com Docker (Recomendado)

A maneira mais prática e recomendada para rodar em servidores e VPS:

### 1. Crie o arquivo `docker-compose.yml`

```yaml
version: '3.8'

services:
  arendcalls:
    image: nathanarend/arendcalls:latest
    container_name: arendcalls_server
    restart: unless-stopped
    network_mode: "host"
    environment:
      - API_KEY=sua_chave_mestra_secreta_aqui
      # Só se for usar gravação de chamada — chave AES-256-GCM (32+ bytes aleatórios)
      # para cifrar as credenciais do B2/webhook no wacalls.db:
      - RECORDING_CONFIG_KEY=troque_por_32_bytes_aleatorios_aqui
      # Recomendado em produção: ~75% do limite de memória do container
      # - GOMEMLIMIT=1500MiB
    volumes:
      - arendcalls_data:/app/data
      # Gravações ainda não enviadas ao B2 (fila local):
      - arendcalls_recordings:/app/recordings
    expose:
      - "8080"
    ports:
      - "50000-50100:50000-50100/udp"
    deploy:
      labels:
        - traefik.enable=true
        - traefik.http.routers.arendcalls.rule=Host(`call.seudominio.com`)
        - traefik.http.routers.arendcalls.entrypoints=websecure
        - traefik.http.routers.arendcalls.tls.certresolver=letsencryptresolver
        - traefik.http.services.arendcalls.loadbalancer.server.port=8080

volumes:
  arendcalls_data:
    driver: local
  arendcalls_recordings:
    driver: local
```

### 2. Suba o container

```bash
docker compose up -d
```

Acesse o endereço configurado (ou `http://localhost:8080`), clique em **Nova Conexão** e leia o QR Code com o seu WhatsApp (**Aparelhos Conectados**)!

---

<a id="instalacao-local"></a>
## 💻 Instalação e Execução Local

### Pré-requisitos
- **Go 1.26+**
- **Node.js 20+** e **npm**

### Passo a Passo

```bash
# 1. Clone o repositório
git clone https://github.com/nathanarend/ArendCalls.git
cd ArendCalls

# 2. Instale as dependências e compile o cliente web
cd client
npm install
npm run build
cd ..

# 3. Baixe as dependências do Go e inicialize o servidor
go mod download
go run ./cmd/server -addr :8080 -static client/dist
```

### Parâmetros de Inicialização do Servidor

| Parâmetro | Padrão | Descrição |
|---|---|---|
| `-addr` | `:8080` | Endereço e porta de escuta HTTP |
| `-db` | `wacalls.db` | Caminho do arquivo de banco SQLite das instâncias |
| `-static` | `client/dist` | Pasta com os arquivos estáticos do frontend compilado |
| `-debug` | `false` | Habilita logs detalhados do WhatsApp e WebRTC |
| `-max-calls` | `0` | Limite de chamadas simultâneas por conta (`0` = sem limite) |
| `-apikey` | `""` | Define a Chave de Super-Usuário (sobrescreve a env `API_KEY`) |
| `-recordings-dir` | `recordings` | Pasta para os WAVs de gravação ainda não enviados ao B2 |
| `-recording-workers` | `3` | Workers concorrentes de upload das gravações para o B2 |
| `-pprof` | `""` | Se definido, serve `net/http/pprof` nesse endereço (use `127.0.0.1:6060` — **nunca exponha publicamente**) |

**Variáveis de ambiente relevantes:** `API_KEY` (chave mestra),
`RECORDING_CONFIG_KEY` (AES-256-GCM para cifrar credenciais de gravação no
`wacalls.db` — sem ela, segredos em texto puro + warning), `GOGC` (padrão `200` na
imagem Docker), `GOMEMLIMIT` (recomendado em produção, ~75% da RAM do container).

---

<a id="como-funciona"></a>
## 🔄 Como Funciona o Fluxo de Chamada

```
1. POST .../calls            ➔ O servidor cria a chamada e envia a oferta <call> ao WhatsApp
2. Browser abre WebRTC       ➔ POST .../calls/{id}/webrtc (troca de SDP do áudio local)
3. Destinatário Atende       ➔ WhatsApp confirma aceite e entrega chaves criptográficas
4. Conexão de Transporte     ➔ STUN/ICE conecta os relays do WhatsApp via DataChannel
5. Conversação Ativa         ➔ Fluxo bidirecional SRTP com codec MLow rodando
6. Finalização               ➔ DELETE .../calls/{id} encerra a chamada e libera recursos
```

---

<a id="arquitetura"></a>
## 🏗️ Arquitetura

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          NAVEGADOR (Cliente React 19)                    │
│   Microfone + Fone  ·  WebRTC Data Channel (16 kHz PCM)  ·  HTTP + SSE   │
└───────────────────────────────┬──────────────────────────────────────────┘
                                 │  POST /api/sessions/{sid}/calls/{id}/webrtc (SDP)
                                 │  GET  /api/events                           (SSE)
                                 ▼
┌──────────────────────────── SERVIDOR GO (cmd/server) ──────────────────────┐
│  SessionManager   Gerenciador de contas (whatsmeow + CallManager + bridge)  │
│  Broker           Hub de eventos SSE e despachante de Webhooks              │
│  Bridge           Pion WebRTC bridge (PCM 16 kHz ⇄ Core VoIP)               │
│                                                                            │
│  internal/wa       Adaptador VoipSocket sobre whatsmeow                    │
│  internal/voip     CallManager · Sinalização · Codec MLow · Transporte    │
│  internal/voip/recording   Grampo paralelo · WAV estéreo · fila · upload  │
└──────────┬────────────────────┬───────────────────────────┬───────────────┘
           │ Sinalização        │ Mídia SRTP / MLow         │ WAV (após atender)
           ▼                    ▼                           ▼
   ┌───────────────┐   ┌──────────────────────┐   ┌──────────────────────┐
   │  WhatsApp WS  │   │   WhatsApp Relays    │   │   Backblaze B2 (S3)  │
   │  (whatsmeow)  │   │  (SRTP over SCTP/DC) │   └──────────┬───────────┘
   └───────────────┘   └──────────────────────┘   webhook HMAC ▼
                                                  ┌──────────────────────┐
                                                  │    App consumidor    │
                                                  └──────────────────────┘
```

---

<a id="endpoints"></a>
## 📡 Endpoints da API

Todas as rotas de API exigem autenticação (ver [Segurança](#seguranca)).

### Instâncias / Sessões

| Método | Rota | Finalidade |
|---|---|---|
| `GET` | `/api/sessions` | Listar todas as instâncias cadastradas |
| `POST` | `/api/sessions` | Criar nova instância e iniciar pareamento QR |
| `PATCH` | `/api/sessions/{sid}` | Renomear o nome de identificação da conta |
| `PATCH` | `/api/sessions/{sid}/webhook` | Configurar URL de webhook para eventos |
| `DELETE` | `/api/sessions/{sid}` | Excluir e desvincular a instância |
| `POST` | `/api/sessions/{sid}/pair` | Gerar novo QR Code para uma conta desconectada |
| `POST` | `/api/sessions/{sid}/logout` | Desconectar sessão (mantém no banco para re-parear) |
| `POST` | `/api/sessions/{sid}/start` · `/restart` · `/stop` | Controlar o ciclo de vida da conexão da instância |
| `POST` | `/api/sessions/{sid}/check-number` | Verificar se um número tem WhatsApp |

### Chamadas

| Método | Rota | Finalidade |
|---|---|---|
| `POST` | `/api/sessions/{sid}/calls` | Iniciar chamada de voz — `{ phone, record?, clinicId?, duration_ms? }` |
| `POST` | `/api/sessions/{sid}/calls/{id}/webrtc` | Trocar SDP do WebRTC para streaming de áudio |
| `POST` | `/api/sessions/{sid}/calls/{id}/accept` | Atender chamada recebida — `{ record?, clinicId? }` |
| `POST` | `/api/sessions/{sid}/calls/{id}/reject` | Rejeitar chamada recebida |
| `POST` | `/api/sessions/{sid}/calls/{id}/hold` · `/unhold` | Colocar em espera / retomar |
| `DELETE` | `/api/sessions/{sid}/calls/{id}` | Desligar/encerrar chamada ativa |
| `GET` | `/api/sessions/{sid}/history` | Histórico das últimas 50 chamadas da instância |

### Gravação de Chamadas

| Método | Rota | Finalidade |
|---|---|---|
| `GET` · `PATCH` | `/api/sessions/{sid}/recording-config` | Ler/definir destino da gravação da sessão (B2 + webhook; segredos redigidos na leitura; os 6 campos B2+webhook são all-or-nothing) |
| `GET` | `/api/sessions/{sid}/recordings` | Monitoramento: estado por gravação, contagem por status, uso de disco local |
| `GET` | `/api/sessions/{sid}/calls/{id}/recording-info` | Reconciliação: mesmo payload do webhook, para recuperar notificações perdidas |

### Painel / Sistema / Eventos

| Método | Rota | Finalidade |
|---|---|---|
| `GET` · `PATCH` | `/api/panel-settings` | Interruptor global "exibir chamadas recebidas no painel" |
| `PATCH` | `/api/sessions/{sid}/panel-inbound` | Override por conta: forçar exibição da chamada recebida dessa conta |
| `GET` | `/api/system/metrics` | Telemetria sob demanda da VPS e ArendCalls (RAM, CPU, uptime, disco) |
| `GET` | `/api/events` | Stream global de eventos em tempo real (SSE) |
| `GET` | `/api/sessions/{sid}/events` | Stream de eventos de uma sessão específica (SSE) |

---

<a id="gravacao"></a>
## 🎙️ Gravação de Chamada no Servidor

O ArendCalls — que já é o relay das duas pontas de voz — grava a chamada, gera um
**WAV estéreo 16 kHz** (canal esquerdo = atendente, direito = cliente), sobe direto
para o **Backblaze B2** e avisa o sistema consumidor por **webhook assinado**. O
navegador do atendente não grava, não codifica e não sobe áudio.

**Como ligar:**

1. Definir o destino **por sessão** (painel → *Configurações da conta → Gravação*,
   ou `PATCH /api/sessions/{sid}/recording-config`):

   ```json
   {
     "b2Endpoint": "https://s3.us-west-000.backblazeb2.com",
     "b2Region": "us-west-000",
     "b2Bucket": "meu-bucket-privado",
     "b2KeyId": "…",
     "b2AppKey": "…",
     "b2Prefix": "",
     "webhookUrl": "https://api.seusistema.com/calls/recording-ready",
     "webhookSecret": "…",
     "urlTtlSeconds": 3600,
     "recordInbound": false
   }
   ```

   > B2 e webhook são **mutuamente obrigatórios**: ou os 6 campos (endpoint, bucket,
   > key id, app key, webhook URL, webhook secret), ou tudo vazio. Sem configuração
   > completa, o WAV fica no disco aguardando (não é perdido).

2. Ativar **por chamada**: `record: true` no corpo do `POST /calls` (saída) ou do
   `POST .../accept` (entrada). Alternativamente, `recordInbound: true` na config
   grava toda chamada recebida atendida. O campo `clinicId` (opcional) entra na
   chave do arquivo.

**Fluxo:** começa quando a chamada é atendida (nunca no toque), sobrevive a
hold/transferência, teto de 40 min, grava a duração exata pelo relógio do servidor,
descarta gravações < 5 s. Uma **fila durável** (`call_recordings` no SQLite) drenada
por um **pool de workers** garante que uma rajada de encerramentos não vire uma
rajada de uploads. Depois do upload verificado (`HeadObject`), o WAV local é apagado.

**Webhook "gravação pronta"** — `POST {webhookUrl}` com header
`X-ArendCalls-Signature: sha256=<hmac>` e corpo:

```json
{
  "callId": "…", "sessionId": "…", "clinicId": "…",
  "recordingKey": "recordings/{clinicId}/{YYYY}/{MM}/{callId}.wav",
  "recordingUrl": "…",
  "durationSeconds": 143, "channels": "stereo", "mimeType": "audio/wav",
  "startedAt": "2026-09-05T17:03:11Z", "endedAt": "2026-09-05T17:05:34Z"
}
```

Retry com backoff (1 min / 5 min / 15 min / 1 h) persistido e retomado no boot;
`GET .../calls/{id}/recording-info` devolve o mesmo payload para reconciliação.

> **`RECORDING_CONFIG_KEY`** — defina esta env var (32+ bytes aleatórios) para que
> as credenciais do B2 e o segredo do webhook sejam cifrados (AES-256-GCM) no
> `wacalls.db`. Sem ela, os segredos ficam em **texto puro** e o servidor loga um
> aviso no boot.

---

<a id="painel-inbound"></a>
## 📴 Chamadas Recebidas no Painel

- **Interruptor global** (`GET`/`PATCH /api/panel-settings`, default **ligado**):
  liga/desliga a exibição de chamadas recebidas no painel para todas as contas.
- **Override por conta** (`PATCH /api/sessions/{sid}/panel-inbound`, default
  **desligado**): força a exibição da chamada recebida daquela conta mesmo com o
  interruptor global desligado — é um bypass, nunca esconde.
- Regra: `exibir = interruptor_global OU override_da_conta`.

Com o painel silenciado, os eventos SSE e os webhooks de chamada recebida
**continuam disparando** normalmente — útil para rodar o painel como console
administrativo enquanto um app/endpoint externo atende as chamadas.

---

<a id="seguranca"></a>
## 🔒 Segurança e Autenticação

### 1. Acesso Direto (Local ou com API Key)
Envie a chave no cabeçalho `Authorization: Bearer <API_KEY>`, `X-Api-Key: <API_KEY>` ou via parâmetro de URL `?apikey=<API_KEY>`:

```bash
curl -X GET "https://call.seudominio.com/api/sessions" \
  -H "Authorization: Bearer sua_chave_mestra_aqui"
```

### 2. Com Proxy Reverso Traefik (Basic Auth + API Key)
```bash
curl -s -u usuario:senha "https://call.seudominio.com/api/sessions?apikey=sua_chave_mestra_aqui"
```

> **Aviso de Segurança:** O arquivo `wacalls.db` guarda os tokens e credenciais de sessão do WhatsApp, e as credenciais do B2/webhook de gravação (cifradas se `RECORDING_CONFIG_KEY` estiver definida). **Nunca commite este arquivo** em repositórios públicos e mantenha seus backups protegidos.

> **pprof:** a flag `-pprof` fica desligada por padrão. Se ligar, aponte apenas para
> `127.0.0.1` — a porta expõe profiling e não deve ficar acessível publicamente.

---

<a id="creditos"></a>
## 👥 Créditos e Agradecimentos

O **ArendCalls** é desenvolvido como um fork aprimorado do projeto de código aberto [WaCalls](https://github.com/jobasfernandes/wacalls), criado por:

<div align="center">

<a href="https://github.com/jotadev66"><img src="https://github.com/jotadev66.png" width="60" height="60" style="border-radius:50%" alt="jotadev66"/></a>
<a href="https://github.com/jobasfernandes"><img src="https://github.com/jobasfernandes.png" width="60" height="60" style="border-radius:50%" alt="jobasfernandes"/></a>
<a href="https://github.com/edgardmessias"><img src="https://github.com/edgardmessias.png" width="60" height="60" style="border-radius:50%" alt="edgardmessias"/></a>
<a href="https://github.com/w3nder"><img src="https://github.com/w3nder.png" width="60" height="60" style="border-radius:50%" alt="w3nder"/></a>

[**@jotadev66**](https://github.com/jotadev66) · [**@jobasfernandes**](https://github.com/jobasfernandes) · [**@edgardmessias**](https://github.com/edgardmessias) · [**@w3nder**](https://github.com/w3nder)

</div>

Projetos de base essenciais:
- [**whatsmeow**](https://github.com/tulir/whatsmeow) — Biblioteca em Go para o protocolo do WhatsApp Web
- [**pion/webrtc**](https://github.com/pion/webrtc) — Stack WebRTC pura em Go
- [**whatsapp-rust**](https://github.com/oxidezap/whatsapp-rust) — Implementação de referência do codec MLow

---

<a id="licenca"></a>
## 📄 Licença

Distribuído sob a licença [MIT](./LICENSE).

