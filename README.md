# just-NAS

以 Alpine Linux、OpenZFS 與 Go 建立的 USB／ISO 開機式家庭 NAS。正式功能須在 Linux 開機環境使用；Windows 直接執行不會回傳模擬硬碟或假成功。

## 建置

需求：Go 1.25+、Docker Linux 引擎。Windows：

```powershell
.\scripts\build-all-in-one.ps1
```

Linux：

```sh
sh scripts/build-iso.sh
```

產物：
- `output/alpine-nas-full.iso`
- `output/alpine-nas-full.iso.sha256`
- `output/package-versions.txt`
- `output/aports-revision.txt`

Dockerfile 與 Dockerfile.iso-builder 都提供 ISO 建置環境，Docker 僅用於建置 ISO。NAS 與 ZFS 在 VirtualBox 或實體機的 Linux 核心上執行。首次建置會下載核心與硬體韌體，耗時較久；後續使用 `just-nas-iso-cache` volume 快取。

建置基底鎖定 Alpine 3.24.3，套件固定同一 v3.24 repository；每次輸出的 manifest 記錄精確套件與 aports 版本。同分支套件仍會更新，重建不保證位元完全一致。要封存特定版本，請保留 ISO、SHA256、manifest 與 builder image。

## 首次使用

1. VM 建議 2 核心、4GB RAM，以 ISO 開機，另接獨立空白虛擬資料碟。
2. 取得 DHCP 位址，瀏覽 `http://NAS-IP:8080`。VirtualBox NAT 可轉送主機 8080 到客體 8080。
3. 從 VM 主控台取得 setup token。此憑證不透過網頁公開，預設 root 登入維持鎖定。
4. 設定至少 12 字元的管理員密碼。建池前設定在 RAM，重開機會重新初始化。
5. 建立第一個 ZFS Pool，系統自動建立專用設定資料集，保存管理員與之後的檔案帳號／分享。
6. 建立資料集，例如 `datapool/files`，掛載點為 `/mnt/datapool/files`。
7. 在「檔案共享」建立 SMB 檔案帳號，發布 SMB 分享並授權帳號。管理員密碼與 SMB 密碼獨立。
8. Windows 連線 `\\NAS-IP\ShareName`；NFS 需填寫明確的用戶端 IP 或 CIDR。

若使用 VirtualBox NAT，在 Windows 主機執行 SMB 測試可轉送主機 1445 到客體 445，再使用支援自訂 port 的 SMB client。Windows 檔案總管使用標準 445；要從其他家用裝置存取，將 VM 改為區網橋接網路。

## 目前 just-nas VM 連線

VM 已改用 Host-only 網路，DHCP 為此 VM 保留 `192.168.56.101`，供這台 Windows 主機測試。

- 管理頁：`http://192.168.56.101:8080/`
- 檔案總管網址列：`\\192.168.56.101\Family`
- SMB 帳號：`family`，使用 SMB 密碼，與管理頁密碼獨立。
- NFS：`192.168.56.101:/mnt/testpool/files`，僅允許主機 `192.168.56.1`。

Windows 自己的 445 埠由系統服務佔用，請直接連 VM IP。此網路供本機使用；其他家用裝置需要另外設定區網橋接。

在目前網路重跑驗收前，先設定：

```powershell
$env:NAS_TEST_HOST='192.168.56.101'
$env:NAS_TEST_SMB_PORT='445'
$env:NAS_TEST_NFS_PORT='2049'
$env:NAS_TEST_NFS_CLIENT='192.168.56.1'
```

未設定時，驗收腳本仍使用原 NAT 預設值。

## 持久化與資料保護

第一個池的 `<pool>/.just-nas` 資料集掛在 `/mnt/<pool>/.system_config`。它保存管理員雜湊、SMB 雜湊資料庫、檔案帳號 UID 與分享定義，權限為 root 私有。

重開機先載入 ZFS、匯入池並掛載設定資料集，再還原帳號與啟動分享。Session 不跨重開機保存。系統不以 `zpool import -f` 搶占其他主機的池；遇到多個設定來源或損壞 JSON 會記錄錯誤，不應覆寫原設定。

空閒硬碟必須沒有分割區、檔案系統簽章、掛載與既有 ZFS 成員標記。系統不提供自動清碟；有資料的碟不會強制格式化。建池前會重新檢查，且要求輸入池名稱。控制台禁止銷毀承載系統設定的池。刪資料集不是遞迴刪除；有快照或子資料集時會由 ZFS 拒絕。

分享目標必須位於已掛載的資料集內。SMB 透過各別帳號驗證，共享內的檔案使用 nobody 服務身分；這是家庭分享模型，不提供每個檔案的使用者 ACL。NFS 使用 IP／網段限制並映射為 nobody，預設不允許 root 特權。

快照還原不自動刪除較新快照。有較新快照時會拒絕；管理員須先自行移除不需要的快照。SMART 不可用時顯示未知，虛擬硬碟一般不提供實體 SMART。

舊版本只保存 smb.conf／exports，未保存管理員、檔案帳號與可靠的設定來源。更新前保留舊 ISO 及舊 .system_config；不要把新 JSON 狀態直接覆蓋到舊備份。既有 ZFS 資料池可保留資料；舊分享須在新控制台重新發布。

## API

所有 `/api/*` 管理端點須登入；只有 `/api/auth/status`、首次 setup 與 login 公開。
- `POST /api/auth/setup`：`{token,password}`；`POST /api/auth/login`：`{password}`。
- 回傳 session cookie 與 CSRF token；寫入請求須帶 `X-CSRF-Token`，跨來源請求拒絕。
- 若本機瀏覽器／代理只移除 Origin 的埠號，僅在 Sec-Fetch-Site 為 same-origin 且 Referer 完整符合目的協定、主機與埠號時接受；不接受任意跨埠請求，CSRF 驗證仍保留。
- `GET /api/system/diagnostics`：核心、ZFS、Samba、NFS 及設定保存狀態。
- `GET/POST/DELETE /api/users`：家庭檔案帳號管理。
- 保留 storage pools/datasets/snapshots、shares、status、logs API。
- 建池 body 加入與 name 相同的 `confirm`；銷毀池／資料集 query 加入 `confirm=name`；快照還原 body 加入 `confirm=name`。

## 測試

```powershell
go test ./...
go vet ./...
node --check web/js/app.js
```

VM 驗收腳本 `scripts/verify-vm.py` 專門針對 just-nas VM、新建的 test-a.vdi / test-b.vdi 與本機測試轉送埠。執行前會檢查 SATA port 1/2 的 host 檔案路徑，僅接受這兩顆 20GB 虛擬測試碟；原 SATA port 0 不會初始化。

```powershell
python -m pip install --target output/test-deps requests smbprotocol
python scripts/verify-vm.py create
python scripts/verify-vm.py cycle
python scripts/verify-vm.py files
python scripts/nfs_probe.py
python scripts/verify-browser-origin.py
```

驗收報告、序列埠日誌與測試密碼位於 `output/vm-test/`，不得提交或對外分享測試密碼。交付後可登入控制台，使用「變更密碼」設定個人的管理員密碼。VM 測試只證明虛擬硬體環境；實體網卡、HBA、SMART 與 USB 開機須在相應硬體驗證。

