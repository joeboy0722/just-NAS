# 系統擴充與應用程式商店

開發基線為 `f2115af`（`v2.0.0-rc4`），功能分支為 `codex/optional-extensions-stores`。原 Release 與原 ISO 保留；新映像是本地開發候選，不自動發布 GitHub。

## 核心與選配

核心保留網路、儲存、USB、基本／伺服器／VM 顯示、電源與硬體監控。Python 是現有 NFS 套件的相依，不單獨移除；SMB、NFS、Restic、SMART 與權限工具仍可離線使用。

| 系統擴充 ID | 內容 |
|---|---|
| `gpu-nvidia` | nouveau 與既有 NVIDIA 韌體；不含 CUDA 或 NVIDIA 專有驅動 |
| `gpu-amd` | AMD／Radeon 核心顯示驅動與韌體 |
| `gpu-intel` | i915／xe 核心顯示驅動與韌體 |
| `audio` | 音效裝置、SoundWire 與專用韌體 |
| `media` | 攝影機、電視卡與影像擷取 |
| `ai-accelerators` | AI 加速器驅動與韌體 |
| `ssh-server` | 僅接受 Ed25519 公鑰的 root SSH 管理入口 |
| `container-engine` | Docker Engine、CLI 與完整離線相依 |

GPU 選配不包含圖形桌面、完整使用者空間加速函式庫或容器 GPU 直通。部分音效／媒體共用元件仍由鍵盤、顯示或主機監控使用，留在核心。拆分檢查同時檢查模組與韌體依賴，不能只按廠牌刪目錄。

## 管理員操作

1. 在「系統擴充」選擇擴充儲存池。系統建立專用資料集，核對 GUID 與實際掛載點；未掛載時拒絕寫入，不寫入 RAM 的空目錄。
2. 以「離線匯入」加入 `.jnpkg`，檢視來源、大小、權限、版本與相依後安裝；也可從已簽署的官方目錄下載。離線包與線上包使用相同驗證及任務流程。
3. 在「應用程式商店」匯入靜態網站示範。容器引擎未安裝時，可從已備妥的離線包或官方目錄一併安裝，須明確勾選。
4. 選擇 NAS 區網 IP 與未使用的連接埠。預設使用應用自己的資料夾；既有資料空間仍須允許應用 Unix 身分讀取／寫入，不會自動遞迴改擁有者或繞過檔案權限。
5. 應用可啟停、檢視日誌與開啟連線入口。靜態網站示範不含登入，內容在選定的區網位址開放。其他應用的登入由應用本身提供。
6. 移除應用預設保留資料；重新安裝須明確勾選使用保留資料。永久刪除資料另輸入應用名稱確認，有快照／複本相依時不強制刪除。
7. 驅動安裝、更新及停用於重新開機套用；停用並重開機後才能移除。仍有應用安裝時，不能移除或停用容器引擎。
8. SSH 須先設定自己的 Ed25519 公鑰。管理台提供伺服器 SHA256 指紋，登入帳號為 `root`；不開放密碼登入。

應用更新先停止服務並拍攝資料快照。失敗時保留失敗資料世代，以更新前快照複製回原位置並重建舊版服務。設定匯出包含套件參照、部署與 SSH 公鑰，不包含映像、應用資料或 SSH 私有主機金鑰；這些需要另外備份。

刪池預覽列出相關擴充、應用、分享與任務。刪除前先停止並移除相關容器，成功後清除關聯設定、排程、任務及告警。其他池上的資料保留；銷毀失敗則恢復設定與服務，不把驅動的待重啟狀態當成已完成。已啟用的驅動須先停用並重啟，才能刪除其儲存池。

## 套件格式與自製應用

`.jnpkg` 是 gzip tar。首筆 `package.json` 包含原始 `manifest` JSON 與 Ed25519 `signature`（base64），簽章涵蓋 manifest 原始位元組。`payload/` 的檔案逐筆核對 SHA256、長度與權限；不接受符號連結、越界或向前硬連結、重複路徑、setuid、安裝腳本或任意 Compose。相同內容可用指向先前已驗證檔案的內部硬連結去重。

官方目錄與套件必須通過公鑰驗證。自製應用／驅動顯示為非官方並要求管理員確認；原生 APK 系統能力只接受官方簽章。應用映像固定摘要，非 root 執行，禁用特權、主機 socket 與任意裝置，限制 CPU、記憶體與 PID，輪替日誌。

自製應用先準備 `payload/image.tar`（`docker save` 輸出）與 manifest。manifest 的 `app.image` 使用映像組態內容的 SHA256 摘要或可從離線映像解析的固定映像摘要，不使用浮動標籤。可參考示範映像及官方離線包內的 manifest。

```powershell
python scripts/build-extension-packages.py --manifest my-app.json --payload payload --output my-app.jnpkg
```

封裝工具補入格式、ABI、架構、檔案清單、大小與校驗值。手動封裝的應用是未簽署套件，不會自行變成官方項目。

```powershell
go run -buildvcs=false ./cmd/nas-package-check assets/store-keys/official.pub 'output/store-packages/*.jnpkg'
```

此檢查不安裝套件，可在 Windows 驗證實際離線包。

## 建置與發布

- 新候選檔案：`output/alpine-nas-stores.iso`。原 `output/alpine-nas-full.iso` 保留作 rc4 比對基線；首次建置需下載其相符映像及授權集合。
- 先建置核心以產生 `output/extensions-payload/`，再執行 `python scripts/build-extension-packages.py` 產生九個離線包與已簽署目錄；需 Python cryptography。
- 商店公鑰位於 `assets/store-keys/official.pub`，私鑰只存 Git 與 Docker 均忽略的 `secrets/stores-ed25519.key`。請另加密備份私鑰，勿隨測試輸出清理；缺少或不匹配時拒絕默默更換信任公鑰。
- 套件與目錄在 `output/store-packages/`。新 Docker 相依的對應來源另存 `runtime-corresponding-sources.tar.zst`；核心既有上游來源延用 rc4 的已驗證來源分卷。
- 預設線上來源是本專案 GitHub 的 `stores-v1` Release。此開發工作不發布該 Release；未上架或斷網時使用映像附帶的已簽署目錄，下載失敗明確顯示，可改用離線匯入。
- 正式上架需將目錄與其中列出的精確檔案一併發布，保留各套件授權及對應來源；不要只發布二進位包。just-NAS 自有程式與示範採 MIT，整個映像與第三方套件並非統一 MIT。

可開機參數 `just_nas_no_extensions=1` 略過擴充與應用恢復；核心管理仍可啟動。不相容的核心模組拒絕載入。Docker 明確使用 ZFS 後端，不採用未驗證的預設儲存後端。

## 驗收

`go test ./...`、`go vet ./...`、`scripts/test-split-modloop.py`、`scripts/stores-ui.cjs`、`scripts/verify-stores-vm.py` 與 `scripts/verify-stores-recovery.py` 分別涵蓋驗證器、拆分邊界、介面及 VM 流程。瀏覽器夾具檢查與真實 VM 檢查分開記錄。

只有 `output/vm-test/stores-test.vdi` 可供本輪初始化；先核對 SATA 插槽、檔案、UUID 與穩定裝置識別。現有資料碟不作破壞性測試，另保留 VM 快照 `pre-stores-20261024`。實機 GPU 顯示、特殊控制器、實體 SMART、USB 開機與斷電仍屬硬體驗收，不以 VM 結果替代。
