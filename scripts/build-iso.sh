#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ "${NAS_SOURCE_BOOTSTRAP:-0}" != 1 ] && [ ! -f output/compliance/iso/READY ]; then
 echo 'Missing third-party license collection. Extract the matching Release licenses archive into output/compliance; see COMPLIANCE.md.' >&2
 exit 1
fi
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o nas-server-linux ./cmd/nas-server
docker build -t just-nas-builder -f Dockerfile.iso-builder .
mkdir -p output
docker run --rm -e NAS_SOURCE_BOOTSTRAP -v just-nas-iso-cache:/iso-cache -v "$PWD/output:/builder/output" -v "$PWD/output/compliance/iso:/builder/compliance:ro" -v "$PWD/scripts:/builder/scripts:ro" -v "$PWD/nas-server-linux:/builder/nas-server-linux:ro" just-nas-builder sh /builder/scripts/make-custom-iso.sh
