#!/bin/sh
# Monta os pacotes "extrair e executar" de uma release a partir dos binarios e
# do client ja compilados.
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
        cp "$art/wacalls-server-${target}.exe" "$dir/arendcalls.exe"
        # cmd.exe precisa de CRLF.
        sed 's/$/\r/' "$pkg/windows/iniciar.bat" > "$dir/iniciar.bat"
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
