# VM 應用驗證紀錄（進行中）

本文件記錄已發生的測試，不代表完整驗收通過。現有核心 Release 與 stores-v1 保持原樣，尚未發布 VM 測試包。

2026-10-26 使用者調整本輪優先順序：先確認 TCG 下 Ubuntu 與 Windows 10 可安裝至虛擬磁碟、退出 ISO 後開機；略過系統更新及非必要套件安裝。30 分鐘壓測明確延後，待後續新增功能時一起執行。基本 OS 驗證不等同完整商店發布驗收。

## 環境與邊界

- 外層 just-nas：VirtualBox、4 vCPU／8 GiB；已保存關機快照與 Git 基線。
- VM 資料只使用白名單 128 GiB 動態 VDI 的專用 vmtest 池，原有 homepool 帳號、資料與分享保留。
- 已啟用巢狀虛擬化選項，但實際沒有 /dev/kvm。KVM 尚待實際環境驗證，不宣稱通過。
- Ubuntu 24.04.4 Desktop 與繁體中文標準 Windows 10 Pro（來源映像索引 3）；Tiny10 不代替正式媒體驗收。
- 自動安裝測試副本、測試憑證、帳密與客體磁碟只保存在忽略目錄，不進 Git 或發布附件。使用者原始 ISO 唯讀。

## 已完成的原型測試

| 項目 | 實際結果 |
|---|---|
| 官方簽章離線包 | QEMU runtime 與原生 VM 管理器成功安裝；未安裝 Docker |
| 原生應用入口 | 真實 NAS 圖標可進入整合介面，健康查詢正常 |
| TCG 客體程序 | BIOS 空白磁碟測試成功；這不代表 OS 安裝完成 |
| 隔離 | 實際 QEMU UID、NoNewPrivs、檔案視野與 cgroup 限制已檢查 |
| 大型 ISO | 原始 Ubuntu 約 6.7 GB、Windows 約 4.9 GB 透過媒體 API 串流上傳、校驗並登錄 |
| 主控台 | 實際 noVNC 連線顯示 BIOS 與 Ubuntu 安裝介面；關閉頁面不停止客體 |
| 登入撤銷 | 登出後既有主控台連線中止 |
| 介面 | 亮暗主題、1920／1366／768／390 寬度、四頁籤共 32 張畫面檢查通過，未發現整頁橫向溢出或未處理 JS 錯誤 |
| 程式檢查 | Go 全套測試與 go vet 已通過；後續修改仍須重跑 |

## 真實失敗與修正

首次 Ubuntu 桌面安裝尚未寫入系統磁碟時，QEMU 因單台 VM cgroup 記憶體超限被 Linux OOM killer 終止。NAS 管理仍可用；未以暫停／恢復掩蓋問題。

