<div align="center">

# 📞 ArendCalls

**Chamadas de voz nativas do WhatsApp em puro Go e React 19, direto do seu navegador.**
*Fork corporativo de alta performance baseado no [WaCalls](https://github.com/jotadev66/wacalls)*

[![Docker](https://img.shields.io/badge/DockerHub-nathanarend%2Farendcalls-blue?logo=docker&logoColor=white)](https://hub.docker.com/r/nathanarend/arendcalls)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![whatsmeow](https://img.shields.io/badge/whatsmeow-VoIP-25D366?logo=whatsapp&logoColor=white)](https://github.com/tulir/whatsmeow)
[![pion](https://img.shields.io/badge/pion-WebRTC-FF6B6B)](https://github.com/pion/webrtc)
[![Licença](https://img.shields.io/badge/licen%C3%A7a-MIT-green.svg)](#licenca)

[Diferenciais](#diferenciais) · [Início Rápido (Docker)](#inicio-rapido) · [Windows + Cloudflare Tunnel](#cloudflare-tunnel) · [Como Funciona](#como-funciona) · [Arquitetura](#arquitetura) · [Rotas da API](#endpoints) · [Gravação](#gravacao) · [Migração](#migracao) · [Autenticação](#seguranca)

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

> [!TIP]
> **Windows sem abrir portas:** rode o `arendcalls.exe` e acesse o painel de qualquer lugar por um endereço `https://` com o **Cloudflare Tunnel**. É o jeito mais fácil: nada de IP fixo, roteador ou certificado. [Passo a passo](#cloudflare-tunnel).

> [!TIP]
> **Compatível em paralelo com WAHA / APIs de Mensagens:**
> Graças à arquitetura Multi-Device (MDv2) do WhatsApp, o **ArendCalls pode ser utilizado em paralelo ao [WAHA](https://waha.devlike.pro/)** ou Evolution API no mesmo número/conta, **sem qualquer interferência**. O WAHA gerencia mensagens de texto, mídias e automações de chat, enquanto o ArendCalls atua de forma independente e dedicada exclusivamente para **chamadas de voz VoIP nativas**.

---

<a id="diferenciais"></a>
## 🚀 Principais Recursos e Diferenciais

Este repositório (`ArendCalls`) traz diversas melhorias de engenharia e usabilidade em relação ao projeto original:

| Recurso | Detalhes |
|---|---|
| 🇧🇷 **Interface 100% em Português** | Telas, modais, mensagens de erro, alertas e documentação totalmente traduzidos (pt-BR). |
| 🤝 **Paralelo com WAHA Sem Interferência** | Opera simultaneamente com o WAHA no mesmo número de WhatsApp. O WAHA cuida das mensagens/mídias e o ArendCalls cuida das ligações de voz. |
| 🟢 **Keepalive Anti-Desconexão & Presença** | Pulso de presença (30s available, depois unavailable) ao conectar, ao parear (quando o pushname chega) e no worker diário: atualiza a última atividade em "Dispositivos conectados" e evita a expiração de 14 dias dos companion devices. Totalmente invisível para contatos (não exibe "online" nos chats). Endpoint manual dedicado `POST /api/sessions/{sid}/presence`. |
| 🎙️ **Gravação no Servidor** | O ArendCalls grava a chamada (WAV estéreo, atendente/cliente separados), sobe para o Backblaze B2 e avisa por webhook assinado (HMAC). O navegador do atendente não grava nem sobe áudio. Ativação por chamada. |
| 📴 **Controle de Chamadas Recebidas no Painel** | Interruptor global para o painel exibir (ou não) chamadas recebidas, com override por conta. Permite usar o painel como console administrativo enquanto SSE/webhooks seguem disparando. |
| ⏸️ **Modo Espera (Hold) Integrado** | Botão no painel de chamada para colocar o cliente em espera reproduzindo áudio de espera sem encerrar a ligação. |
| ⚡ **Codec MLow Otimizado** | Encode ~2,7× mais rápido (memoização de twiddles do FFT, pool de scratch da busca CELP) — saída de áudio byte a byte idêntica, mais chamadas simultâneas por VPS. |
| 🛡️ **Autenticação Unificada** | Chave mestra global (`API_KEY`) para as integrações de backend e login com senha (hash bcrypt, bloqueio por tentativas) para o painel web. |
| 🏢 **Gerenciador Visual de Instâncias** | Criar, renomear e gerenciar múltiplas conexões de WhatsApp diretamente pela barra lateral. |
| 🚚 **Migração entre Servidores** | Clone completo do banco de um servidor para outro pelo painel (contas sem novo QR, webhooks, gravações pendentes), com bloqueio da origem e confirmação em duas fases — as mesmas chaves nunca rodam em dois lugares. |
| 💾 **Persistência Inteligente** | Contas deslogadas são mantidas salvas no banco de dados SQLite (`logged_out`), preservando IDs e webhooks. |
| 📊 **Telemetria da VPS sob Demanda** | Modal com RAM, CPU, uptime, goroutines, disco e chamadas ativas — polling só enquanto aberto, zero overhead em background. |
| 📖 **Guia da API Integrado** | Documentação interativa embutida na própria interface com exemplos de cURL, Webhooks e Server-Sent Events (SSE). |
| 🔄 **Máquina de Estados Precisa** | Status claros e confiáveis: *Ligando...* (`starting`) ➔ *Chamando...* (`ringing` ao tocar) ➔ *Em chamada* (`active`). |
| ☁️ **Windows + Cloudflare Tunnel** | Painel acessível pela internet a partir de um Windows comum, sem abrir portas nem configurar roteador: `cloudflared` como serviço + áudio por WebSocket. |
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
| `-static` | `client/dist` | Pasta com os arquivos estáticos do frontend compilado (sem a flag, procura `client/dist` ao lado do executável e depois no diretório atual) |
| `-debug` | `false` | Habilita logs detalhados do WhatsApp e WebRTC |
| `-max-calls` | `0` | Limite de chamadas simultâneas por conta (`0` = sem limite) |
| `-apikey` | `""` | Define a Chave de Super-Usuário (sobrescreve a env `API_KEY`) |
| `-recordings-dir` | `recordings` | Pasta para os WAVs de gravação ainda não enviados ao B2 |
| `-recording-workers` | `3` | Workers concorrentes de upload das gravações para o B2 |
| `-pprof` | `""` | Se definido, serve `net/http/pprof` nesse endereço (use `127.0.0.1:6060` — **nunca exponha publicamente**) |
| `-hash-password` | — | Lê uma senha (sem eco no terminal), imprime o hash bcrypt para `ADMIN_PASSWORD_HASH` e sai |

**Variáveis de ambiente relevantes:** `API_KEY` (chave mestra),
`ADMIN_USER` / `ADMIN_PASSWORD_HASH` (login do painel — ver [Autenticação](#seguranca)),
`WA_PRESENCE_INTERVAL` (intervalo de keepalive do worker, ex: `24h`, `1h`, padrão: `24h`),
`WA_PRESENCE_INTERVAL_HOURS` (alternativa numérica para intervalo em horas),
`WA_VERSION_CHECK_HOUR` (hora local, 0–23, da checagem diária da versão do cliente WhatsApp; se mudou, reconecta as contas abertas sem ligação ativa; padrão: `3`),
`RECORDING_CONFIG_KEY` (AES-256-GCM para cifrar credenciais de gravação no
`wacalls.db` — sem ela, segredos em texto puro + warning), `GOGC` (padrão `200` na
imagem Docker), `GOMEMLIMIT` (recomendado em produção, ~75% da RAM do container),
`AUDIO_PREBUFFER_MS_WEBRTC` / `AUDIO_PREBUFFER_MS_WEBSOCKET` (pré-buffer da reprodução
por transporte, 0–500 ms, padrões `0` e `60` — ver [Pré-buffer](#prebuffer)).

### 🪟 Windows Server (binário nativo)

O mesmo código compila para Windows, sem Docker nem WSL.

1. Baixe na [página de Releases](https://github.com/nathanarend/ArendCalls/releases) o
   `wacalls-server-windows-amd64.exe` (renomeie para `arendcalls.exe`) e o
   `wacalls-client-<versão>.tar.gz`, ou compile a partir do código:
   ```bash
   GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o arendcalls.exe ./cmd/server
   ```
2. Monte a pasta `C:\arendcalls` com o binário e o frontend (o `tar` já vem no Windows Server 2019+):
   ```powershell
   mkdir C:\arendcalls\client\dist
   tar -xzf wacalls-client-<versão>.tar.gz -C C:\arendcalls\client\dist
   ```
3. Libere o firewall (PowerShell como administrador). A faixa UDP é a mídia WebRTC das chamadas:
   ```powershell
   New-NetFirewallRule -DisplayName "ArendCalls HTTP" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow
   New-NetFirewallRule -DisplayName "ArendCalls WebRTC" -Direction Inbound -Protocol UDP -LocalPort 50000-50100 -Action Allow
   ```
4. Instale como serviço com o [NSSM](https://nssm.cc/). O NSSM envia Ctrl+C ao parar o
   serviço, então o servidor encerra as chamadas e o banco de forma limpa:
   ```powershell
   nssm install ArendCalls C:\arendcalls\arendcalls.exe -addr :8080 -static C:\arendcalls\client\dist -db C:\arendcalls\wacalls.db -recordings-dir C:\arendcalls\recordings
   nssm set ArendCalls AppDirectory C:\arendcalls
   nssm set ArendCalls AppEnvironmentExtra API_KEY=sua_chave_mestra GOGC=200
   nssm start ArendCalls
   ```
5. **Login do painel** (obrigatório se o painel ficar acessível pela internet). Gere o
   hash da senha e passe-o ao serviço junto com as outras variáveis — o
   `AppEnvironmentExtra` substitui a lista inteira a cada chamada:
   ```powershell
   C:\arendcalls\arendcalls.exe -hash-password
   # Senha: ********   →  imprime algo como $2a$12$N5ay...
   nssm set ArendCalls AppEnvironmentExtra API_KEY=sua_chave_mestra GOGC=200 ADMIN_USER=admin 'ADMIN_PASSWORD_HASH=$2a$12$N5ay...'
   nssm restart ArendCalls
   ```
   Use **aspas simples** no PowerShell: com aspas duplas, os `$` do hash viram variáveis.

Para um teste rápido, basta dar duplo clique no `arendcalls.exe`: ele encontra o
`client\dist` ao lado do executável e sobe em `http://localhost:8080`.

Para HTTPS, use o [Cloudflare Tunnel](#cloudflare-tunnel) (abaixo) ou um proxy reverso na frente (IIS com ARR, Caddy ou Traefik). No
painel de métricas, o *load average* aparece zerado: o Windows não tem essa métrica.

<a id="cloudflare-tunnel"></a>
### ☁️ Acesso pela internet com Cloudflare Tunnel (Windows, o jeito mais fácil)

O `cloudflared` cria uma conexão **de saída** do Windows até a Cloudflare e publica o
painel num endereço `https://` seu (ex.: `https://calls.seudominio.com`). Não precisa de
IP fixo, redirecionamento de porta no roteador nem certificado. Também funciona num PC
atrás de NAT/CGNAT.

**Pré-requisito:** um domínio adicionado à sua conta Cloudflare (plano gratuito serve).

1. **Suba o ArendCalls** (duplo clique no `arendcalls.exe` ou serviço NSSM acima) e
   confirme que o painel abre em `http://localhost:8080`. Com o tunnel, prefira
   `-addr 127.0.0.1:8080` (no `nssm install`, no lugar de `-addr :8080`): o painel deixa
   de ser acessível direto pela rede, só pelo tunnel.
2. **Instale o `cloudflared`** (PowerShell):
   ```powershell
   winget install --id Cloudflare.cloudflared
   ```
   Sem winget: baixe o `cloudflared-windows-amd64.msi` nos
   [releases do cloudflared](https://github.com/cloudflare/cloudflared/releases/latest).
3. **Crie o tunnel** no painel da Cloudflare: **Networking → Tunnels → Create a tunnel**,
   dê um nome (ex.: `arendcalls`) e escolha **Windows**. Copie o
   comando que aparece (`cloudflared.exe service install <token>`) e rode num
   **PowerShell como administrador**. Ele instala o tunnel como serviço do Windows, que
   sobe sozinho com o computador.
4. **Publique o painel:** no tunnel, aba **Routes → Add route → Published application**.
   Escolha o subdomínio e o domínio, e em **Service URL** informe `http://localhost:8080`.
5. **Ligue o login do painel** (passo 5 do serviço NSSM acima). O painel passa a estar na
   internet: sem `ADMIN_PASSWORD_HASH`, qualquer pessoa com o endereço tem acesso total.
6. **Troque o áudio para WebSocket** em cada navegador que for atender: **Configurações
   do Painel → Transporte de áudio → WebSocket**. O tunnel não transporta UDP, então o
   WebRTC não conecta por ele.

Pronto: abra `https://calls.seudominio.com`. Com o tunnel, as regras de firewall do
passo 3 do serviço deixam de ser necessárias (TCP 8080 e UDP 50000-50100).

O bloqueio de tentativas de login usa o IP do cabeçalho `CF-Connecting-IP`. Com
`-addr 127.0.0.1:8080` só o `cloudflared` alcança o ArendCalls, então esse cabeçalho só
pode vir da Cloudflare. Escutando em todas as interfaces (`:8080`), alguém na rede local
pode forjá-lo.

> [!WARNING]
> **Não use o tunnel rápido** (`cloudflared tunnel --url http://localhost:8080`, endereço
> `*.trycloudflare.com`). Ele [não suporta Server-Sent Events](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/do-more-with-tunnels/trycloudflare/),
> e o painel depende deles para mostrar QR code, status das contas e chamadas recebidas
> em tempo real. O endereço também muda a cada reinício.

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
| `POST` | `/api/sessions/{sid}/presence` | Forçar envio de presença (`available` / `unavailable`) para keepalive |
| `POST` | `/api/sessions/{sid}/logout` | Desconectar sessão (mantém no banco para re-parear) |
| `POST` | `/api/sessions/{sid}/start` · `/restart` · `/stop` | Controlar o ciclo de vida da conexão da instância |
| `POST` | `/api/sessions/{sid}/check-number` | Verificar se um número tem WhatsApp |

### Chamadas

| Método | Rota | Finalidade |
|---|---|---|
| `POST` | `/api/sessions/{sid}/calls` | Iniciar chamada de voz — `{ phone, record?, clinicId?, duration_ms? }` |
| `POST` | `/api/sessions/{sid}/calls/{id}/webrtc` | Trocar SDP do WebRTC para streaming de áudio |
| `POST` | `/api/sessions/{sid}/calls/{id}/audio-ticket` | Ticket de uso único (30 s) para o navegador abrir o WebSocket de áudio sem a API key — ver [Transporte de áudio](#transporte-audio) |
| `GET` | `/api/sessions/{sid}/calls/{id}/audio` | WebSocket de áudio, alternativa ao `/webrtc` que funciona atrás de proxy/tunnel |
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

### Migração entre Servidores

| Método | Rota | Finalidade |
|---|---|---|
| `GET` | `/api/migration/status` | Estado da migração nesta instância (papel, fase, progresso, registro) |
| `POST` | `/api/migration/import` | **No destino:** puxa o clone da origem — `{ sourceUrl, sourceApiKey, allowInsecure? }` |
| `POST` | `/api/migration/source/lock` | **Na origem** (chamada pelo destino): desconecta tudo e gera o snapshot |
| `GET` | `/api/migration/source/{mid}/files/{name}` | **Na origem:** download do snapshot / WAVs (aceita `Range`) |
| `POST` | `/api/migration/source/{mid}/commit` · `/abort` | **Na origem:** confirmar (apaga as chaves locais) ou desbloquear |

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
descarta gravações < 5 s (ligação não atendida não grava nada: `skipped` com
`error = "not answered"`). Uma **fila durável** (`call_recordings` no SQLite) drenada
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

<a id="transporte-audio"></a>
## 🎧 Transporte de Áudio: WebRTC ou WebSocket

O áudio entre navegador e servidor é PCM bruto (**Int16 LE, 16 kHz, mono**) nos dois
sentidos. O cliente escolhe o transporte ao conectar o áudio de cada chamada; o
servidor oferece os dois sempre, e nada no resto da API muda.

| | WebRTC (`POST .../webrtc`) | WebSocket (`GET .../audio`) |
|---|---|---|
| Canal | DataChannel `pcm` | Uma mensagem binária por quadro |
| Rede | UDP direto até o servidor (50000-50100) | HTTP/WebSocket: passa por proxy e Cloudflare Tunnel |
| Quando usar | Servidor com IP público | Servidor atrás de NAT/tunnel, sem UDP de entrada |

O painel usa WebRTC por padrão; a escolha fica em **Configurações do Painel →
Transporte de áudio** e vale para aquele navegador.

**Autenticação do WebSocket**, nesta ordem:
1. `?ticket=` — para clientes de outra origem (um CRM). O backend do cliente chama
   `POST /api/sessions/{sid}/calls/{id}/audio-ticket` com a API key e recebe
   `{ "ticket": "<64 hex>", "expiresIn": 30 }`; o navegador conecta em
   `wss://.../api/sessions/{sid}/calls/{id}/audio?ticket=...`. O ticket vale 30 s,
   uma vez só, e só para aquela chamada. A API key nunca vai para o navegador.
2. Cookie do painel — exige `Origin` igual ao host (proteção contra CSWSH).
3. API key (`X-Api-Key`, `Authorization: Bearer` ou `?apikey=`).

**Fechamento e queda.** O servidor fecha com `1000 call_ended` quando a chamada
termina e com `1000 replaced` quando outra conexão de áudio assume a chamada (ex.:
retomada depois da espera). Qualquer outra queda é anormal: o servidor espera
**15 s** por uma nova conexão (com ticket novo) antes de encerrar a chamada, e com a
chamada em espera não encerra. O servidor manda ping a cada 20 s, porque o
Cloudflare derruba WebSocket ocioso depois de ~100 s.

**Mensagens do WebSocket.** Binário = um quadro de áudio. A primeira mensagem do
servidor, antes de qualquer áudio, é texto: `{"type":"config","prebufferMs":60}`.
Clientes que não usam pré-buffer podem ignorar mensagens de texto.

<a id="prebuffer"></a>
**Pré-buffer da reprodução.** Quando o contato pausa a fala, o WhatsApp para de mandar
quadros (DTX) e o buffer do navegador esvazia. Na volta, o primeiro quadro tocaria na
hora, e qualquer atraso do seguinte vira um picote na primeira sílaba. Com pré-buffer,
o cliente segura o início de cada fala por N ms antes de tocar: N ms a mais de latência
só no começo da fala, em troca de absorver o jitter. O valor é por transporte, definido
no servidor e entregue ao cliente (no WebRTC, em `prebuffer_ms` da resposta do
`/webrtc`; no WebSocket, na mensagem `config`):

| Variável | Padrão | |
|---|---|---|
| `AUDIO_PREBUFFER_MS_WEBRTC` | `0` | Desligado: UDP tem pouco jitter |
| `AUDIO_PREBUFFER_MS_WEBSOCKET` | `60` | TCP/tunnel: um pacote atrasado segura os seguintes |

Aceita 0–500; valor inválido usa o padrão (com aviso no log). O valor em uso aparece no
log de início (`audio prebuffer webrtc_ms=… websocket_ms=…`) e no console do navegador
(`[arendcalls] prebuffer=…ms`). Para trocar, reinicie o servidor; vale a partir da
próxima ligação. Se ouvir picote no começo das falas, aumente; se a conversa parecer
"atrasada", diminua.

**Restrições:**
- WebSocket é TCP: um pacote perdido segura os seguintes até ser retransmitido. Em
  rede ruim isso aparece como atraso pontual em vez de picote. É a troca aceita para
  funcionar atrás do tunnel.
- A API de WebSocket do navegador não envia o header `Authorization`. Se houver
  **Basic Auth no proxy** (ex.: Traefik), a rota `/api/sessions/*/calls/*/audio`
  precisa ficar fora dele para clientes de outra origem; o ticket é a autenticação.
  O painel não é afetado (mesma origem, o cookie vai no handshake).

---

<a id="migracao"></a>
## 🚚 Migração entre Servidores

Move tudo de um servidor ArendCalls (**origem**) para outro (**destino**): contas com as chaves do WhatsApp (sem escanear QR de novo), nomes, IDs, webhooks, configuração de gravação e WAVs ainda não entregues ao B2. No painel do **destino**, clique em ⇄ (barra lateral) e informe a URL e a `API_KEY` da origem.

O protocolo garante que as mesmas chaves nunca ficam ativas nos dois lados:

1. **Bloqueio** — a origem encerra as conexões (sem logout, o aparelho continua vinculado), pausa as gravações e gera um snapshot consistente (`VACUUM INTO`). O bloqueio é persistido: a origem continua parada mesmo se reiniciar.
2. **Download conferido** — banco e WAVs com sha256 e tamanho exatos; downloads interrompidos retomam via `Range`.
3. **Verificação** — `integrity_check`, `foreign_key_check`, contagem de linhas igual à da origem e schema atualizado para a versão do destino.
4. **Import atômico** — uma transação troca todas as tabelas e confere cada uma (linhas + sha256 do conteúdo) antes do commit. Antes disso, o destino guarda um backup de si mesmo.
5. **Conexão** — o destino conecta as contas. Se **nenhuma** conectar em 90 s, ele volta ao backup e só então desbloqueia a origem.
6. **Confirmação** — o destino avisa a origem, que apaga as próprias chaves e fica desativada (banner no painel, rotas de escrita respondem `423`). Sem resposta, o destino repete até conseguir; até lá a origem segue bloqueada.

Requisitos e observações:

- O destino precisa estar **vazio** (sem contas pareadas) e na **mesma versão** da origem ou mais nova. Nenhuma chamada pode estar em andamento na origem.
- Use **https**: o snapshot carrega as chaves das contas. `http` só com `allowInsecure` (rede privada/VPN).
- Variáveis de ambiente **não** migram (`API_KEY`, `ADMIN_PASSWORD_HASH`, `RECORDING_CONFIG_KEY`). Os segredos de gravação são re-cifrados com a `RECORDING_CONFIG_KEY` do destino.
- Se o destino não conseguir falar com a origem no fim, o painel da origem oferece **Desbloquear** (o destino não usou as chaves) e **Finalizar manualmente** (o destino está operando). Cada botão explica quando é seguro usá-lo.
- Uma conta que aparecer como **"Em uso em outro servidor"** recebeu `stream replaced`: outro processo conectou com as mesmas chaves. Ela não reconecta sozinha; mantenha só uma instância ativa.

---

<a id="seguranca"></a>
## 🔒 Segurança e Autenticação

### Login do painel web
Defina `ADMIN_PASSWORD_HASH` (hash bcrypt gerado com `arendcalls -hash-password`) e,
opcionalmente, `ADMIN_USER` (padrão `admin`). Com o hash definido:
- o painel pede usuário e senha; a sessão vale 7 dias (cookie `HttpOnly`, `SameSite=Strict`, `Secure` sob HTTPS) e é perdida ao reiniciar o servidor;
- após 5 senhas erradas, o IP fica bloqueado por 1 min, dobrando a cada novo erro até 15 min;
- a `API_KEY` segue valendo para integrações.

**Sem `ADMIN_PASSWORD_HASH`, o painel fica aberto** (comportamento antigo: o cookie de
admin é entregue a quem abrir a página) e o servidor avisa no log. Só use assim atrás
de um proxy com autenticação própria (ex: Basic Auth no Traefik).

No Docker Compose, escreva cada `$` do hash como `$$`:
```yaml
      - ADMIN_PASSWORD_HASH=$$2a$$12$$N5ay...
```

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

O **ArendCalls** é desenvolvido como um fork aprimorado do projeto de código aberto [WaCalls](https://github.com/jotadev66/wacalls), criado por:

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

