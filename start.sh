#!/bin/bash
set -e

# Garante que o Go instalado localmente está no PATH
export PATH=$PATH:$HOME/.local/go/bin

cd "$(dirname "$0")"

BIN=arendcalls_bin

# 1. Build do frontend (React) -> client/dist
echo "==> build do client (vite)"
( cd client && npm run build )

# 2. Build do servidor: binário estático, sem CGO, enxuto (mesmas flags do Dockerfile)
echo "==> build do server (go)"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BIN" ./cmd/server

# 3. Sobe o binário com o tuning de runtime de produção.
#    GOGC=200: deixa o heap crescer ~3x antes de coletar — corta CPU gasto em GC
#    no hot path de áudio (o default 100 custa CPU mensurável sob carga de call
#    e causa engasgo no áudio recebido). Igual ao ENV do Dockerfile.
echo "==> iniciando o servidor ArendCalls em :8080"
exec env GOGC=200 "./$BIN" -static client/dist -addr :8080 -db wacalls.db "$@"
