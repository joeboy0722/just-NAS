#!/bin/sh
set -eu
cp /builder/scripts/mkimg.nas.sh /aports/scripts/
cp /builder/scripts/genapkovl-nas.sh /aports/scripts/
printf '\n# build inputs: %s\n' "$(sha256sum /builder/nas-server-linux /builder/scripts/nas-server.initd /builder/LICENSE /builder/THIRD-PARTY-NOTICES.md | sha256sum)" >> /aports/scripts/genapkovl-nas.sh
sed -i 's/--no-chown//g' /aports/scripts/mkimage.sh
key_id="$(sha256sum /root/.abuild/*.pub | sha256sum | cut -d ' ' -f 1)"
cache=/iso-cache
if [ -f /iso-cache/signing-key-id ] && [ "$(cat /iso-cache/signing-key-id)" != "$key_id" ]; then
 cache="/iso-cache/$key_id"
else
 mkdir -p /iso-cache
 printf '%s\n' "$key_id" > /iso-cache/signing-key-id
fi
cd /aports
sh scripts/mkimage.sh --workdir "$cache" --tag 3.24.3 --arch x86_64 --profile nas --outdir /builder/output \
 --repository https://dl-cdn.alpinelinux.org/alpine/v3.24/main \
 --repository https://dl-cdn.alpinelinux.org/alpine/v3.24/community
cp /builder/output/alpine-nas-3.24.3-x86_64.iso /builder/output/alpine-nas-full.iso.partial
mv /builder/output/alpine-nas-full.iso.partial /builder/output/alpine-nas-full.iso
cd /builder/output
sha256sum alpine-nas-full.iso > alpine-nas-full.iso.sha256
git -C /aports rev-parse HEAD > aports-revision.txt
xorriso -indev alpine-nas-full.iso -find /apks/x86_64 -name '*.apk' -exec echo 2>/dev/null | sed -e 's|.*/||' -e "s/'$//" | sort -u > package-versions.txt



# Fail the build if size regressions or incomplete offline dependencies slip in.
sh /builder/scripts/audit-iso.sh /builder/output/alpine-nas-full.iso /builder/output/iso-audit
