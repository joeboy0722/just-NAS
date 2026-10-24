# just-NAS v2.0.0-rc4 — 授權與對應來源補件版

本版取代 v2.0.0-rc3，保留 NAS 功能與動態載入的 OpenZFS，補齊第三方授權、著作權聲明、韌體文件與精確對應來源。

## 本版修正

- ISO 根目錄及開機系統包含第三方授權目錄、套件索引與來源取得說明。
- 保存 84 組精確 Alpine 配方、其上游來源與修補檔，涵蓋 146 個離線 APK 及主要開機元件；來源經原配方 SHA512／SHA256 驗證。
- 保存韌體 WHENCE、廠商聲明與 Alpine 使用的 Raspberry Pi 補充來源。
- 保存 Restic 實際使用的 85 個 Go 模組及兩個 Go runtime 的來源與校驗資料。
- 正常建置驗證授權目錄、離線套件清單與開機 payload；不相符即拒絕通過。

## 下載

- `just-NAS-v2.0.0-rc4-x86_64.iso`：NAS 開機映像。
- `SHA256SUMS`：所有附件的 SHA256。
- `just-NAS-v2.0.0-rc4-licenses.tar.gz`：獨立授權資料包，自行建置前解壓至 `output/compliance/`。
- `just-NAS-v2.0.0-rc4-sources.tar.zst.part-001` 至 `part-007`：完整對應來源的連續分卷；全部下載後，依 `SOURCES.md` 合併解壓。
- `SOURCES.md`、`SOURCE-PARTS.json`：來源校驗、合併與重建說明。
- `LICENSE`、`THIRD-PARTY-NOTICES.md`、`COMPLIANCE.md`、套件與 aports 資訊。

來源與 ISO 於同一 Release 提供下載，不需額外付費；原始碼另附，不塞進開機 ISO。持續散布 ISO 時也應保留來源附件。

## MIT 與第三方授權

**just-NAS 自有程式與文件採 MIT License，Copyright (c) 2026 joeboy0722。** 完整條款見 `LICENSE`。

Linux、OpenZFS、Samba、Go runtime、韌體等第三方元件保留各自原有授權、著作權及例外條款；整張 ISO 不能視為全部 MIT。

OpenZFS 以獨立 `zfs.ko` 動態載入。OpenZFS 官方認為可散布獨立模組，FSF 對其與 Linux 的組合持不同見解。本版提供來源與聲明，不宣稱該法律爭點已消除，詳見 `COMPLIANCE.md`。

## 驗證與使用範圍

本版為 **Pre-release／開發驗證版本**。發布驗證包括來源校驗、來源分卷完整性、ISO／runtime 授權目錄、離線安裝、Go 測試及 VM 冷開機、ZFS、SMB／NFS 與既有設定核對。

仍以家庭區網使用為範圍；實體網卡、HBA、SMART、USB 開機與真實斷電需另行驗收。

降低配額不立即刪檔；持續超額滿 **30 天**後，依規則清理到期回收內容及最久未修改的檔案，保留管理員手動快照。請詳閱 README 並另做獨立備份。
