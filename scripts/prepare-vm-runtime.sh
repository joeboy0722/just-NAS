#!/bin/sh
set -eu
apk add --no-cache qemu-system-x86_64 qemu-img ovmf seabios qemu-hw-display-virtio-vga
dest=/out/vm-runtime-payload
mkdir -p "$dest/apks" "$dest/licenses"
apk fetch --recursive --output "$dest/apks" qemu-system-x86_64 qemu-img ovmf seabios qemu-hw-display-virtio-vga
apk info -vv > "$dest/licenses/package-versions.txt"
apk info -a > "$dest/licenses/package-information.txt"
[ ! -d /usr/share/licenses ] || cp -a /usr/share/licenses "$dest/licenses/alpine-licenses"
qemu-system-x86_64 -version > "$dest/licenses/qemu-version.txt"
qemu-system-x86_64 -display help > /out/qemu-display-backends.txt
qemu-system-x86_64 -machine help > /out/qemu-machine-types.txt
find /usr/share -iname '*OVMF*' -o -iname '*edk2*' -o -iname '*bios*.bin' > /out/qemu-firmware-files.txt
tar -czf /out/qemu-runtime-root.tar.gz /usr/bin/qemu-system-x86_64 /usr/bin/qemu-img /usr/share/qemu /usr/share/OVMF /usr/share/seabios 2>/dev/null || true
