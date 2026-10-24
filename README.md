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

VM 使用 Host-only 網路，已確認固定 IP `192.168.56.101`，並保留同位址的 DHCP reservation 供回復測試。

- 管理頁：`https://192.168.56.101:8443/`（HTTP 8080 導向 HTTPS；請先核對並信任 NAS 公開憑證）
- 檔案總管網址列：`\\192.168.56.101\Family`
- SMB 帳號：`family`，使用 SMB 密碼，與管理頁密碼獨立。
- NFS：`192.168.56.101:/mnt/homepool/LinuxShare`，僅允許主機 `192.168.56.1`。

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

分享目標必須位於已掛載的資料集內。新建個人／公共空間使用獨立 Unix UID 與 POSIX ACL：個人目錄只供本人存取，公共空間依帳號設定唯讀或讀寫。舊版分享仍使用 nobody，請用新受管理空間建立隔離。NFS 使用 IP／網段限制並映射為 nobody，預設不允許 root 特權。

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


## 家庭 NAS v2.0.0-rc2 候選版本

先保留 Git 本地基線 `baseline-working-nas`；功能分支為 `feature/family-nas`。程式備份不等於 NAS 資料備份。候選版本的逐項驗收狀態見 [IMPLEMENTATION.md](IMPLEMENTATION.md)。

管理員在「檔案共享」建立檔案帳號，再選「建立個人／公共空間」指定池、擁有者、配額或公共存取權。每個人的個人空間應各建一個資料集。管理員密碼與檔案帳號分開；同一組檔案帳密供 SMB 與 `/files.html` 使用。分享可編輯、停用；名稱和協定固定，改名需另建分享。移除分享與帳號不會遞迴刪除資料。刪除帳號後 UID 與名稱不重用，避免新帳號繼承舊回收或歷史權限；停用帳號則可重新啟用。

網頁檔案入口支援串流上下傳、資料夾、同空間移動、私人回收區及單檔歷史恢復。上傳預設不覆蓋同名內容；中斷不發布半成品，殘留暫存於 48 小時後清理。回收內容保留 30 天，只可由原帳號經入口存取。

### 配額與保護

新資料集使用 ZFS project quota，保留刪檔所需的資料集元資料空間。調低到現有用量以下時不刪除檔案；可以讀取、下載、移入回收區及永久刪除，拒絕增加資料的操作。提高配額或釋放足夠空間後解除限制。SMB 反映 ZFS 的區塊配額與非同步記帳，用量不等於檔案總邏輯大小；快照占用另列。

超額滿 30 天，排程先清理該空間已到期回收內容，再由最久未修改的一般檔案開始刪除，降到配額內。近期回收內容及管理員手動快照保留；快照仍可能占用池空間。到期清理是永久刪除，會寫入稽核，可在背景任務中止。配額無法代替池剩餘容量監控。

每個新受管理空間建立每小時／每日／每週快照與配額檢查排程，預設保留 24／14／8 份。維護頁可修改間隔、保留份數或停用。清理只刪有系統標記的自動快照。歷史恢復寫到新位置；整體回滾須由管理員確認。

### 備份與恢復

第一版只掛載管理員指定 UUID 的既有 ext4 備份裝置，不格式化插入的硬碟。備份目的地須與來源池位於不同裝置，並核對儲存庫 ID；碟缺失或 UUID 不符即停止，不退回 RAM 目錄。建立 Restic 儲存庫時請離線保存密碼。

備份使用資料集快照及一致的加密設定匯出，預設每日執行，保留每日 7／每週 4／每月 6 份，每月完整讀取驗證。恢復指定備份 ID 與恢復用資料集，在新目錄以 Restic verify 核對內容。管理員可用「下載恢復結果」取回 TAR，在電腦的新資料夾解開、比對後，再用檔案帳號複製回分享。不要直接覆蓋唯一資料副本。

控制台可匯出／匯入加密設定，匯入會驗證內容並重新啟動。完整設定採有版本、校驗碼的世代切換；不相容舊映像不能修改新版設定。更新前保留映像、SHA256、設定匯出及離線密碼，使用完整 ISO 替換並驗證；不要直接降版覆寫設定。設定格式不相容時先用相容舊設定備份及舊映像恢復。

### 維護與 HTTPS

維護頁提供背景任務、排程、告警、稽核、診斷下載、服務重新啟動、scrub、SMART 短測試及鏡像成員替換。SMART 任務的成功表示測試指令已接受，實體結果需再查 SMART 頁；虛擬磁碟通常不支援。替換原成員可用完整裝置路徑或池內 GUID，新碟必須空白且未被使用，不執行強制清碟。

網路目前管理主要介面 eth0，固定 IP 套用後須在 90 秒內從新位址登入按「確認網路」，否則回復。其他介面提供狀態查看。

HTTPS 於 8443 監聽。初始設定後請下載公開憑證、核對 NAS 身分，於自己的用戶端信任或匯入自有憑證，再啟用 HTTP 導向。不要把私鑰匯入瀏覽器，也不要將此開發 VM 對外公開。SMTP 預設關閉；未設定郵件仍保存控制台告警。

### 家庭版本驗收

```powershell
python scripts/verify-family.py --project
python scripts/verify-family-advanced.py quota
python scripts/verify-family-advanced.py backup
python scripts/verify-family-advanced.py accounts
python scripts/verify-family-system.py snapshots
python scripts/verify-family-system.py shares
python scripts/verify-family-system.py files-edge
python scripts/verify-family-system.py settings
python scripts/verify-family-system.py network
python scripts/verify-family-system.py nfs
```

上述腳本只使用專用 homepool 測試空間，設定／網路測試會重開 VM。建池腳本會驗證 family-project-a/b 等 VDI 白名單，故障換碟後需先核對新拓撲，不可略過檢查直接重建。所有測試密碼與報告位於被 Git 忽略的 output/vm-test。

交付映像與結果見 IMPLEMENTATION.md。一般唯讀檢查使用 `python scripts/verify-family-final.py`；故障演練後不可直接重跑初始建池腳本。

## 全介面改版

亮暗雙主題、帳號權限勾選、引導式設定與新版家庭檔案入口，操作說明及驗收紀錄見 [UI-REFRESH.md](UI-REFRESH.md)。
