# --- Stage 1: Build Frontend (Node) ---
FROM node:20-alpine AS builder-frontend
WORKDIR /app/client

# Copiar os arquivos de dependência primeiro para aproveitar cache
COPY client/package*.json ./
RUN npm install

# Copiar o resto dos arquivos do client e compilar
COPY client/ ./
RUN npm run build

# --- Stage 2: Build Backend (Go) ---
FROM golang:alpine AS builder-backend
WORKDIR /app

# Instalar GCC e dependências necessárias para compilar CGO (SQLite)
RUN apk add --no-cache gcc musl-dev build-base

# Copiar os arquivos de dependência Go
COPY go.mod go.sum ./
RUN go mod download

# Copiar o restante do código fonte
COPY . .

# Compilar o binário
RUN CGO_ENABLED=1 GOOS=linux go build -o arendcalls ./cmd/server

# --- Stage 3: Imagem Final (Alpine) ---
FROM alpine:latest
WORKDIR /app

# Instalar dependências de sistema mínimas (CA certificates e tzdata)
RUN apk --no-cache add ca-certificates tzdata

# Criar o diretório de dados que será mapeado pelo Docker Volume
RUN mkdir -p /app/data

# Copiar o binário compilado do Stage 2
COPY --from=builder-backend /app/arendcalls /app/arendcalls

# Copiar os arquivos estáticos do frontend compilados
COPY --from=builder-frontend /app/client/dist /app/client/dist

# Expor a porta principal da API (8080)
EXPOSE 8080

# (Opcional, informativo) Expor as portas UDP do WebRTC
EXPOSE 50000-50100/udp

# O volume onde o banco SQLite vai persistir
VOLUME ["/app/data"]

# Opcional: Variável de ambiente padrão
ENV API_KEY=""

# Iniciar o servidor apontando os estáticos para a pasta correta e o banco para o diretório de dados
ENTRYPOINT ["/app/arendcalls", "-addr", "0.0.0.0:8080", "-static", "/app/client/dist", "-db", "/app/data/wacalls.db"]
