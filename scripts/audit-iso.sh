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
tar -xzOf "$tmp/nas.apkovl.tar.gz" usr/local/bin/nas-server > "$tmp/embedded-server"
cmp /builder/nas-server-linux "$tmp/embedded-server"
tar -xzOf "$tmp/nas.apkovl.tar.gz" usr/share/just-nas/catalog.json > "$tmp/embedded-catalog.json"
cmp /builder/store-catalog.json "$tmp/embedded-catalog.json"
tar -xzOf "$tmp/nas.apkovl.tar.gz" usr/share/just-nas/keys/official.pub > "$tmp/embedded-store.pub"
cmp /builder/store-keys/official.pub "$tmp/embedded-store.pub"
# Verify terminal ownership and branding in the actual overlay, not only source templates.
tar -xzOf "$tmp/nas.apkovl.tar.gz" etc/inittab > "$tmp/inittab"
grep -q '^tty1::sysinit:/bin/true$' "$tmp/inittab"
grep -q '^tty0::sysinit:/bin/true$' "$tmp/inittab"
grep -q '^tty2::respawn:/sbin/getty' "$tmp/inittab"
if grep -q '^tty1::respawn:' "$tmp/inittab"; then echo 'ERROR: competing tty1 login' >&2; exit 1; fi
tar -xzOf "$tmp/nas.apkovl.tar.gz" etc/issue > "$tmp/issue"
grep -q 'Welcome to Just NAS.' "$tmp/issue"
if grep -q 'Welcome to Alpine' "$tmp/issue"; then echo 'ERROR: unbranded login' >&2; exit 1; fi
tar -xzOf "$tmp/nas.apkovl.tar.gz" etc/init.d/nas-boot > "$tmp/nas-boot"
grep -q 'just-NAS' "$tmp/nas-boot"
for file in LICENSE THIRD-PARTY-NOTICES.md; do
 xorriso -osirrox on -indev "$iso" -extract "/$file" "$tmp/$file" >/dev/null 2>&1
 cmp "/builder/$file" "$tmp/$file"
 tar -xzOf "$tmp/nas.apkovl.tar.gz" "usr/share/licenses/just-nas/$file" > "$tmp/runtime-$file"
 cmp "/builder/$file" "$tmp/runtime-$file"
done
for notice in LICENSE PATENTS; do
 xorriso -osirrox on -indev "$iso" -extract "/THIRD-PARTY-LICENSES/just-nas-go/$notice" "$tmp/go-image-$notice" >/dev/null 2>&1
 cmp "/builder/vendor/golang.org/x/image/$notice" "$tmp/go-image-$notice"
 tar -xzOf "$tmp/nas.apkovl.tar.gz" "usr/share/licenses/just-nas/go-image/$notice" > "$tmp/runtime-go-image-$notice"
 cmp "/builder/vendor/golang.org/x/image/$notice" "$tmp/runtime-go-image-$notice"
done
if [ "${NAS_SOURCE_BOOTSTRAP:-0}" != 1 ]; then
 xorriso -osirrox on -indev "$iso" -extract /THIRD-PARTY-LICENSES "$tmp/iso-licenses" >/dev/null 2>&1
 cmp /builder/compliance/files.sha256 "$tmp/iso-licenses/files.sha256"
 (cd "$tmp/iso-licenses" && sha256sum -c files.sha256 >/dev/null)
 mkdir -p "$tmp/overlay"
 tar -xzf "$tmp/nas.apkovl.tar.gz" -C "$tmp/overlay" usr/share/licenses/just-nas/third-party
 (cd "$tmp/overlay/usr/share/licenses/just-nas/third-party" && sha256sum -c files.sha256 >/dev/null)
 find "$tmp/apks" -name '*.apk' | sed 's|.*/||' | sort > "$report/runtime-apks.txt"
 # Core packages must be a subset of the already sourced/licensed release inputs.
 python3 /builder/scripts/audit-core.py "$tmp" "$report"

fi
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
printf 'PASS: ISO/runtime licenses, offline install, required runtime commands, BIOS/UEFI boot assets, ZFS modules and firmware\n' | tee "$report/result.txt"
