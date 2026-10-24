#!/bin/sh
# Test-only copy: original ISO stays unchanged; do not publish this fixture.
set -eu
source_iso="${1:?source ISO}"; target_iso="${2:?test ISO}"
[ "$source_iso" != "$target_iso" ]
[ ! -e "$target_iso" ]
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
xorriso -osirrox on -indev "$source_iso" -extract /boot/syslinux "$tmp/syslinux" -extract /boot/grub "$tmp/grub" >/dev/null 2>&1
find "$tmp/syslinux" -name '*.cfg' -exec sed -i '/^[[:space:]]*[Aa][Pp][Pp][Ee][Nn][Dd][[:space:]]/ s/$/ just_nas_boot=plain/' {} +
find "$tmp/grub" -name '*.cfg' -exec sed -i '/^[[:space:]]*linux[[:space:]]/ s/$/ just_nas_boot=plain/' {} +
grep -R -q 'just_nas_boot=plain' "$tmp/syslinux"
xorriso -indev "$source_iso" -outdev "$target_iso" -boot_image any replay -map "$tmp/syslinux" /boot/syslinux -map "$tmp/grub" /boot/grub -commit -end >/dev/null 2>&1
printf 'Created test-only plain-mode ISO: %s\n' "$target_iso"
