# NAS 開發建置主機（本地候選）

Docker 是應用商店選配的主機服務，不隨核心 ISO 安裝。Docker 卡片、圖標及依賴使用同一個 `container-engine`，不是 Docker-in-Docker。核心仍保留商店平台及其他 NAS 功能需要的共用核心能力；成品稽核拒絕 Docker/containerd/runc、建置工具、引擎離線包與容器映像混入核心。

## 本輪保護門檻

目前 NAS 的 Windows TCG 手動安裝保持不動。只修改本地來源、執行本地測試及建置候選；沒有部署 NAS、建立資料集、安裝 Docker、重啟主機或改 Windows Hypervisor。NAS 自行建置與 KVM 驗證均須另外完成，不能用本地 Docker 成功代替。

## Windows 本地候選

```powershell
./scripts/build-all-in-one.ps1 -DockerHost npipe:////./pipe/dockerDesktopLinuxEngine -OutputDirectory output/nas-build-host-local
```

產物與 baseline 複本在獨立目錄，不覆蓋目前 NAS 開機映像。Go 初始版本 1.25.6；NAS 用的工具鏈與 Alpine builder 基底均固定映像摘要。映像準備採目前已驗證的 Docker legacy builder 資源限制（已棄用，未來引擎移除此能力時須明確失敗並更新建置器）；建置工作為 2CPU/3GiB，SquashFS 2執行緒/512MiB。容器內有編譯工具，不安裝到 NAS 主系統。

## TCG 完成後才可以使用的遠端入口

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
