# VM／QEMU／Docker 選配套件（預發布）

這是 just-NAS 商店 v2 預發布套件，需搭配 **v2.1.0-rc4** 核心。VM 管理器與 Docker 都是選配，不內建於核心；VM 不依賴 Docker。

- `vm-manager-1.0.0-rc2.jnpkg`：原生管理介面、noVNC、KVM／TCG、TCG 快取設定、執行中 NAT 轉發修改。
- `qemu-runtime-11.0.3-r0.jnpkg`：QEMU、BIOS／UEFI 與完整離線相依。
- `container-engine-1.0.0.jnpkg`：Docker29.9.2、ZFS 後端；內容與既有 stores-v1 套件相同，舊網址保留。
- `catalog.json`：官方 Ed25519 簽章目錄。來源與完整第三方授權集中於支援包；SHA256SUMS 驗證附件。

已驗證 Windows10專業版 KVM 手動安裝、進入桌面、正常關機及卸下ISO後磁碟開機；另通過空白KVM客體、TCG資源限制、NAT熱更新、離線簽章／來源檢查及NAS自我建置。這不是全面穩定性認證：Ubuntu24.04先前TCG安裝後桌面黑畫面仍列限制，完整Linux KVM安裝與30分鐘壓測尚未完成。套件上架後會另驗證匿名下載及全新測試環境的線上安裝。

Docker圖標提供引擎啟停、狀態與日誌；任意容器操作目前經管理員SSH，沒有完整圖形容器管理面板。

just-NAS自有程式與文件採 **MIT**。第三方QEMU、noVNC、Docker、韌體及相依各自保留原授權，整包不是統一MIT。支援包附專案來源、QEMU完整對應來源／配方、Docker對應來源壓縮包、版本清單與授權；核心既有來源分卷繼續在v2.0.0-rc4公開提供。
