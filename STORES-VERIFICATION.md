# 系統擴充與應用商店：v2.1.0-rc1 驗證

分支 `codex/optional-extensions-stores`，基線 `f2115af`／`v2.0.0-rc4`。程式已推送 GitHub main；擴充包先上架 stores-v1 並通過真實下載／安裝，再發布本體預發布版 v2.1.0-rc1。原有帳號、Family 與 homepool 資料保留。測試前保留 VM 快照 `pre-stores-20261024`。

## 實際大小

| 映像 | 位元組 | MiB |
|---|---:|---:|
| 保留的 rc4 | 472,907,776 | 451.00 |
| 商店候選 | 298,844,160 | 285.00 |

縮減 **36.81%**。候選 `output/alpine-nas-stores.iso` 的 SHA256 為 `dcafe6400b1f2e66091e1b425c4e6adb457dcc224d6b779a0089b8f7361698e5`。原 ISO 未覆蓋。

核心離線 APK 由 146 減至 142，移出四個 SSH 套件；Docker 不在核心。modloop 拆出 3,833 個選配檔案，保留項目除依賴索引外與 rc4 位元組一致。保留模組未引用移出的韌體，基本顯示後備、跨功能共用模組與必要韌體仍保留。

| 離線包 | 下載大小 |
|---|---:|
| `gpu-nvidia` | 107.10 MiB |
| `gpu-amd` | 35.53 MiB |
| `gpu-intel` | 13.21 MiB |
| `audio` | 17.72 MiB |
| `media` | 12.38 MiB |
| `ai-accelerators` | 5.10 MiB |
| `ssh-server` | 6.58 MiB |
| `container-engine` | 68.92 MiB |
| `static-site` | 2.30 MiB |

九個包與簽署目錄位於 `output/store-packages/`，已逐包檢查實際檔案雜湊及 Ed25519 簽章。NVIDIA 為 nouveau／既有韌體，不含 CUDA、專有驅動或容器 GPU 加速。

## 已通過

| 範圍 | 結果與證據 |
|---|---|
| Go／封裝 | `go test ./...`、`go vet ./...`、拆包邊界測試與九個實際套件驗證 |
| 映像 | 實際嵌入伺服器／目錄／公鑰比對、modloop 簽章、保留驅動依賴、離線 APK 安裝、BIOS／UEFI、SMB／NFS／Restic／SMART 工具及授權；`output/core-iso-audit/` |
| 瀏覽器 | 20 個夾具檢查；真實 VM 登入、更新設定預填、背景更新保留草稿、取消不更動部署；亮暗 × 1920／1366／768／390，共 16 張真實截圖；`output/stores-review/` |
| 離線安裝 | 僅 host-only 網卡的 VM 安裝 Docker、音效、SSH 及網站；Docker 實際回報 ZFS；驅動重啟載入、停用重啟後移除；`stores-install.json`、`stores-app.json`、`stores-reboot.json` |
| 應用限制 | 實際 inspect 非 root、cap-drop、no-new-privileges、唯讀根／內容、CPU／記憶體／PID 限制；啟停、日誌、保留資料再安裝、引擎相依阻擋；`stores-recovery.json` |
| 更新復原 | 故意失敗更新後舊應用與 proof 檔可讀，失敗資料保留；停止中的應用更新後仍停止，IP／連接埠保留；`stores-recovery.json`、`stores-stopped-update.json` |
| SSH／設定 | 公鑰登入、固定主機公鑰、拒絕無公鑰登入；加密設定匯出保留部署與 SSH 公鑰；`stores-recovery.json` |
| 維護失敗 | 容器崩潰顯示真實失敗；忙碌池銷毀失敗後恢復設定、引擎、應用及 NFS；`stores-maintenance.json` |
| 缺少擴充碟 | 冷開機管理與 SMB 正常、拒絕寫入空掛載點、正常關機，接回原 VDI 自動恢復引擎／SSH／應用；`stores-missing-pool.json` |
| 刪池連動 | storetest 刪除後擴充、應用部署、相關任務與 StoresNFS 分享同步清除，homepool ONLINE；`stores-cleanup.json` |
| 公開下載 | 九個套件全部從 GitHub Release 匿名下載，大小與 SHA256 全數一致，目錄 Ed25519 簽章正確；`output/release-download-verification.json` |
| 真實線上安裝 | VM 暫加 NAT，實際目錄來源為 network，下載並安裝引擎／應用、網站回應正常，Docker 為 ZFS；另驗證驅動下載。最後清理測試池並還原網路；`stores-online.json` |
| 清除後重啟 | 擴充／應用設定保持空白，SSH 與示範連接埠未監聽；`stores-final-core.json` |
| 原功能 | 最終映像靜態 IP、HTTPS、家庭登入、session 隔離、SMB 原始資料／容量／私有拒絕；NFSv4 於專用測試資料集寫入、比對並還原；`family-final-smoke.json`、`stores-maintenance.json` |

