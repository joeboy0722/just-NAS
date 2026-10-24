profile_nas() {
 profile_standard
 profile_abbrev="nas"
 title="Just NAS"
 desc="Offline ZFS NAS"
 kernel_addons="zfs"
 apks="alpine-base linux-lts zfs zfs-lts zfs-openrc samba-server samba-common-tools samba-server-openrc nfs-utils nfs-utils-openrc smartmontools util-linux openssh-server openssh-server-common-openrc shadow tzdata openssl acl restic"
 apkovl="scripts/genapkovl-nas.sh"
 hostname="just-nas"
 kernel_cmdline="console=tty0 console=ttyS0,115200"
 syslinux_serial="0 115200"
}

