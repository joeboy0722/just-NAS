#!/bin/sh
set -eu
export DOCKER_HOST=unix:///run/just-nas-docker.sock DOCKER_BUILDKIT=0
work=${1:?absolute managed run directory}
pool=${2:-vmtest}
case "$pool" in ''|*[!a-zA-Z0-9_.-]*) exit 2;; esac
case "$work" in /mnt/"$pool"/.just-nas-build/runs/*) ;; *) exit 2;; esac
base=/mnt/$pool/.just-nas-build
[ "$(findmnt -n -o SOURCE -T "$work")" = "$pool/.just-nas-build" ]
cd "$work"
mkdir -p output
# Build arguments are bounded even during image preparation; no Docker-in-Docker or privileged containers.
docker build --memory=3g --cpu-period=100000 --cpu-quota=200000 -t just-nas-toolchain:1.25.6 -f Dockerfile.build-toolchain .
docker run --rm --cpus=2 --memory=3g --pids-limit=512 -v "$work:/src:ro" -v "$work/output:/out" -v "$base/go-cache:/root/.cache/go-build" just-nas-toolchain:1.25.6
docker run --rm --cpus=2 --memory=512m -v "$work/output:/out:ro" just-nas-toolchain:1.25.6 go version -m /out/nas-server-linux > output/server-build-info.txt
sha256sum output/nas-server-linux > output/server-binary.sha256
cp output/nas-server-linux nas-server-linux
docker build --memory=3g --cpu-period=100000 --cpu-quota=200000 -t just-nas-builder:nas-host -f Dockerfile.iso-builder .
docker image inspect just-nas-toolchain:1.25.6 just-nas-builder:nas-host > output/builder-images.json
docker run --rm --cpus=2 --memory=3g --pids-limit=512 -e NAS_BUILD_CPUS=2 -e NAS_SQUASHFS_MEMORY=512M -v "$base/cache:/iso-cache" -v "$work/output:/builder/output" -v "$work/output/compliance/iso:/builder/compliance:ro" -v "$work/nas-server-linux:/builder/nas-server-linux:ro" just-nas-builder:nas-host sh /builder/scripts/make-custom-iso.sh
cd output
cp alpine-nas-stores.iso just-NAS-nas-built-candidate-x86_64.iso
sha256sum just-NAS-nas-built-candidate-x86_64.iso > SHA256SUMS
# Only this marker means every preceding check actually succeeded.
printf '%s\n' 'PASS NAS build and finished-ISO audit; VM boot acceptance remains separate' > NAS-BUILD-PASS.txt
