# Third-party components

The MIT license in LICENSE applies to the just-NAS project code and documentation.
It does not relicense the third-party software, firmware or other material bundled
in the bootable ISO. Those components retain their own copyright notices and
licenses, including their applicable redistribution and source-code obligations.

The ISO contains Alpine Linux packages and components such as the Linux kernel,
OpenZFS, Samba, NFS utilities, BusyBox, Restic, Go runtime and hardware firmware.
Their licenses are not all MIT. Package license metadata is available in the
Alpine APK files (.PKGINFO); package versions and the aports revision used to build
this image are distributed alongside the ISO.

Upstream source and package build information:

- Alpine packages: https://pkgs.alpinelinux.org/
- Alpine aports build recipes: https://gitlab.alpinelinux.org/alpine/aports
- Linux: https://www.kernel.org/
- OpenZFS: https://github.com/openzfs/zfs
- Samba: https://www.samba.org/
- NFS utilities: https://git.kernel.org/pub/scm/linux/kernel/git/rw/nfs-utils.git/
- BusyBox: https://busybox.net/
- Restic: https://github.com/restic/restic
- Go: https://go.dev/

The just-NAS LICENSE and this notice are included both at the ISO root and in
/usr/share/licenses/just-nas/ in the running system.
