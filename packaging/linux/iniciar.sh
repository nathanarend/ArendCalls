#!/bin/sh
set -e
cd "$(dirname "$0")"

if [ ! -f .env ]; then
    cp .env.example .env
    echo "Arquivo .env criado. Preencha API_KEY e o login do painel e rode ./iniciar.sh de novo."
    exit 0
fi

# Leitura literal (sem source): o hash bcrypt tem "$" e seria expandido pelo shell.
while IFS= read -r line || [ -n "$line" ]; do
    line=$(printf '%s' "$line" | tr -d '\r')
    case "$line" in
        '' | '#'*) continue ;;
        *=*) export "${line%%=*}=${line#*=}" ;;
    esac
done < .env
: "${ADDR:=127.0.0.1:8080}"

if [ "$API_KEY" = "troque_esta_chave" ]; then
    echo "ERRO: troque o API_KEY no arquivo .env antes de iniciar." >&2
    exit 1
fi

echo "Iniciando ArendCalls em $ADDR ..."
exec ./arendcalls -addr "$ADDR" "$@"
