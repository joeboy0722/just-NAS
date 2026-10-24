#!/bin/sh
# Alpine Linux NAS 環境初始化指引腳本
# 注意：依據使用者規範，套件安裝與系統配置需由使用者執行或明確確認。

set -e

echo "=== Alpine Linux NAS 部署與配置指引 ==="

# 1. 系統套件安裝說明
echo ""
echo "[Step 1] 請手動執行以下 apk 命令安裝 NAS 所需套件："
echo "   apk update"
echo "   apk add zfs zfs-lts samba nfs-utils smartmontools openssh shadow"
echo ""

# 2. tmpfs 掛載日誌與暫存區保護 Flash/SSD
echo "[Step 2] 設定 /var/log 與 /tmp 為 tmpfs (RAMDisk)："
echo "在 /etc/fstab 中新增以下內容："
echo "   tmpfs /var/log tmpfs defaults,noatime,mode=0755,size=64M 0 0"
echo "   tmpfs /tmp     tmpfs defaults,noatime,mode=1777,size=128M 0 0"
echo ""

# 3. OpenZFS 記憶體 (ARC Cache) 上限調整
echo "[Step 3] 限制 ZFS ARC 記憶體上限 (建議 512MB ~ 1GB)："
echo "建立 /etc/modprobe.d/zfs.conf 並寫入："
echo "   options zfs zfs_arc_max=1073741824" # 1GB in bytes
echo ""

# 4. OpenRC 服務開機自啟
echo "[Step 4] 設定開機自動啟動服務："
echo "   rc-update add zfs-import boot"
echo "   rc-update add zfs-mount boot"
echo "   rc-update add samba default"
echo "   rc-update add nfs default"
echo "   rc-update add sshd default"
echo ""
echo "=== 設定指引完畢 ==="
