#!/bin/sh
# Monta os pacotes "extrair e executar" de uma release a partir dos binarios e
# do client ja compilados. No Windows o pacote leva o lancador ArendCalls.exe
# (servico + icone da bandeja, ver windows/build.sh), que precisa estar em
# <dir dos artefatos>/arendcalls-launcher-windows-amd64.exe.
#   packaging/bundle.sh <tag> <dir dos artefatos> <dir de saida>
# Usado pelo workflow de release; roda igual localmente.
set -eu

tag=$1
art=$(cd "$2" && pwd)
out=$(mkdir -p "$3" && cd "$3" && pwd)
pkg=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for target in windows-amd64 linux-amd64 linux-arm64; do
    name="arendcalls-${tag}-${target}"
    dir="$work/$name"
    mkdir -p "$dir/client/dist"
    tar -xzf "$art/wacalls-client-${tag}.tar.gz" -C "$dir/client/dist"
    case $target in
    windows-*)
        # O servidor nao pode se chamar arendcalls.exe: o Windows ignora
        # maiusculas e ele colidiria com o lancador ArendCalls.exe.
        cp "$art/wacalls-server-${target}.exe" "$dir/arendcalls-server.exe"
        cp "$art/arendcalls-launcher-${target}.exe" "$dir/ArendCalls.exe"
        # cmd.exe e o Bloco de Notas precisam de CRLF.
        sed 's/$/\r/' "$pkg/windows/desinstalar.bat" > "$dir/desinstalar.bat"
        sed "s/{{VERSAO}}/$tag/; s/\$/\r/" "$pkg/windows/LEIA-ME.txt" > "$dir/LEIA-ME.txt"
        sed 's/$/\r/' "$pkg/env.example" > "$dir/.env.example"
        rm -f "$out/$name.zip"
        (cd "$work" && zip -qr "$out/$name.zip" "$name")
        ;;
    *)
        install -m 755 "$art/wacalls-server-${target}" "$dir/arendcalls"
        install -m 755 "$pkg/linux/iniciar.sh" "$dir/iniciar.sh"
        cp "$pkg/env.example" "$dir/.env.example"
        tar -czf "$out/$name.tar.gz" -C "$work" "$name"
        ;;
    esac
done
