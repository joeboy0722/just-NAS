#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o nas-server-linux ./cmd/nas-server
docker build -t just-nas-builder -f Dockerfile.iso-builder .
mkdir -p output
docker run --rm -v just-nas-iso-cache:/iso-cache -v "$PWD/output:/builder/output" -v "$PWD/scripts:/builder/scripts:ro" -v "$PWD/nas-server-linux:/builder/nas-server-linux:ro" just-nas-builder sh /builder/scripts/make-custom-iso.sh