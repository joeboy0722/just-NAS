[**下載 just-NAS v2.1.0-rc4 ISO**](https://github.com/joeboy0722/just-NAS/releases/download/v2.1.0-rc4/just-NAS-v2.1.0-rc4-x86_64.iso)

家庭NAS預發布更新：原生VM應用、選配Docker入口與NAS自行建置。既有rc2、stores-v1及來源分卷下載保留。

## 本版內容

- 應用程式圖標入口、商店分頁、系統相依提示；Docker以服務圖標管理，引擎與QEMU／VM套件均不內建於核心ISO。
- VM管理器採QEMU，支援KVM；自動模式在硬體不可用時使用TCG，也可強制只用KVM。TCG翻譯快取可設定並計入記憶體限制；NAT轉發可於執行中修改並驗證／回復。
- NAS選裝Docker後可自行測試及建置ISO。已在Windows Docker停止時完整重建、自製ISO冷開機恢復服務、再次建置，產物校驗相符。
- 延續文字品牌啟動畫面、多IP管理網址、SMB／NFS、家庭檔案、ZFS、備份與既有管理功能。

## 套件與使用

[VM／QEMU／Docker官方預發布套件與簽章目錄](https://github.com/joeboy0722/just-NAS/releases/tag/stores-v2)。VM不依賴Docker；兩者皆由管理員選配安裝。Docker圖標目前只有引擎管理與日誌，任意容器管理使用管理員SSH。

## 實測範圍與限制

Windows10 KVM手動安裝、桌面、正常關機、卸下ISO後磁碟開機已實測。核心Go測試、套件簽章／對應來源、ISO離線依賴／授權、KVM能力、NAT熱更新與NAS自我建置均有紀錄。商店另驗證匿名下載、全新測試環境線上安裝；細項以支援包內實測報告為準。

仍是**預發布版**：完整Linux KVM安裝與30分鐘壓測未完成；先前Ubuntu24.04 TCG安裝後桌面黑畫面仍列已知限制。VM硬體驗證來自VirtualBox巢狀AMD-V環境，不代表所有實機硬體相容。Windows安裝媒體、客體磁碟與帳密不隨發布提供。

## 附件與授權

只有五個手動附件：ISO、SHA256SUMS、LICENSE、SOURCES.md、支援包。ISO內仍附授權；支援包集合來源、版本、建置／測試報告與操作文件。

just-NAS自有程式與文件採 **MIT**。整個ISO及第三方QEMU、Docker、OpenZFS、韌體等不是統一MIT，各自保留原授權。對應來源、校驗值與既有來源分卷直接連結見SOURCES.md。
