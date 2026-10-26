# 虛擬機管理器（開發候選）

此應用以原生服務運作，從應用商店安裝，不依賴 Docker。需要支援第二版套件格式的 just-NAS 核心；rc3 不支援，請使用本輪交付的相容候選映像。

## 安裝與資料

離線安裝包分成 `qemu-runtime-11.0.3-r0.jnpkg` 與 `vm-manager-1.0.0-rc2.jnpkg`。先匯入兩個套件，在管理器安裝視窗同意系統相依後依序安裝。QEMU、UEFI／BIOS、noVNC 與其相依不預裝在核心。

從「應用程式」開啟管理器後，選擇 VM 儲存池、上傳 ISO，再建立客體。VM 磁碟與 ISO 使用專用 ZFS 資料集；設定備份包含 VM 定義與套件參照，不包含大型磁碟或安裝媒體。關閉主控台或網頁不會停止客體。

預設 Linux 為 2 vCPU／4 GiB／32 GiB，Windows 10 為 2 vCPU／4 GiB／64 GiB。記憶體另計 QEMU 開銷，並保留 NAS 資源。磁碟容量代表虛擬上限，並不保證儲存池一定有足夠實體空間。

## 操作與限制

- 自動加速會先探測 KVM；不可用時使用 TCG。指定 KVM 時不會暗中回退，其他啟動錯誤也不會以 TCG 掩蓋。
- 主控台透過 NAS 管理員 HTTPS 登入與 WebSocket 代理，不公開 QMP／VNC 連接埠。登出會中止主控台連線。
- NAT 支援連外及 TCP／UDP 的 IPv4 連接埠轉發。需有 NAS 的連外路由與 DNS；只有 Host-only 網路的 VM 無法從客體連到網際網路。
- 修改 CPU、記憶體、TCG 快取、開機媒體、韌體或擴大磁碟前須先關機；NAT 轉發使用獨立即時設定入口。更換 BIOS／UEFI 可能使已安裝系統無法開機，擴大磁碟後仍需在客體內擴充分割區。
- 正常關機等待最多 60 秒，逾時由管理員決定是否強制關機；不自動強殺。重新啟動採正常關機後再次啟動。
- 移除預設保留資料，可重新登錄；永久刪除須輸入 VM 名稱。管理器更新、移除與 NAS 正常關機前會要求客體先停止。
- 第一版限管理員與可信客體，不提供橋接、實體裝置直通、任意 QEMU 參數、客體映像匯入、即時遷移或 Windows 11 TPM／Secure Boot。

TCG 是相容模式，不將它視為不可信客體的安全沙箱。QEMU 客體程序另外套用獨立 UID、檔案視野、無新增權限與 cgroup 資源限制。

## 驗證狀態

本版提供 stores-v2 預發布套件，需v2.1.0-rc4核心。Windows10 KVM手動安裝、桌面、正常關機及卸下ISO後磁碟開機已通過；完整Linux KVM安裝與30分鐘壓測未完成，Ubuntu24.04先前TCG安裝後黑畫面仍列限制。詳細記錄見VM-VERIFICATION.md。

`publish-vm-release.py`保留全面驗收的嚴格門檻；本次範圍較小的明確預發布由`release-preview.py`執行，必須記錄未完成項目，不產生虛假的全面通過報告。第三方來源及授權與套件一起提供，不包含客體ISO、磁碟或帳密。

## 本地設定候選：TCG 快取與即時 NAT

`1.0.0-rc2` 應用候選將 NAT 轉發獨立到虛擬機操作選單。執行中／暫停時可以新增、移除及改轉發目的埠，只有受改動規則的連線可能中斷。CPU、客體 RAM、TCG 翻譯快取與媒體設定仍需先關機。後端確認實際轉發表與保存結果，錯誤時回復原規則；若狀態不明，不顯示假成功。

進階設定提供 64／128／256／512／1024 MiB 翻譯快取，舊 VM 預設維持64 MiB。快取不是客體 RAM；增加部分會另行預留並計入 cgroup 上限，自動模式也會預留 TCG 回退所需容量。只用 KVM 時不使用此快取，且加大不保證提速。

本地候選封裝（不自動部署或發布）：

```powershell
python scripts/build-vm-packages.py --app-only --app-version 1.0.0-rc2 --binary output/vm-manager-settings-linux --output output/vm-settings-packages
```

真實 NAS 驗證結果見 VM-VERIFICATION.md；獨立無磁碟 QEMU 測試與介面假資料檢查不能代替 OS 安裝驗收。
