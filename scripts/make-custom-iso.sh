#!/bin/sh
set -eu
if [ "${NAS_SOURCE_BOOTSTRAP:-0}" = 1 ]; then
 echo 'LOCAL SOURCE BOOTSTRAP ONLY: this image must not be published' >&2
 touch /builder/output/UNPUBLISHABLE-BOOTSTRAP
 manifest=bootstrap
else
 test -f /builder/compliance/READY || { echo 'Missing matching third-party license/source collection' >&2; exit 1; }
 (cd /builder/compliance && sha256sum -c files.sha256 >/dev/null)
 manifest=$(sha256sum /builder/compliance/files.sha256)
fi
cp /builder/scripts/mkimg.nas.sh /aports/scripts/
cp /builder/scripts/genapkovl-nas.sh /aports/scripts/
printf '\n# build inputs: %s\n' "$(sha256sum /builder/nas-server-linux /builder/scripts/nas-server.initd /builder/LICENSE /builder/THIRD-PARTY-NOTICES.md /builder/store-keys/*.pub /builder/store-catalog.json | sha256sum)" >> /aports/scripts/genapkovl-nas.sh
sed -i 's/--no-chown//g' /aports/scripts/mkimage.sh
printf '# third-party collection: %s\n' "$manifest" >> /aports/scripts/genapkovl-nas.sh
key_id="$(sha256sum /root/.abuild/*.pub | sha256sum | cut -d ' ' -f 1)"
cache=/iso-cache/stores-v1
if [ -f /iso-cache/signing-key-id ] && [ "$(cat /iso-cache/signing-key-id)" != "$key_id" ]; then
 cache="/iso-cache/stores-v1/$key_id"
else
 mkdir -p /iso-cache
 printf '%s\n' "$key_id" > /iso-cache/signing-key-id
fi
policy=$(sha256sum /builder/scripts/split-modloop.py | cut -d ' ' -f 1)
if [ -f "$cache/split-policy.sha256" ] && [ "$(cat "$cache/split-policy.sha256")" != "$policy" ]; then
 cache="$cache/policy-$policy"
fi
mkdir -p "$cache"
cd /aports
sh scripts/mkimage.sh --workdir "$cache" --tag 3.24.3 --arch x86_64 --profile nas --outdir /builder/output \
 --repository https://dl-cdn.alpinelinux.org/alpine/v3.24/main \
 --repository https://dl-cdn.alpinelinux.org/alpine/v3.24/community
cp /builder/output/alpine-nas-3.24.3-x86_64.iso /builder/output/alpine-nas-stores.iso.partial
mv /builder/output/alpine-nas-stores.iso.partial /builder/output/alpine-nas-stores.iso
cd /builder/output
sha256sum alpine-nas-stores.iso > alpine-nas-stores.iso.sha256
git -C /aports rev-parse HEAD > aports-revision.txt
xorriso -indev alpine-nas-stores.iso -find /apks/x86_64 -name '*.apk' -exec echo 2>/dev/null | sed -e 's|.*/||' -e "s/'$//" | sort -u > package-versions.txt



# Fail the build if size regressions or incomplete offline dependencies slip in.
sh /builder/scripts/audit-iso.sh /builder/output/alpine-nas-stores.iso /builder/output/core-iso-audit
printf '%s\n' "$policy" > "$cache/split-policy.sha256"
[ "${NAS_SOURCE_BOOTSTRAP:-0}" = 1 ] || rm -f /builder/output/UNPUBLISHABLE-BOOTSTRAP
