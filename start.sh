#!/bin/bash

# Garante que o Go instalado localmente está no PATH
export PATH=$PATH:$HOME/.local/go/bin

echo "Iniciando o servidor WaCalls..."
# Executa o servidor apontando para o build do React que (espera-se) já foi gerado
go run ./cmd/server -static client/dist -addr :8080
