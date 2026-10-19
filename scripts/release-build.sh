#!/usr/bin/env bash
# Builds the release binaries and SHA256SUMS into OUTDIR. The release workflow
# runs this; run it yourself to reproduce a release byte for byte:
#
#   scripts/release-build.sh 0.2.0 <full commit SHA of v0.2.0> dist
#
# Needs any Go >= 1.21 on PATH: GOTOOLCHAIN makes it fetch and use GO_VERSION.
set -euo pipefail

# The exact Go release that builds release binaries. Raise it deliberately;
# it is part of what makes a release reproducible.
GO_VERSION=go1.27.2
TARGETS="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"

usage="usage: $0 VERSION REVISION OUTDIR"
version=${1:?$usage}
revision=${2:?$usage}
out=${3:?$usage}

cd "$(dirname "$0")/.."
export GOTOOLCHAIN=$GO_VERSION CGO_ENABLED=0
if [ "$(go env GOVERSION)" != "$GO_VERSION" ]; then
  echo "expected $GO_VERSION, got $(go env GOVERSION)" >&2
  exit 1
fi

mkdir -p "$out"
for target in $TARGETS; do
  os=${target%/*}
  arch=${target#*/}
  # -buildvcs=false: the revision is passed explicitly, so the binary doesn't
  # depend on the checkout's state (e.g. untracked files marking it modified).
  GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=$version -X main.revision=$revision" \
    -o "$out/registries-conf_${version}_${os}_${arch}" ./cmd/registries-conf
done

cd "$out"
if command -v sha256sum >/dev/null; then
  sha256sum registries-conf_* > SHA256SUMS
else
  shasum -a 256 registries-conf_* > SHA256SUMS # macOS
fi
cat SHA256SUMS
