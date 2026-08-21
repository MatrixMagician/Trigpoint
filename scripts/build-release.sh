#!/usr/bin/env bash
# Builds the release artifacts: one static trig per supported platform, each in
# its own tarball, with a SHA256SUMS file over the lot. The release workflow
# runs this and uploads what it leaves in dist/; running it by hand produces the
# same files, which is the point — a release nobody can reproduce locally is a
# release nobody can check.
#
#   usage: scripts/build-release.sh [version] [outdir]
#
# version defaults to the tag at HEAD, or to the short commit for an untagged
# build. Linux only, and deliberately: see docs/adr/0019-v1-ships-linux-only.md.
set -euo pipefail

cd "$(dirname "$0")/.."

version=${1:-$(git describe --tags --exact-match 2>/dev/null || git rev-parse --short HEAD)}
outdir=${2:-dist}

# CGO off is what makes the binary static: with it on, net and os/user link
# against the system libc and the binary stops running on a machine whose libc
# is older than the builder's. trimpath keeps the builder's home directory out
# of the panic traces.
export CGO_ENABLED=0

rm -rf "$outdir"
mkdir -p "$outdir"

for platform in linux/amd64 linux/arm64; do
	os=${platform%/*}
	arch=${platform#*/}
	stage=$outdir/trig_${version}_${os}_${arch}
	mkdir -p "$stage"

	GOOS=$os GOARCH=$arch go build -trimpath -ldflags '-s -w' -o "$stage/trig" ./cmd/trig
	cp README.md LICENSE "$stage/"
	tar -czf "$stage.tar.gz" -C "$outdir" "$(basename "$stage")"
	rm -rf "$stage"
	echo "built $stage.tar.gz"
done

(cd "$outdir" && sha256sum ./*.tar.gz > SHA256SUMS)
echo "wrote $outdir/SHA256SUMS"
