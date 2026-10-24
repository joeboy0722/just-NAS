build_just_nas_license() {
 cp /builder/LICENSE /builder/THIRD-PARTY-NOTICES.md "$DESTDIR/"
 if [ "${NAS_SOURCE_BOOTSTRAP:-0}" != 1 ]; then
  cp -a /builder/compliance "$DESTDIR/THIRD-PARTY-LICENSES"
 fi
}

section_just_nas_license() {
 [ "$PROFILE" = nas ] || return 0
 local manifest=bootstrap
 [ "${NAS_SOURCE_BOOTSTRAP:-0}" = 1 ] || manifest=$(sha256sum /builder/compliance/files.sha256)
 build_section just_nas_license $( (printf '%s\n' "$manifest"; cat /builder/LICENSE /builder/THIRD-PARTY-NOTICES.md) | checksum)
}

profile_nas() {
 profile_standard
 profile_abbrev="nas"
 title="Just NAS v2.1.0-rc2"
 desc="Offline ZFS NAS"
 kernel_addons="zfs"
 # The running kernel and ZFS modules live in /boot/modloop-lts, generated
 # by kernel_flavors + kernel_addons. Do not duplicate their APKs here:
 # linux-lts pulls the entire linux-firmware APK set into the offline repo.
 # Optional hardware is split before modloop signing by the builder wrapper.
 apks="alpine-base zfs zfs-openrc samba-server samba-common-tools samba-server-openrc nfs-utils nfs-utils-openrc smartmontools util-linux shadow tzdata openssl acl restic"
 apkovl="scripts/genapkovl-nas.sh"
 hostname="just-nas"
 kernel_cmdline="console=tty0 console=ttyS0,115200"
 syslinux_serial="0 115200"
}