已限制 TCG 翻譯快取為 64 MiB，保留客體 RAM、QEMU 開銷與 NAS 保留量的準入檢查，並記錄程序退出結果及 cgroup memory.events。修正後正在重驗，不將尚未完成的 OS 安裝標成成功。[QEMU 快取設定說明](https://www.qemu.org/docs/master/system/qemu-manpage.html)

改用 Linux VirtIO 2D 顯示後，實際 noVNC 已可操作 Ubuntu live 桌面，未再出現該 cgroup OOM。原始 Ubuntu ISO 的 SHA-256 已核對 Canonical 官方清單一致。安全更新卡在相依計算；經使用者明確授權，在確認沒有 dpkg 安裝及套件檢查乾淨後，只結束該更新程序，安裝器隨後完成收尾及關機。

Ubuntu 已實際卸下 ISO，從虛擬磁碟啟動並回報 Ubuntu 24.04.4、TCG 與 GDM active，正常重新啟動及關機也已執行。但畫面仍黑，尚未通過已安裝桌面的視覺驗收；使用者要求限制這部分排查時間，暫列顯示相容性待查，不逕自歸咎上游或宣稱完整通過。QEMU 2D 後端需要客體軟體處理 3D，屬可能排查方向而非已確定原因。[QEMU 顯示說明](https://www.qemu.org/docs/master/system/devices/virtio/virtio-gpu.html)

Windows 10 安裝環境已在標準 VGA／TCG 下正常顯示。兩次自動安裝測試副本均被 ProductKey 回應檔驗證拒絕；省略該元素仍未解決，不將測試工具修改稱為修復成功。已停止處理自動安裝，改以原始 Windows.iso 手動選擇 Windows 10 Pro、不輸入產品金鑰，確認只有該客體新建的 64 GiB 空白磁碟後開始正式安裝。暫用 NAT 網卡斷線以避免更新，管理用 Host-only 網卡保持連線；原始媒體未修改。

測試環境的暫用 NAT 網卡有預設路由，但 NAS DNS 原本為空。已補上本次開機的暫用 DNS，未修改保存的管理網路設定；NAS 與 live 客體均已確認名稱解析。新版管理器另外提示缺少路由／DNS，並透過唯讀的專用 resolver 目錄同步網路設定，避免原子更新 resolv.conf 後執行中的客體仍使用舊 inode。此同步仍需新版套件實測。

QEMU 擴充更新時另找到 APK virtual 相依集合被縮減的問題，已修正並在 NAS 重新安裝成功。跨池刪除已補齊原生設定的交易紀錄、相關任務與告警清理，及其他池保留資料的預覽；兩個方向均加入回歸測試。

已實測缺少登入、缺少 CSRF、跨來源、同名媒體、無效媒體及中斷上傳拒絕。正常關機逾時後客體保持執行，須另行名稱確認才能強制關機。ISO 子資料集空間限制測試仍在改善失敗回報，不列為通過。

實際 NAS 匯入已拒絕無效簽章、未簽章原生應用、錯誤架構、不相容最低版本、未核准服務與截斷套件。Linux 版測試通過 FIFO／符號連結日誌防護；已補上 IO error 暫停仍占用資源及阻止重複啟動／編輯／移除的處理，仍待新版套件的真實故障驗收。

Ubuntu 安裝期間，原有 HTTPS、帳號隔離、SMB 檔案與 NFSv4 唯讀回歸通過；這些短期檢查不代替安裝完成後的 30 分鐘連續測試。

停止的空白 SMOKE-TCG 客體已驗證 4→5 GiB qcow2 真實容量擴大、禁止縮小、移除後保留資料及重新登錄，磁碟雜湊一致。沒有改動兩個 OS 安裝客體。

瀏覽器實測確認舊版在 ISO 子資料集空間不足時可能呈現連線中斷，而非明確空間錯誤；白名單配額已恢復，原有 ISO 未變。新版的子資料集掛載及容量檢查尚待部署後重驗，不把此項列為通過。介面焦點循環修正已在本機來源預覽配合真實 NAS API 通過八組亮暗／尺寸表單檢查；仍須重驗安裝包內的介面。

## TCG 開機驗證續測（2026-10-26）

使用者將本輪交付縮為 TCG 基本 OS 開機驗證與推送 `codex/vm-manager` 功能分支；KVM 暫擱置，不合併 main、不發布 Release。原每十五分鐘追蹤已依使用者後續指示取消，改為人工接續；不執行三十分鐘壓測。

標準 Windows 10 Pro 已完成原始媒體安裝、自動重啟及進入 OOBE，但尚未進入桌面或卸下 ISO。OOBEREGION、OOBEKEYBOARD 使用畫面提供的跳過選項接續；其後 OOBELOCAL 沒有跳過選項。客體 UnattendGC 錯誤日誌顯示 netprofm／Schedule 等待逾時（0x8007041D）、LocalUser plugin 失敗（0x80070490）及 appxsvc 啟動失敗（0x8007042C）。稍後唯讀查詢確認 netprofm、Schedule 已為 RUNNING，appxsvc 為 STOPPED；已使用 OOBE 重試按鈕重試一次，十五分鐘後仍回到 OOBELOCAL；本次 Windows 基本桌面與磁碟重開機驗收判定未通過，不再無限重試。尚未確認根因，不稱為正常安裝完成，也未繞過 OOBE 或強制重啟。 已發出正常 ACPI 關機；任務在 60 秒時回報逾時，但稍後客體自行正常停止，未使用強制關機。目前 Ubuntu、Windows 與 SMOKE 客體均已停止，磁碟與設定保留。

QMP 確認實際 TCG；準備階段多次讀寫計數有增加，沒有磁碟讀寫失敗或 cgroup OOM。這些只證明程序及 I/O 活動，不能取代桌面與退出媒體後開機驗收。

## 媒體疑慮與直接 Windows TCG 對照

既有 `Windows.iso` 已標註來源／映像內容有疑慮，暫停作為正式驗收依據；未證實檔案損壞。原檔 SHA-256 為 `7f4d86956f9e34c7f4c9389f949bc6045689759169c49a808ab7a91bf1c4564e`，與測試媒體相同；wimlib 1.14.5 對 boot.wim、install.esd 的 metadata、逐檔解壓與校驗均通過。兩者沒有額外整包 integrity table；此檢查不證明 Microsoft 原版來源，也不排除映像整合或已安裝系統的邏輯問題。

Windows 版 QEMU 已免安裝執行並確認 TCG。原始 ISO 的全新空白磁碟測試只進到安裝介面，尚未完成新安裝。另一組已安裝客體的獨立副本在直接 Windows TCG 下也重現 OOBEREGION；將翻譯快取由 64 MiB 改為 512 MiB 的獨立 overlay 後，區域與鍵盤頁正常，但帳號步驟仍出現 OOBELOCAL。不能將快取調整稱為完整修復，亦未完成同一凍結基線的64 MiB回測。Windows包11.0.0／NAS11.0.3、writeback／cache=none及宿主隔離差異均保留在本地對照紀錄。

依使用者指示已取消自動追蹤。下一輪改用重新取得的 Microsoft 官方媒體與全新虛擬磁碟；目前官方直接ISO下載被網站驗證拒絕，已取得並驗證Microsoft簽章的媒體建立工具，但電腦操作工具啟動失敗，需要使用者操作官方工具建立ISO。使用者其後已用官方工具完成新ISO：SHA-256 `041f2db7bacaf0334858e7972926c3b6b7e7a71b11c86460ee82684e25437f78`。新boot.wim、install.esd皆通過wimlib解壓／校驗，分別12849與75667個內部資料區塊校驗值集合與舊媒體一致；安裝映像blob table完全相同，XML只差最後修改時間。這降低媒體系統檔損壞的可能，不能用整檔雜湊不同認定舊ISO遭修改。新媒體已驗證，客體安裝及桌面仍未驗收通過，須以全新虛擬磁碟測試。

## 官方媒體乾淨安裝與外部問題追蹤（2026-10-26）

官方新媒體已開始真正的全新安裝對照：獨立 64 GiB qcow2、原廠 OVMF 變數、Windows QEMU 11.0.0、qemu64／2 vCPU／4 GiB、TCG 翻譯快取 512 MiB、writeback 與離線網路。沒有承接先前失敗系統的磁碟或 UEFI 狀態；只保留相同機型與韌體程式。已手動選定 Windows 10 專業版、無產品金鑰、自訂安裝，核對唯一 64 GiB 未配置測試磁碟後開始安裝；OS 安裝、OOBE 與磁碟重啟仍未通過。這不是與 NAS 11.0.3／cache=none／Linux 隔離完全相同的對照。

[Dell 官方 OOBE 說明](https://www.dell.com/support/kbdoc/en-td/000135587/the-windows-oobe-out-of-box-experience-fails-when-you-start-your-dell-pc-for-the-first-time)記錄初始化時序與內部等待問題，提供重試／重啟建議；本例舊客體重試失敗，不能直接認定同一根因。[Arch 原始問題 FS61506](https://bugs.archlinux.org/task/61506)曾在官方 Windows 10 1809 與 QEMU 3.1／KVM 重現相近錯誤，但其結案原因是無回應，並無確認修復，不作現行版本已有解法的依據。快取容量是[QEMU 已文件化的參數](https://www.qemu.org/docs/master/system/qemu-manpage.html)，目前只證明部分流程改善，未證明完整修復。

外層 NAS 重啟後另發現舊 prototype2 映像沒有保存之前 RAM 內的核心修正，導致 VM 管理服務未恢復；重新部署既有本地修正版後，launcher 與 VM 查詢正常，三台原客體均保持關機、資料保留。此次只恢復測試執行環境，沒有替換開機 ISO；仍須以正確候選映像另做冷開機驗證。

官方媒體乾淨安裝後續已正常通過區域、鍵盤與官方離線本機帳號／密碼確認，並提交三題測試安全問題；目前未重現舊客體的 OOBELOCAL。隨後觀察到客體自行重啟，尚未核對重啟原因或進入桌面；沒有送出強制重啟或繞過 OOBE，仍不列為基本驗證通過。依使用者外出前重新授權，已恢復每十分鐘自動接續與遇錯排查。

使用者手動接續後已提供 Windows 桌面畫面，並以同一 QEMU 程序的實際畫面再次確認桌面可見；這僅證明直接 Windows TCG 已進桌面，尚未完成卸下 ISO 後的重開機與檔案持久化驗證。依使用者要求開始把相同官方 ISO 上傳 NAS，另建乾淨客體供使用者手動安裝，不使用無人值守回應檔或自動送客體按鍵。NAS 仍為 QEMU 11.0.3／64 MiB TCG 快取／cache=none，直接 Windows 為 11.0.0／512 MiB／writeback，差異保留，不據此逕稱已確認根因。

NAS 手動對照已準備並啟動：`Windows 10 Pro Official Manual`，獨立新建 64 GiB qcow2／原廠 UEFI 變數、2 vCPU／4 GiB、實際 TCG，位於原白名單 vmtest 池。NAS 已接收與直接 Windows 測試相同的官方 ISO，大小 4,918,607,872 bytes、SHA-256 `041f2db7bacaf0334858e7972926c3b6b7e7a71b11c86460ee82684e25437f78`，伺服器結果與本機一致。未代送任何安裝按鍵，交由使用者手動安裝；原客體保留。直接 Windows 測試客體已在桌面後正常 ACPI 關機，程序退出，未強殺。

## 尚待完成

原 NAS Windows 客體受 OOBELOCAL 阻塞，失敗證據保留；目前官方新媒體的獨立乾淨安裝已由使用者接續至桌面，仍未完成退出媒體後開機驗證。Ubuntu 已完成磁碟安裝及啟動，桌面顯示尚待驗。完整桌面操作、檔案持久化、NAT 與轉發、故障案例、三次外層冷開機及匿名線上重裝仍待後續驗收；30 分鐘並行 NAS 檢查依使用者指示延後。

依最新指示，本輪只備份並推送功能分支；不合併 main、不更改預設分支、不發布 Release。完整商店發布驗收尚未完成，核心候選僅本地保存。

## 設定改善與使用者中止慢速安裝（2026-10-26）

本地增加 TCG 翻譯快取 64–1024 MiB 選擇；舊設定維持 64 MiB。加大部分同時納入準入與 cgroup 記憶體限制，自動模式保守預留 TCG 回退容量，KVM 不使用此快取。快取只能在關機時改動。NAT 轉發改為獨立設定，可在 running/paused 時透過 QMP 即時增減，未變規則不重建；比對實際 usernet 表後才保存，套用或保存失敗會回復，無法確認回復時明確報錯。Go 測試、vet、Linux 編譯及本地瀏覽器假資料測試通過（16 張亮暗／尺寸畫面）；真實 NAS 熱更新尚未驗收。

使用者其後明確要求停止 `Windows 10 Pro Official Manual`，不再等待 TCG 安裝最後步驟，並開始 OS 更新與自我建置。正常 ACPI 關機已提出，退出結果另行核實；本輪 NAS Windows 桌面與磁碟開機仍不可標示通過。未修改 Hyper-V/VBS/BIOS。候選部署、Docker 安裝及 NAS 自行建置結果於 NAS-BUILD-VERIFICATION.md 記錄。

### 實際 NAS 的設定改版驗收

已更新官方簽章的 VM 管理器 `1.0.0-rc2`，既有四台 VM 與資料保留。以白名單 vmtest 池內新建的一次性 256MiB 空白 BIOS 客體驗證：256MiB TCG 快取正確計入 cgroup 上限、TCP/UDP 執行中新增及移除、相同主機埠改客體目的埠、與 NAS HTTPS 8443 衝突拒絕、失敗後原設定保留、全程 QEMU 程序識別不變，以及移除後 listener 釋放，均通過。一次性客體已停止與清除，原四台客體保持關機；這不是 OS 安裝驗收。實測腳本 scripts/test-vm-live-settings.py，不在建置容器執行時運行。

## Windows KVM 手動安裝交接（2026-10-26）

依使用者要求新建 `Windows 10 Pro KVM Manual`（3f664b7af389818306c93ae2697e1a77），保留原TCG客體。CPU2、RAM4096MiB、磁碟64GiB、UEFI、pc-q35-9.2、NAT、標準VGA／e1000／AHCI與原手動測試相同，使用新空白磁碟與UEFI變數；加速設為只用KVM，不允許回退TCG。

先不掛ISO啟動空白客體，API確認running／actual_acceleration=kvm，QMP確認enabled=true，才正常退出空白QEMU並掛入原官方媒體Win10_22H2_zh-TW_x64_Microsoft.iso（SHA256 041f2db7bacaf0334858e7972926c3b6b7e7a71b11c86460ee82684e25437f78）。再次啟動並確認KVM後先暫停，避免交接時錯過光碟開機提示；使用者可從操作選單繼續執行、開啟主控台手動安裝。沒有發送安裝按鍵，也沒有開始自動安裝。TCG翻譯快取在KVM模式下不使用。OS安裝結果尚待使用者操作後驗收。

本機交接資料：output/kvm-host-test/windows-kvm-manual.json及windows-kvm-ready.png。
