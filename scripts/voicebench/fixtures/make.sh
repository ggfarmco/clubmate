#!/bin/sh
set -eu
dir=$(cd "$(dirname "$0")" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
say -v Milena -o "$tmp/vb.aiff" "Запусти агента на ветке фича войс"
ffmpeg -loglevel error -y -i "$tmp/vb.aiff" -ac 1 -ar 16000 "$dir/ru.wav"
echo "$dir/ru.wav"
