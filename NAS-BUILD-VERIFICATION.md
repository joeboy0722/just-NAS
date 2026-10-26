# Docker 選配與 NAS 建置：第一階段驗證

日期：2026-10-26。分支：codex/nas-build-host。Git 基線本地 bundle：output/pre-nas-build-host-20261026.bundle。

## 已通過（本地）

- Windows Go 1.25.6 全套 go test ./... 與 go vet ./...。
- NAS 工具鏈相同的 Linux 容器（2CPU/3GiB）：全套 Go test/vet 及 Linux server 編譯。
- 瀏覽器 mock API 27項流程：Docker 商品與下載路由、系統相依流程、失敗不安裝、取消操作、真實回報狀態模型、圖標操作路由，以及亮暗1920/1366/768/390版面。這不是 NAS 實機驗收。
- Docker 圖標另有八張亮暗/尺寸截圖，目錄 output/launcher-review/docker-*.png。
- 本地腳本4項測試：來源敏感檔案邊界、預覽不連線、不完整TCG門檻拒絕、核心拒絕容器專用套件/服務/套件包並保留共用函式庫與第三方授權來源。
- 成品 ISO 稽核：無 Docker/containerd/runc/build tools、引擎離線包或容器映像；保留硬體內容、核心來源對應、modloop簽章、離線套件安裝、NAS必要工具、BIOS/UEFI及授權資料均通過。原始授權中的 docker 目錄屬來源資料，不能誤判為執行檔。
- 修正套件版本清單：使用成品抽出的 APK 清單，空清單拒絕交付。

候選在 output/nas-build-host-local，最終明確命名 ISO、SHA256SUMS與BUILD-INFO.json一起交付；其來源 commit 與校驗值以該目錄紀錄為準，不代表已在VM啟動通過。

## 明確未執行

本輪沒有連線更新、重啟、安裝套件或改動正在跑TCG的NAS，沒有代送安裝按鍵。未建立NAS建置資料集、未啟動NAS Docker、未關閉Windows Docker Desktop；未改Hypervisor/VBS/BCD/BIOS、未重啟Windows。

NAS實際安裝Docker、線上/離線安裝實測、完整自行建置、Windows本地daemon關閉時重建、新產物冷開機與再建置、KVM及其Windows安裝驗證均待前一階段門檻通過後執行。未發布Release、未合併main，不能將第一階段視為整個計畫完成。

## 2026-10-26：開始 NAS 自行建置

使用者決定中止過慢的 TCG 安裝後，客體先收到 ACPI 正常關機，超過 60 秒後自行退出，沒有強殺。設定已加密匯出，外層 NAS 關機快照 `pre-nas-selfbuild-20261026-1850` 已建立。候選 7dc118a 冷開機後 HTTPS、原帳號、儲存池、SSH 及 VM 管理器恢復，不需要 RAM 程式修補；所有客體保持關機。

Docker 選配離線包安裝成功：實際引擎 29.9.2、ZFS 儲存後端，engine 資料集 quota8GiB。首次 NAS 建置在 Go 測試階段中止：公開授權集合內的完整 Go 原始碼被 ./... 誤掃，已補原始碼封包內 output/go.mod 邊界並加入回歸測試。失敗日誌保留，沒有標示映像成功。

第一份 NAS 自製 ISO 已通過完整稽核並取回核對 SHA256，大小285MiB。Windows Docker Desktop 隨後正常關閉，本地主機 daemon 連線失敗已記錄。第二輪命中 modloop 快取後，稽核因依賴上一輪 extensions-payload/split-inventory.json 暫存檔而中止；改為直接從實際 ISO 解出的模組樹辨識唯一核心版本，保留所有原有硬體、韌體及簽章檢查，並新增不依賴拆分清單的回歸測試。第二輪失敗不作無 Windows Docker 的完整成功證據。
