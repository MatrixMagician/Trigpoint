#!/usr/bin/env bash
# Records docs/demo/trigpoint.gif. vhs drives a real terminal, and the tape
# drives a real trig against a real tmux, so the demo cannot drift from what the
# program does — re-record it and see.
#
#   usage: docs/demo/record.sh
#
# Needs docker or podman. Everything else (vhs, ffmpeg, tmux, the trig under
# test) is built into the image from Dockerfile.
set -euo pipefail

cd "$(dirname "$0")/../.."

CGO_ENABLED=0 go build -trimpath -o docs/demo/trig ./cmd/trig
trap 'rm -f docs/demo/trig' EXIT

docker build -t trig-vhs docs/demo
docker run --rm -v "$PWD/docs/demo:/vhs:z" trig-vhs demo.tape
echo "wrote docs/demo/trigpoint.gif"