上述 JSON 位於 `output/vm-test/`。相關腳本為 `scripts/verify-stores-*.py`、`scripts/stores-ui.cjs`、`scripts/stores-live.cjs`。程式測試另涵蓋損壞包、無效簽章、路徑穿越、越界連結、錯版核心、缺少掛載、相依與容器限制；不將程式測試冒充實機驗收。

早期驗證使用 `output/vm-test/stores-test.vdi`（UUID `68254ab5-dfeb-41dd-af5d-b6cc857eaf2c`）。線上驗證另建白名單 `stores-online-test.vdi`（UUID `e09cfe39-4a32-45d8-a42f-e2db132c9016`），兩者均為 SATA 2 的 4 GiB 專用虛擬碟；測試後恢復原掛載。未直通、初始化或格式化主機實體硬碟。測試池最後清除，原有資料與帳號保留。

## 授權、來源與驗收界線

- 自有程式與示範採 MIT；整個 ISO／選配包包含各自授權的第三方元件，不是統一 MIT。包內附授權及來源資訊。
- 新引擎相依對應來源：`output/store-packages/runtime-corresponding-sources.tar.zst`；不變的核心、韌體與工具鏈延用 rc4 已收集且驗證的來源分卷。發布時須一併保留。
- 官方來源固定本專案 `stores-v1` Release，目錄／套件皆驗簽。已公開上架並通過全部九包匿名下載及 VM 真實線上安裝。內建簽署目錄、斷網錯誤與完整離線安裝亦已驗證。
- 實體 GPU 顯示接手、特殊 HBA／網卡、USB 開機、SMART 及斷電仍待硬體驗收，VM 不替代實機。
- 真正磁碟耗盡與任意時點斷電矩陣仍待故障注入。現有空間檢查、持久任務中斷辨識與缺池／失敗更新復原，不代表所有故障點均驗收通過。
- 救援參數 `just_nas_no_extensions=1` 有解析回歸測試；完整實機救援啟動仍待驗。

## 里程碑提交

| 提交 | 內容 |
|---|---|
| `a15c50f` | 精確拆包與離線包建置 |
| `3e9d0e7` | 簽章、持久擴充與容器生命週期 |
| `4a7eb09` | 兩個商店與示範應用 |
| `32e5e2a` | VM 整合、無狀態根、SSH、健康狀態 |
| `d4f968b` | 更新保留設定、簽署目錄與驗證 |
| `d2ef9f3` | 映像精確內容比對 |
| `d24601b` | 持久套件參照安全檢查 |
| `039f756` | 缺池關機與救援參數；初次候選二進位來源 |

目前映像二進位來源為乾淨提交 `d6e34cd`。線上驗證腳本為 `scripts/verify-store-downloads.py` 與 `scripts/verify-stores-online.py`；發布順序固定為擴充 → 匿名下載／VM 安裝 → 本體。

操作說明見 [EXTENSIONS.md](EXTENSIONS.md)。這是預發布候選，未宣稱全面實機相容。
