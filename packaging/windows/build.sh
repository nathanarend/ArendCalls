#!/bin/sh
# Compila o lançador do Windows (serviço + ícone da bandeja), com ícone e
# manifesto (DPI, visual moderno) embutidos:
#   packaging/windows/build.sh <versão> <saída.exe>
# Usado pelo workflow de release; roda igual localmente (precisa de go).
set -eu

version=$1
mkdir -p "$(dirname "$2")"
out=$(cd "$(dirname "$2")" && pwd)/$(basename "$2")
cd "$(dirname "$0")"
work=$(mktemp -d)
trap 'rm -rf "$work" ./*.syso' EXIT

go run ./cmd/genicon "$work/app.ico"
rm -f ./*.syso
go run github.com/tc-hib/go-winres@v0.3.3 simply \
    --arch amd64 --out rsrc --icon "$work/app.ico" --manifest gui \
    --product-name ArendCalls --file-description ArendCalls \
    --product-version "$version" --file-version "$version" \
    --original-filename ArendCalls.exe
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
    -ldflags "-H windowsgui -s -w -X main.version=$version" \
    -o "$out" .
