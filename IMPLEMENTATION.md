# 家庭 NAS 實作與驗收進度

本文件記錄開發候選版本，尚未宣稱完成所有驗收。

## 已保存的基線

- Git 基線 `1a34b86`，標籤 `baseline-working-nas`，功能分支 `feature/family-nas`。
- `output/baseline-working-nas.bundle`、VM 設定與原 ISO SHA256。
- 原測試 VDI 的加密離線備份與獨立金鑰在 `output/vm-test/`，均排除 Git。
- SATA 0 原有 VDI 保留。新測試 VDI：family-a、family-b、family-backup、family-spare。

## 已實作

- 分享編輯、啟停、固定 ID；帳號密碼同步變更與停用；公共／個人 ZFS 空間與 POSIX ACL。
- Unix UID 降權檔案程序；獨立檔案 session；網頁上下載、分頁、移動、回收／恢復、單檔快照恢復。
- 完整設定世代、校驗、加密匯出／匯入；持久任務、排程、告警與稽核。
- Restic 獨立 ext4 備份裝置、UUID／儲存庫識別、快照來源、版本保留與驗證恢復。
- 配額調低時不刪檔；超額期限 30 天；到期先清到期回收內容，再按最舊修改時間刪除，保留手動快照。清理可中止。
- 網路套用後 90 秒確認／回復；HTTPS 8443 與憑證匯入；SMTP 預設停用；鏡像替換與 scrub 任務。

## 已通過

- Go 測試、go vet、三份前端 JS 語法檢查。
- VM：全新鏡像池與帳號、公共與私有空間、session 隔離、網頁讀寫／唯讀、路徑越權拒絕、回收恢復、SMB 實際讀寫與私有隔離。
- VM：獨立備份碟、Restic 備份、版本列出、完整 read-data check、另處恢復及 Restic verify、加密設定匯出。
- 瀏覽器：管理頁登入、分享編輯實際儲存、編輯與停用操作可見。

## 待完成驗收

- 將最新排版與嵌入修正部署後驗證家庭檔案頁。
- 超額配額的 SMB／網頁限制、刪除與解除；30 天清理的可控測試。
- 帳號停用／改密碼、分享啟停與公共權限更新的既有 session 效果。
- 排程保留、單檔快照恢復、備份碟缺失安全停止。
- NFS、重啟持久化、設定匯入、HTTPS、網路回復與鏡像換碟演練。
- 完成映像交付、最終報告與操作說明。

## 驗收指令

`python scripts/verify-family.py` 僅在確認新 VDI 白名單後才建立 homepool。

`python scripts/verify-family-advanced.py backup|quota|accounts` 使用明確命名的測試空間。

所有測試帳密與原始結果留在 output，不提交 Git。實體硬碟、SMART、USB 開機與實際斷電需另作硬體驗收。
