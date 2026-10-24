# just-NAS 開機畫面

開機的 OK 刻意用紅色、FAIL 用綠色；狀態文字反映真實結果，不會製造假失敗。核心網頁可用後顯示 `Welcome to Just NAS.` 並恢復正常配色：綠色成功、黃色提醒、紅色失敗。

- Alt+F1：品牌狀態；Alt+F2：本機維護登入。登入歡迎詞改為 just-NAS，Alpine 的上游授權仍保留。
- 驅動、池／掛載、設定、網路、分享、網頁與擴充各自回報結果。未建池／未設定分享／沒有擴充屬首次設定或略過，不等於失敗。
- 網頁必須實際監聽且本機健康請求成功，才宣告就緒。擴充在背景恢復，不阻塞核心服務。
- 設定的管理網卡置頂，列出其他啟用介面的 IPv4／IPv6。每兩秒更新、每頁最多四個位址、每五秒換頁，保留首要入口。
- 不列回環、link-local、veth、Docker 內部網路；一般 bridge、VLAN、bond 可顯示。網址存在不代表所有網段均可互通。
- 原憑證沒有涵蓋新增 IP 時會提醒，不會自動更換憑證或變更服務綁定。
- 開機參數 `just_nas_boot=plain`：正常配色的純文字逐行輸出；`just_nas_no_extensions=1` 仍可略過擴充。
- 維護日誌：`/var/log/nas-server.log`、`/var/log/nas-server-stdio.log`。當次啟動的無密碼狀態快照為 `/run/just-nas/boot.json`，不是公開 API。
- 序列終端保留純文字狀態與管理位址。硬體不支援 VT 或顯示時仍可使用序列終端；本版不宣稱全面實機顯示相容。

## 發布附件

rc2 使用五個手動附件：ISO、SHA256SUMS、LICENSE、SOURCES.md、支援資料包。建置資訊、套件清單、測試結果、操作文件、自有原始碼與完整授權包放進支援資料包；大型上游來源沿用 rc4 已校驗分卷，SOURCES.md 保留直接入口。

GitHub 自動產生的 Source code zip／tar.gz 仍會出現。擴充包的 stores-v1 固定網址保持可用。rc1 已撤下，不再重發。
