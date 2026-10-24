# 系統擴充與應用程式商店

開發基線：`f2115af`（`v2.0.0-rc4`）。實作位於 `codex/optional-extensions-stores`；舊 Release 保留。

核心保留網路、儲存、USB、基本與虛擬機顯示、電源及硬體監控。選配項目為 GPU 原生支援、音效、媒體擷取、AI 加速器、SSH 與容器引擎。所有安裝由管理員操作；不操作主機實體硬碟。

## 套件格式

`.jnpkg` 為 gzip tar，首筆 `package.json` 包含 `manifest` 與 Ed25519 `signature`（base64）。簽署原始 manifest JSON 位元組。其餘檔案為 `payload/` 下的 regular files；相同內容可使用指向先前已校驗檔案的內部硬連結以避免韌體重複，逐檔 SHA256、長度與權限須符合 manifest；不接受符號連結、越界連結、腳本或任意 Compose。自製應用／驅動須確認信任；原生 APK 能力只接受官方簽章。

離線包包含完整相依與映像。未配置持久儲存不允許安裝。應用移除預設保留資料。

## 驗收紀錄

尚在開發；未完成的 VM／硬體驗收不得宣稱通過。
