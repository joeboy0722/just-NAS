# NAS 開發建置主機（本地候選）

Docker 是應用商店選配的主機服務，不隨核心 ISO 安裝。Docker 卡片、圖標及依賴使用同一個 `container-engine`，不是 Docker-in-Docker。核心仍保留商店平台及其他 NAS 功能需要的共用核心能力；成品稽核拒絕 Docker/containerd/runc、建置工具、引擎離線包與容器映像混入核心。

## 本輪保護門檻

第一階段已完成本地候選。2026-10-26 使用者要求停止過慢的 TCG 安裝、開始更新 NAS 與自我建置；這項變更不代表 Windows 安裝驗收已通過。先正常關閉全部客體、備份與關機快照，才部署候選。Windows Hypervisor 與 KVM 切換仍保持後續門檻，不能用本地 Docker 成功代替 NAS 建置。

本輪實際自我建置、關閉 Windows Docker 重建及自製 ISO 冷開機後再建置已通過，見 [驗證紀錄](NAS-BUILD-VERIFICATION.md)。實驗用第二張 NAT 網卡在冷開機後需重新啟用 DHCP／測試 DNS；不影響既有 Host-only 管理設定。

## Windows 本地候選

```powershell
./scripts/build-all-in-one.ps1 -DockerHost npipe:////./pipe/dockerDesktopLinuxEngine -OutputDirectory output/nas-build-host-local
```

產物與 baseline 複本在獨立目錄，不覆蓋目前 NAS 開機映像。Go 初始版本 1.25.6；NAS 用的工具鏈與 Alpine builder 基底均固定映像摘要。映像準備採目前已驗證的 Docker legacy builder 資源限制（已棄用，未來引擎移除此能力時須明確失敗並更新建置器）；建置工作為 2CPU/3GiB，SquashFS 2執行緒/512MiB。容器內有編譯工具，不安裝到 NAS 主系統。

## NAS 遠端入口與明確中止測試

先正常關機並確認 TCG Windows 的桌面、卸下ISO後磁碟開機及資料保留，再保存證據 JSON：`environment` 必須是 `nas`，`desktop`、`disk_boot`、`normal_shutdown` 都必須是布林 true，並記錄客體 ID 與 ISO 雜湊供人工核對。未做的項目不可填 true。安裝本地候選、備份原映像與設定後，從商店選裝 Docker，確認擴充的 ZFS 後端。

預覽完全不連線：

```powershell
./scripts/build-on-nas.ps1
```

真正執行需提供既有管理員 SSH 私鑰、已核對的 known_hosts、實際 vmtest pool GUID 與驗收檔；私鑰不進原始碼封包：

```powershell
./scripts/build-on-nas.ps1 -Execute -Identity <私鑰路徑> -KnownHosts <已固定主機金鑰檔> -PoolGuid <vmtest的GUID> -TCGEvidence <驗收JSON路徑>
```

來源必須是乾淨的已提交 Git 版本。只傳 Git 追蹤且通過排除規則的檔案，另外附匹配的公開授權集合與未修改 baseline ISO；不傳 output 測試資料、客體、帳密或簽章私鑰。來源封包與取回 ISO 都驗 SHA256。不呼叫 Windows Docker，也不提供本地 daemon 回退。

管理員 worker 限定 vmtest/.just-nas-build、固定掛載點與使用者提供的 pool GUID；沒有正確掛載即停止。建置資料集 quota 32GiB、vmtest 需至少40GiB可用；引擎資料集固定 homepool/.just-nas-extensions/engine、quota8GiB且homepool至少12GiB可用。發現客體程序則拒絕建置；不強殺客體、不移除既有資料或自動清快取。Docker 僅用私有 Unix socket，原始碼唯讀掛載，不公開 socket、不用 privileged 容器。

結果存於 `output/nas-self-build/<runID>`，含 ISO、校驗碼、builder image IDs/摘要、建置日誌與來源修訂。NAS 工作目錄保留作診斷。成功標記僅代表建置／成品稽核，仍需新 ISO 冷開機及再次建置驗證。

## 後續主機切換

只有在 Windows Docker Desktop 關閉時，NAS 能完整重建、產物冷開機恢復服務且再建置通過後，才盤點 Windows 的 Hypervisor/WSL2/VBS 等依賴並備份 BCD。新增單次選用的無 Hypervisor/VSM 開機項目，原項目與原預設保留。不改 BIOS/Secure Boot、不刪 WSL。重開機時間需與使用者協調。

KVM 驗收必須核對 VirtualBox 原生 AMD-V（非NEM）、NAS /dev/kvm 的實際建立能力與受限客體 QMP `enabled=true`；強制 KVM 失敗不得改 TCG 偽裝通過。保留 TCG、KVM、建置及冷開機各自結果，不發布 Release、不做30分鐘壓測。

### 使用者決定中止 TCG 測試

若使用者明確決定不再等 TCG 安裝，可使用 `-AllowIncompleteTCG`（Python：`--allow-incomplete-tcg`）繼續 NAS 建置。證據仍必須如實記錄 `environment: nas`、`outcome: interrupted_by_user`、`machine_id` 及 `normal_shutdown: true`。桌面與磁碟開機未驗收就填 false；缺少正常關機證據仍拒絕執行。worker 額外檢查沒有 QEMU 客體程序，不以這個例外略過資源或儲存檢查。

### Docker 操作入口

Docker 圖標目前提供引擎狀態、啟停、日誌與移除，不是通用容器管理面板，也沒有網頁終端機。NAS 自行建置透過管理員 SSH 執行 Docker CLI，以 `DOCKER_HOST=unix:///run/just-nas-docker.sock` 操作 NAS 私有 socket；Docker API 不對網路公開。一般應用仍只能使用商店核准的部署流程。

### KVM 基本 OS 驗收作為建置前置證據

TCG測試中止後，Windows10 KVM已完成手動安裝、桌面、正常關機與卸下ISO磁碟開機。Python入口可使用別名 `--guest-evidence` 指向如實記錄 `environment: nas`、`acceleration: kvm`、`desktop/disk_boot/normal_shutdown: true` 的基本驗收檔；不把它稱為完整壓測或TCG通過。建置前仍要求所有客體停止，儲存及資源檢查不變。

### 隔離建置主機

可指定 `--host`、`--ssh-port`、`--pool-name`、`--pool-guid`、`--engine-dataset`及`--engine-guid`，將建置限制在新的隔離NAS。自訂引擎必須提供實際GUID，仍核對DockerRootDir、ZFS後端、掛載來源、40GiB可用量及配額，不接受任意主機路徑。核心發布採隔離測試機完成，不要求停機或修改原NAS下載網路。
