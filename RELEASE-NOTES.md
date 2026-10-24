# just-NAS v2.0.0-rc3

輕量家庭 NAS 的開發驗證版本，以 Alpine Linux、OpenZFS 與 Go 建立，提供 ISO／USB 開機、網頁管理及家庭檔案入口。

## 本版內容

- ZFS 儲存池、資料空間與配額管理。
- 家庭帳號、個人與公共空間，以及清單勾選式權限設定。
- SMB／NFS 分享、網頁串流上下載、回收區及歷史版本恢復。
- 自動快照、Restic 獨立加密備份、任務排程與告警。
- 亮暗雙主題、手機版面，以及分享、儲存與系統設定操作改善。
- 刪池同步清理關聯分享、任務、排程與設定。
- 移除重複離線安裝包，ISO 約 **443.5 MB**；保留完整核心、驅動及韌體。
- ISO 根目錄與執行系統 `/usr/share/licenses/just-nas/` 附帶授權文件。

## 下載與使用

下載 `just-NAS-v2.0.0-rc3-x86_64.iso`，並使用 `SHA256SUMS` 核對完整性。初次使用從主控台取得 setup token，再開啟 `http://NAS-IP:8080` 完成設定。詳細步驟見專案 README。

隨附 `package-versions.txt`、`aports-revision.txt`、`LICENSE` 與 `THIRD-PARTY-NOTICES.md`，可核對套件版本、建置來源及授權。

## MIT 授權

**just-NAS 專案程式與文件採用 MIT License，Copyright (c) 2026 joeboy0722。** 完整授權條款見本 Release 附帶的 `LICENSE`，亦包含於 ISO。

MIT 授權範圍不會取代 ISO 內第三方元件的原有授權。Linux、OpenZFS、Samba、韌體等元件各自遵循其授權與著作權聲明，詳見 `THIRD-PARTY-NOTICES.md`。

## 使用前須知

這是 **Pre-release／開發驗證版本**，目前以家庭區網使用為範圍。已完成 Go 測試、ISO 離線安裝／授權檔檢查，以及 VirtualBox 冷開機、ZFS、設定保存、SMB／NFS 與網頁下載驗證；實體網卡、HBA、SMART、USB 開機與真實斷電仍需對應硬體驗證。

降低配額不會立即刪檔；持續超額滿 **30 天**，系統會依規則清理到期回收內容及最久未修改的檔案，保留管理員手動快照。請詳閱 README 的資料保護規則，並另做獨立備份。
