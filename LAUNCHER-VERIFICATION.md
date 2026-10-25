# rc3 應用程式入口驗證

本輪為本地候選，未推送分支或替換 GitHub Release。發布版下載仍為 rc2。

## 基線與產物

- 原程式基線：`b41ef42`；本地完整 Git bundle 位於 `output/before-app-launcher-20261025.bundle`。
- 功能提交：`9326f60`；導覽修正與文件：`01a8728`。
- ISO 內伺服器由乾淨的 `01a8728fa1e5cbf95e87f9bd47fdafa5ba441976` 建置；後續提交僅包含驗證腳本與成果文件。
- 候選映像：`output/just-NAS-v2.1.0-rc3-x86_64.iso`，298,844,160 bytes（285 MiB），與 rc2 相同大小。
- 原 VM 在測試前建立關機快照 `before-launcher-validation-20261025`；故障注入改用六顆獨立 VDI 的一次性完整複本，UUID 與路徑保存在忽略的測試目錄中。

## 已完成檢查

| 層級 | 結果及範圍 |
|---|---|
| Go | `go test ./...`、`go vet ./...` 通過；涵蓋批次容器狀態、權限隔離、圖標尺寸／格式／雜湊與入口 URL |
| 模擬 API 瀏覽器 | 25 項通過；亮暗主題四種寬度、長名稱、空清單、損壞圖標、導覽、草稿／焦點／選單保留、單一請求、背景暫停及資訊過期 |
| 相依檢查 | 缺少非 Docker 套件、錯版與循環相依阻擋；未同意不寫入；需重啟阻擋；相依任務失敗不提交應用；離線相依先完成 |
| VM 實際瀏覽器 | 10 項通過；新入口、同意流程、實際容器引擎與應用安裝、開啟後關閉分頁、啟停、簽章圖標更新、解除安裝保留資料與再利用 |
| 重啟 | 安裝登錄、圖標描述、容器狀態與網站可用性恢復 |
| 隔離故障 | 真實容器意外停止、重新啟動恢復、Docker 查詢逾時、掛載失效、拒絕向暫存目錄匯入及掛載回復均通過；全程原機關機 |
| NFS | 專用複本資料集的 NFSv4 讀取、寫入提交及再讀取比對通過 |
| 線上下載 | 複本暫加 NAT，取得實際網路上的簽章目錄、下載官方 Release 應用包並驗證；原機只有 Host-only，保留原設定 |
| NAS 回歸 | HTTPS、固定 IP、家庭入口、管理員／家人 session 隔離、SMB 讀取原檔與容量、私有資料拒絕存取，以及實際 ISO 掛載共 8 項通過 |
| 最終保存 | 原機回復測試前快照後掛載 rc3；擴充設定與應用登錄逐欄比對相同，原網路保留，臨時磁碟卸接，複本刪除，8 項 NAS 回歸再通過 |
| ISO | 內嵌二進位與建置檔逐位元比對、核心模組／韌體、BIOS／UEFI、離線 APK、必要指令及 ISO／執行中授權資料稽核通過 |

圖標新增的 Go WebP 解碼器使用已固定版本、已納入 Git 的 vendor 程式碼；授權與 PATENTS 同時放入 ISO 及執行環境。核心套件、驅動與容器依賴沒有新增預裝項目。

## 成果位置

- `output/launcher-review/browser-fixtures.json`：模擬測試，與真實 VM 結果分開。
- `output/launcher-review/live-browser.json`：VM 實際流程。測試代理僅監聽主機 loopback，驗證 VM HTTPS 憑證，網頁與 API 均來自 VM 映像。
- `output/launcher-review/live-*.png`：兩種主題、1920／1366／768／390 寬度的圖標頁與商店截圖，以及相依確認畫面。
- `output/vm-test/launcher-api-results.json`、`family-final-smoke.json`：重啟與 NAS 回歸。
- `output/vm-test/launcher-isolated-results.json`：實際故障注入；重試與每次白名單核對均留在紀錄，不以重複行數計算測試數量。
- `output/vm-test/launcher-isolated-regression.json`、`launcher-final-preservation.json`：NFS／線上下載與原機保存比對。
- `output/core-iso-audit/`：映像稽核；`output/rc3-server-build-info.txt`：實際建置資訊。

## 測試環境限制

VirtualBox 在本輪有數次 guest heartbeat 停頓與時間追趕，造成主機側 API 逾時；有用暫停／恢復讓測試複本恢復排程，根因尚未定位。這些逾時未算作通過，測試在服務恢復後重跑。驗證結果不代表硬體相容性或效能基準；沒有變更主機實體磁碟、Hyper-V 或驅動設定。

故障測試原先打算在原機快照分支執行，遭自動安全審查拒絕後，改成完整磁碟複本。測試腳本會核對原機關機、複本 VM UUID、每顆 VDI 的 UUID、路徑及無原機 backing disk，才允許注入。

ISO SHA-256：`4973e2b3a03cf7172c67f5c02bf3de42f67c66c1e4b70cd39d1e659e9e3c5f3f`。
