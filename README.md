<div align="center">

# 📞 ArendCalls

**Chamadas de voz nativas do WhatsApp em puro Go e React 19, direto do seu navegador.**
*Fork corporativo de alta performance baseado no [WaCalls](https://github.com/jobasfernandes/wacalls)*

[![Docker](https://img.shields.io/badge/DockerHub-nathanarend%2Farendcalls-blue?logo=docker&logoColor=white)](https://hub.docker.com/r/nathanarend/arendcalls)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![whatsmeow](https://img.shields.io/badge/whatsmeow-VoIP-25D366?logo=whatsapp&logoColor=white)](https://github.com/tulir/whatsmeow)
[![pion](https://img.shields.io/badge/pion-WebRTC-FF6B6B)](https://github.com/pion/webrtc)
[![Licença](https://img.shields.io/badge/licen%C3%A7a-MIT-green.svg)](#-licença)

[Diferenciais](#-principais-recursos-e-diferenciais) · [Início Rápido (Docker)](#-início-rápido-com-docker-recomendado) · [Como Funciona](#-como-funciona-o-fluxo-de-chamada) · [Arquitetura](#-arquitetura) · [Rotas da API](#-endpoints-da-api) · [Autenticação](#-segurança-e-autenticação)

</div>

---

## 💡 Sobre o Projeto

O **ArendCalls** permite conectar uma ou mais contas de WhatsApp via **QR code** e realizar e receber **chamadas de voz 1:1** diretamente de qualquer navegador na rede.

O áudio do microfone do navegador é transmitido como **PCM 16 kHz bruto via canal de dados WebRTC** até o servidor Go. O servidor codifica o áudio utilizando o codec **MLow** da Meta e o injeta na malha de **relays SRTP do WhatsApp**. No fluxo inverso, o áudio do interlocutor é decodificado e reproduzido em tempo real no navegador.

Todo o ecossistema VoIP roda **nativamente em puro Go**:
- Codec de voz MLow embutido
- Empacotamento RTP/SRTP e STUN
- Transporte de relay WebRTC/SCTP
- Sinalização `<call>` integrada ao [**whatsmeow**](https://github.com/tulir/whatsmeow)
- Cliente moderno construído em **React 19 + Tailwind CSS**
- **Sem necessidade de CGO, compiladores C ou DLLs externas**

---

## 🚀 Principais Recursos e Diferenciais

Este repositório (`ArendCalls`) traz diversas melhorias de engenharia e usabilidade em relação ao projeto original:

| Recurso | Detalhes |
|---|---|
| 🇧🇷 **Interface 100% em Português** | Telas, modais, mensagens de erro, alertas e documentação totalmente traduzidos (pt-BR). |
| ⏸️ **Modo Espera (Hold) Integrado** | Botão no painel de chamada para colocar o cliente em espera tocando música suave sem encerrar a ligação. |
| 🔊 **Motor de Áudio Otimizado** | Pausa de 3 segundos entre repetições de áudio de espera e correção de concorrência no fluxo RTP (sem picotes). |
| 🛡️ **Autenticação Unificada** | Chave mestra global (`API_KEY`) para segurança nas integrações de backend e cookies de sessão para o painel web. |
| 🏢 **Gerenciador Visual de Instâncias** | Criar, renomear e gerenciar múltiplas conexões de WhatsApp diretamente pela barra lateral. |
| 💾 **Persistência Inteligente** | Contas deslogadas são mantidas salvas no banco de dados SQLite (`logged_out`), preservando IDs e webhooks. |
| 📖 **Guia da API Integrado** | Documentação interativa embutida na própria interface com exemplos de cURL, Webhooks e Server-Sent Events (SSE). |
| 🔄 **Máquina de Estados Precisa** | Status claros e confiáveis: *Ligando...* (`starting`) ➔ *Chamando...* (`ringing` ao tocar) ➔ *Em chamada* (`active`). |
| 🐳 **Pronto para Produção (Docker)** | Imagem oficial e leve no DockerHub (`nathanarend/arendcalls:latest`) com suporte nativo a Traefik. |

---

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
    volumes:
      - arendcalls_data:/app/data
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
```

### 2. Suba o container

```bash
docker compose up -d
```

Acesse o endereço configurado (ou `http://localhost:8080`), clique em **Nova Conexão** e leia o QR Code com o seu WhatsApp (**Aparelhos Conectados**)!

---

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

---

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
│  internal/wa      Adaptador VoipSocket sobre whatsmeow                     │
│  internal/voip    CallManager · Sinalização · Codec MLow · Transporte SRTP │
└───────────────┬──────────────────────────────────────┬────────────────────┘
                │ Sinalização E2E (<call>)             │ Mídia SRTP / MLow
                ▼                                      ▼
        ┌───────────────┐                    ┌──────────────────────┐
        │  WhatsApp WS  │                    │   WhatsApp Relays    │
        │  (whatsmeow)  │                    │  (SRTP over SCTP/DC) │
        └───────────────┘                    └──────────────────────┘
```

---

## 📡 Endpoints da API

Todas as rotas são isoladas por identificador de sessão (`{sid}`):

| Método | Rota | Finalidade |
|---|---|---|
| `GET` | `/api/sessions` | Listar todas as instâncias cadastradas |
| `POST` | `/api/sessions` | Criar nova instância e iniciar pareamento QR |
| `PATCH` | `/api/sessions/{sid}` | Renomear o nome de identificação da conta |
| `PATCH` | `/api/sessions/{sid}/webhook` | Configurar URL de webhook para eventos |
| `DELETE` | `/api/sessions/{sid}` | Excluir e desvincular a instância |
| `POST` | `/api/sessions/{sid}/logout` | Desconectar sessão (mantém no banco para re-parear) |
| `POST` | `/api/sessions/{sid}/pair` | Gerar novo QR Code para uma conta desconectada |
| `POST` | `/api/sessions/{sid}/calls` | Iniciar uma nova chamada de voz (`{ phone }`) |
| `POST` | `/api/sessions/{sid}/calls/{id}/webrtc` | Trocar SDP do WebRTC para streaming de áudio |
| `POST` | `/api/sessions/{sid}/calls/{id}/accept` | Atender chamada recebida |
| `POST` | `/api/sessions/{sid}/calls/{id}/reject` | Rejeitar chamada recebida |
| `POST` | `/api/sessions/{sid}/calls/{id}/hold` | Colocar em Espera / Retomar (`{ hold: true/false }`) |
| `DELETE` | `/api/sessions/{sid}/calls/{id}` | Desligar/Encerrar chamada ativa |
| `GET` | `/api/sessions/{sid}/history` | Histórico das últimas 50 chamadas da instância |
| `GET` | `/api/system/metrics` | Telemetria sob demanda da VPS e ArendCalls (RAM, CPU, Uptime, Disco) |
| `GET` | `/api/events` | Stream global de eventos em tempo real (SSE) |

---

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

> **Aviso de Segurança:** O arquivo `wacalls.db` guarda os tokens e credenciais de sessão do WhatsApp. **Nunca commite este arquivo** em repositórios públicos e mantenha seus backups protegidos.

---

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

## 📄 Licença

Distribuído sob a licença [MIT](./LICENSE).
