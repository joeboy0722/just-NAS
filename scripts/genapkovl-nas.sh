#!/bin/sh
set -eu
host="$1"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/etc/apk" "$tmp/etc/network" "$tmp/etc/init.d" "$tmp/etc/conf.d" "$tmp/etc/modprobe.d" "$tmp/usr/local/bin" "$tmp/var/lib/just-nas"
chmod 700 "$tmp/var/lib/just-nas"
echo "$host" > "$tmp/etc/hostname"
cat > "$tmp/etc/apk/world" <<EOF
alpine-base
zfs
samba-server
samba-common-tools
samba-server-openrc
nfs-utils
nfs-utils-openrc
smartmontools
util-linux
openssh-server
openssh-server-common-openrc
shadow
tzdata
openssl
acl
restic
EOF
cat > "$tmp/etc/network/interfaces" <<EOF
auto lo
iface lo inet loopback
auto eth0
iface eth0 inet dhcp
EOF
cp /builder/nas-server-linux "$tmp/usr/local/bin/nas-server"
chmod 755 "$tmp/usr/local/bin/nas-server"
mkdir -p "$tmp/usr/share/licenses/just-nas"
cp /builder/LICENSE /builder/THIRD-PARTY-NOTICES.md "$tmp/usr/share/licenses/just-nas/"
cp /builder/scripts/nas-server.initd "$tmp/etc/init.d/nas-server"
chmod 755 "$tmp/etc/init.d/nas-server"
echo 'options zfs zfs_arc_max=536870912' > "$tmp/etc/modprobe.d/zfs.conf"
echo 'zfs' > "$tmp/etc/modules"
rc_add() {
 mkdir -p "$tmp/etc/runlevels/$2"
 ln -s "/etc/init.d/$1" "$tmp/etc/runlevels/$2/$1"
}
for s in devfs dmesg mdev hwdrivers modloop; do rc_add "$s" sysinit; done
for s in hwclock modules sysctl hostname bootmisc syslog networking; do rc_add "$s" boot; done
rc_add nas-server default
for s in mount-ro killprocs savecache; do rc_add "$s" shutdown; done
# Root console access is local only. No passwordless SSH service is enabled.
tar -C "$tmp" -czf "$host.apkovl.tar.gz" etc usr var

