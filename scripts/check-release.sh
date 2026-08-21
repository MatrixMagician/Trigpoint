#!/usr/bin/env bash
# Runs a release artifact the way someone who downloaded it would: on a machine
# with no Go toolchain, from the tarball, with tmux installed and nothing else.
# It is the check behind "a downloaded binary works", rather than a claim that
# it does.
#
#   usage: scripts/check-release.sh [dist-dir] [linux/amd64|linux/arm64]
#
# dist-dir may be relative to the repo root or absolute.
#
# Needs docker or podman. A foreign architecture needs binfmt_misc registered
# for it (qemu-user-static); without that the run cannot happen and the script
# says so rather than passing quietly.
set -euo pipefail

cd "$(dirname "$0")/.."

# Resolved here, so that a relative dist is relative to the repo root and an
# absolute one is left alone. Pasting $PWD onto an absolute path is how this
# used to fail, and it failed inside docker rather than here.
dist=$(cd "${1:-dist}" && pwd)
platform=${2:-linux/amd64}
tarball=$(ls "$dist"/*"${platform#*/}".tar.gz)

docker run --rm --platform "$platform" -v "$dist:/dist:ro,z" debian:bookworm-slim sh -c '
	set -e
	if command -v go >/dev/null; then echo "this image has a Go toolchain; the check is worthless" >&2; exit 1; fi
	apt-get -qq update >/dev/null && apt-get -qq install -y tmux >/dev/null
	mkdir /unpacked && tar xzf "/dist/'"$(basename "$tarball")"'" -C /unpacked --strip-components=1
	/unpacked/trig doctor
'
echo "ok: $(basename "$tarball") runs trig doctor on a clean $platform machine"
