#!/bin/sh
# Verify the finished offline image, rather than merely checking profile text.
set -eu
iso="${1:?usage: audit-iso.sh image.iso}"
report="${2:-$(dirname "$iso")/iso-audit}"
mkdir -p "$report"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
xorriso -indev "$iso" -find / -type f -exec lsdl 2>/dev/null > "$report/files.txt"
# These APKs duplicate /boot and drag the full firmware distribution into the repo.
if grep -E "'/apks/[^/]+/(linux-lts-|zfs-lts-|linux-firmware[^/]*).*\.apk'" "$report/files.txt" > "$report/redundant-kernel-apks.txt"; then
 echo 'ERROR: duplicate kernel/firmware APKs in offline runtime repository' >&2
 exit 1
fi
for file in /boot/vmlinuz-lts /boot/initramfs-lts /boot/modloop-lts /boot/syslinux/isolinux.bin /efi/boot/bootx64.efi; do
 grep -F "'$file'" "$report/files.txt" >/dev/null || { echo "ERROR: missing boot asset $file" >&2; exit 1; }
done
xorriso -osirrox on -indev "$iso" -extract /apks "$tmp/apks" -extract /just-nas.apkovl.tar.gz "$tmp/nas.apkovl.tar.gz" -extract /boot/modloop-lts "$tmp/modloop-lts" >/dev/null 2>&1
tar -xzOf "$tmp/nas.apkovl.tar.gz" etc/apk/world > "$report/runtime-world.txt"
mkdir -p "$tmp/root"
apk add --initdb --root "$tmp/root" --keys-dir /etc/apk/keys --repositories-file /dev/null --repository "$tmp/apks" --no-network --no-scripts $(cat "$report/runtime-world.txt") > "$report/offline-install.txt"
# Package triggers are intentionally disabled for this isolated filesystem audit.
# Install BusyBox applet links locally, without starting services or touching devices.
chroot "$tmp/root" /bin/busybox --install -s
chroot "$tmp/root" /bin/sh -c 'for c in zpool zfs smbd smbpasswd smbcontrol testparm exportfs smartctl restic lsblk blkid findmnt mount umount setfacl getfacl adduser addgroup deluser id ifup ifdown; do command -v "$c" || exit 1; done' > "$report/runtime-commands.txt"
chroot "$tmp/root" /usr/sbin/smbd -V > "$report/samba-version.txt"
chroot "$tmp/root" /usr/bin/restic version > "$report/restic-version.txt"
unsquashfs -l "$tmp/modloop-lts" > "$report/modloop-files.txt"
grep -E '/zfs\.ko(\.(xz|zst|gz))?$' "$report/modloop-files.txt" >/dev/null || { echo 'ERROR: missing ZFS kernel module' >&2; exit 1; }
grep '/modules/firmware/' "$report/modloop-files.txt" >/dev/null || { echo 'ERROR: missing runtime firmware' >&2; exit 1; }
awk '$9 ~ /\.apk\047$/ {print $5 "\t" $9}' "$report/files.txt" | sort -nr > "$report/package-sizes.tsv"
printf 'PASS: offline install, required runtime commands, BIOS/UEFI boot assets, ZFS modules and firmware\n' | tee "$report/result.txt"
