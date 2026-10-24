# ISO 輕量化驗收（2026-10-24）

## 備份與交付

修改前工作目錄乾淨，已建立 `before-iso-slim-20261024` 標籤與 `output/before-iso-slim-20261024.bundle` 完整 Git 備份，並通過 `git bundle verify`。開發分支為 `perf/minimal-offline-iso`。

新版交付為 `output/alpine-nas-full.iso` 與其 `.sha256`，VM 也直接掛載這份正式映像。現有虛擬磁碟、帳號、資料及設定保留。

2026-10-24 準備 GitHub 前，依要求清除舊映像及歷次候選版本；歷史程式可由 Git 標籤回復。先前 bundle 已整併為 `output/before-github-cleanup-20261024.bundle` 完整備份。

## 實際縮減

| 項目 | 大小 |
|---|---:|
| 舊 ISO | 1,419,100,160 bytes（1,419.1 MB） |
| 新 ISO | 443,547,648 bytes（443.5 MB） |
| 減少 | 975,552,512 bytes，68.74% |

原因是離線 APK 倉庫重複收錄了 `linux-lts`、`zfs-lts`，並拉入完整的 `linux-firmware*` 套件分發檔。其中韌體 APK 約 756.8 MiB，核心與 ZFS 模組 APK 約 172.6 MiB。開機所需內容已由原有的核心與 `/boot/modloop-lts` 提供，實際執行環境並未安裝這些重複 APK。

因此只從 ISO 的離線套件清單移除明列的 `linux-lts`、`zfs-lts`；保留原本 LTS 核心、`kernel_addons="zfs"`、完整驅動與韌體 modloop。這不是移除執行環境的核心或 ZFS。

## 保留項目與取捨

- 真正的 runtime world 不變；隔離環境在完全禁止網路的條件下，仍可從新 ISO 安裝全部 146 個執行套件。
- 所有保留的 APK 都與舊 ISO 逐位元相同。核心、initramfs、modloop、EFI 等 14 個開機檔案雜湊一致。
- BIOS 與 UEFI 開機項目皆存在。BIOS boot catalog 與 isolinux boot-info table 隨 ISO 位置改變；isolinux 可執行內容未改變。
- Python 是 `nfs-utils`／`libnfsidmap` 的套件依賴；Samba common-tools 提供帳號、驗證與服務管理所需指令，因此保留。
- 目前最大單一檔案是 330,240,000 bytes 的完整 modloop。為保留實體 NAS 的硬碟控制器、網卡及韌體支援，這次不裁切驅動、不改成僅供 VM 的核心。

## 驗證結果

`scripts/audit-iso.sh` 已接入映像建置流程，會拒絕重新出現的重複核心／韌體 APK，檢查開機檔案、完整離線依賴安裝、必要 runtime 指令、Samba／Restic 可執行性及 modloop 內的 ZFS／韌體。

本次驗證通過：

- ISO 離線安裝與內容檢查；BIOS／UEFI 開機結構檢查。
- `go test ./...`。
- 原 `just-nas` VM 使用新 ISO 冷開機，僅啟用 Host-only 網卡，沒有 NAT／外網介面。
- ZFS 儲存池全部 ONLINE；帳號、分享權限、配額、壓縮、排程、網路與備份設定一致。
- 原虛擬磁碟路徑及 UUID 一致；未操作主機實體磁碟。
- VM 上全部八個網頁資源與專案來源一致。
- SMB 445 及家庭檔案入口下載 `proof.txt` 內容一致；SHA256 為 `6c7144a85a057070b3efc3bb7e72daaeb6ddc5e19e78d25483db9fe0c365f2b2`。
- NFSv4 從實際匯出的 `/mnt/homepool/LinuxShare/proof.txt` 讀取成功，原始內容正確。

本次 VM 實測 BIOS 冷開機；UEFI 以映像結構與未變更的 EFI 檔案驗證，未另開 UEFI VM。實體硬體相容性未重新逐台驗收；完整驅動及韌體內容保持原樣。

保留的摘要報告位於被 Git 忽略的 `output/iso-audit/` 及 `output/ui-review/vm-final-verification.json`。大型解包內容與一次性測試產物已於發布前清理。
