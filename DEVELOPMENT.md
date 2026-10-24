# 開發與工作目錄

## 保留內容

- `cmd/`、`internal/`、`web/`：NAS 程式及測試。
- `scripts/`：建置、ISO 離線內容稽核，以及可重複使用的 API、UI、SMB／NFS、VM 驗證工具。破壞性測試只能在腳本指定的獨立測試 VDI 上執行。
- `output/alpine-nas-full.iso`、`.sha256`、`package-versions.txt`、`aports-revision.txt`：目前交付版本。
- `output/vm-test/`：現用 VM 虛擬碟、測試帳密、受信任憑證、備份儲存庫測試金鑰／UUID，以及 VM 使用中的序列日誌。
- `output/ui-review/vm-before.json`、`vm-before-config.txt`：更新後核對設定與虛擬碟的驗證基準；`vm-final-verification.json` 為最近一次結果。
- `output/test-deps/`：本機 Python SMB 驗證依賴。
- `output/iso-audit/`：ISO 瘦身及離線安裝驗證摘要。
- `output/before-github-cleanup-20261024.bundle`：清理前完整 Git 歷史備份。

`output/` 全部不進 Git。帳密、憑證、VM 狀態與資料碟只供這台開發機使用；乾淨 clone 不會帶有這些檔案。

## 建置與驗證

Windows 使用 `scripts/build-all-in-one.ps1`，Linux 使用 `sh scripts/build-iso.sh`。Go binary、Go cache 與 ISO 都可重建，不需提交到 Git。

程式與 UI 單位檢查：

```text
go test ./...
go vet ./...
node scripts/ui-units.cjs
python scripts/audit-repository.py
```

現有開發 VM 的一般更新流程：

```text
python scripts/vm-cycle.py output/alpine-nas-full.iso
python scripts/ui-verify-vm.py
```

第二個指令以本機基準確認既有帳號、分享、配額、網路、備份、排程、ZFS、網頁資源及 SMB／網頁下載。第一次建立新的測試環境時，須準備自己的憑證、帳密與基準；不可直接把別台機器的狀態當成驗證基準。

歷史 `verify-vm.py` 是 NAT 與 test-a/test-b 拓撲的初始化驗收；`verify-family*.py` 有特定帳號、測試檔及磁碟拓撲前置條件。這些腳本保留供完整功能回歸使用，不能當作無條件執行的一鍵檢查。退役的測試碟及舊版回退 ISO 已清除，相關故障／回退演練需先重建獨立 fixture。

## GitHub 發布

只提交程式、文件和可重複使用的測試工具。ISO 與 SHA256 適合另放 GitHub Release 資產，避免大型映像進入 Git 歷史。本機密碼、金鑰、資料碟及 bundle 不上傳。

`audit-repository.py` 檢查追蹤中的產物路徑、全歷史大型 blob、常見私鑰／Token 格式，以及目前本機驗證密碼是否出現在 Git 歷史；不會印出密碼。這是有範圍的發布前檢查，並非涵蓋所有秘密格式的掃描器。

此次整理尚未設定 GitHub 遠端或執行 push。清理明細保存於本機 `output/cleanup-report.json`。
