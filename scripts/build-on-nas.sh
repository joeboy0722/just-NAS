#!/bin/sh
# Administrator-only worker. Invoked only after the Windows controller's TCG gate.
set -eu
export DOCKER_HOST=unix:///run/just-nas-docker.sock DOCKER_BUILDKIT=0
gu=${1:?expected build pool GUID}; run=${2:?build run identifier}
pool=${3:-vmtest}; engine=${4:-homepool/.just-nas-extensions/engine}; engine_guid=${5:-}
case "$pool" in ''|*[!a-zA-Z0-9_.-]*) exit 2;; esac
case "$engine" in */.just-nas-extensions/engine) ;; *) exit 2;; esac
case "$gu" in ''|*[!0-9]*) exit 2;; esac
case "$run" in ''|*[!a-f0-9]*) exit 2;; esac
[ "$(zpool get -H -o value guid "$pool")" = "$gu" ] || { echo 'Pool identity mismatch' >&2; exit 1; }
# Never build while any managed guest is running.
if pgrep -x qemu-system-x86 >/dev/null || pgrep -f '^/usr/bin/qemu-system-x86_64 ' >/dev/null; then echo 'Stop guests normally before building' >&2; exit 1; fi
[ "$(docker info --format '{{.Driver}}')" = zfs ]
engine_pool=${engine%%/*}
zfs list -H "$engine" >/dev/null
[ -z "$engine_guid" ] || [ "$(zfs get -H -o value guid "$engine")" = "$engine_guid" ]
[ "$(docker info --format '{{.DockerRootDir}}')" = "/mnt/$engine_pool/.just-nas-extensions/engine" ]
[ "$(zfs get -H -o value mounted "$engine")" = yes ]
[ "$(zfs get -H -p -o value used "$engine")" -lt 8589934592 ]
[ "$(zfs get -H -p -o value available "$engine_pool")" -ge 12884901888 ] || { echo 'At least 12 GiB free on engine pool required' >&2; exit 1; }
zfs set quota=8G "$engine"
ds=$pool/.just-nas-build; base=/mnt/$pool/.just-nas-build
[ "$(zfs get -H -p -o value available "$pool")" -ge 42949672960 ] || { echo 'At least 40 GiB free on build pool required' >&2; exit 1; }
if ! zfs list -H "$ds" >/dev/null 2>&1; then
 [ ! -e "$base" ] || { echo 'Refusing existing unmanaged mount directory' >&2; exit 1; }
 zfs create -o quota=32G -o mountpoint="$base" "$ds"
fi
[ "$(zfs get -H -o value mounted "$ds")" = yes ]
[ "$(zfs get -H -o value mountpoint "$ds")" = "$base" ]
[ "$(findmnt -n -o SOURCE -T "$base")" = "$ds" ]
zfs set quota=32G "$ds"
mkdir -p "$base/runs/$run/output" "$base/cache" "$base/go-cache"
printf '%s\n' "$base/runs/$run"
