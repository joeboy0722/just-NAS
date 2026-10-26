#!/bin/sh
# Administrator-only worker. Invoked only after the Windows controller's TCG gate.
set -eu
export DOCKER_HOST=unix:///run/just-nas-docker.sock DOCKER_BUILDKIT=0
gu=${1:?expected vmtest pool GUID}; run=${2:?build run identifier}
case "$gu" in ''|*[!0-9]*) exit 2;; esac
case "$run" in ''|*[!a-f0-9]*) exit 2;; esac
[ "$(zpool get -H -o value guid vmtest)" = "$gu" ] || { echo 'Pool identity mismatch' >&2; exit 1; }
# Never build while any managed guest is running.
if pgrep -x qemu-system-x86 >/dev/null || pgrep -f '^/usr/bin/qemu-system-x86_64 ' >/dev/null; then echo 'Stop guests normally before building' >&2; exit 1; fi
[ "$(docker info --format '{{.Driver}}')" = zfs ]
engine=homepool/.just-nas-extensions/engine
zfs list -H "$engine" >/dev/null
[ "$(zfs get -H -o value mounted "$engine")" = yes ]
[ "$(zfs get -H -p -o value used "$engine")" -lt 8589934592 ]
[ "$(zfs get -H -p -o value available homepool)" -ge 12884901888 ] || { echo 'At least 12 GiB free on homepool required' >&2; exit 1; }
zfs set quota=8G "$engine"
ds=vmtest/.just-nas-build; base=/mnt/vmtest/.just-nas-build
[ "$(zfs get -H -p -o value available vmtest)" -ge 42949672960 ] || { echo 'At least 40 GiB free on vmtest required' >&2; exit 1; }
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
