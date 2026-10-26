# Windows just NAS mode 與 KVM 驗證

2026-10-26，分支 codex/nas-build-host。NAS自行建置及自製ISO冷開機已通過，見NAS-BUILD-VERIFICATION.md。

## 重開機前結果

- Docker Desktop已停止，wsl --shutdown已執行，沒有WSL客體。
- Windows HypervisorPresent=true、VBS狀態2；VirtualBox使用NEM，日誌明確警告不能啟用巢狀VT-x/AMD-V。
- NAS的kvm-amd模組存在，但載入回報Not supported，dmesg顯示SVM not supported；/dev/kvm不存在。尚不能取得KVM，需換開機模式測試。
- 系統碟BitLocker保護Off、FullyDecrypted；未發現Credential Guard／HVCI的UEFI鎖定旗標。沒有更改BitLocker、Secure Boot、BIOS或全域安全政策。
- 已建立永久 **just NAS mode**，GUID `{973d64de-d131-11f1-ac25-f889d27a71a3}`；僅新項目的hypervisorlaunchtype與vsmlaunchtype為Off。
- 原預設Windows `{845e1634-5931-11ee-bef0-ca35d646a187}`保留，選單追加新項目、停留15秒；未設定一次性bootsequence。
- BCD已備份至output/kvm-host-test。NAS內客體及容器均停止，NAS已透過正式流程同步／匯出池後正常關機。

## 重開機後接續

1. 在Windows開機選單選 **just NAS mode**。建議使用重新啟動切換模式。
2. 回到專案執行 `python scripts/verify-kvm-host.py`。它先確認Windows Hypervisor不在運行，再啟動正常關機的NAS，不改其硬體、ISO或原資料。
3. 在白名單vmtest池建立一次性空白客體，只用KVM，必須QMP query-kvm enabled=true；不允許TCG回退冒充。完成後清除一次性客體，保留原客體。
4. 證據保存output/kvm-host-test/after-reboot.json。若失敗，讀取新VirtualBox日誌與NAS模組錯誤，不自行更改BIOS或全域政策。

**重開機後的 KVM 取得與受限客體啟動已通過；完整作業系統安裝仍待驗。** 本輪不重裝Windows客體、不做30分鐘壓測、不發布Release。

## 模式與恢復

just NAS mode下WSL2、Windows Docker Desktop及依賴Hyper-V/VBS的功能不適用；NAS內Docker不受影響。需要Windows虛擬化功能時，重新啟動選原Windows項目即可。

若要移除新模式，以管理員權限核對GUID後執行 `bcdedit /delete {973d64de-d131-11f1-ac25-f889d27a71a3} /cleanup`；原預設從未替換。BCD備份及前後輸出都保存在output/kvm-host-test，不進Git。最初WMI讀取失敗時沒有修改BCD，已改成核對實際BCDEdit輸出。

## 2026-10-26 重開機後實測：通過

- Windows HypervisorPresent=false，VirtualizationBasedSecurityStatus=0。
- VirtualBox日誌為 `HM: HMR3Init: AMD-V w/ nested paging`、`AMD-V nested-guest init`、`Using AMD-V implementation 2.0`，已不靠NEM提供此次虛擬化。
- NAS存在/dev/kvm，kvm_amd與kvm模組載入成功。
- 白名單vmtest池的一次性256MiB空白BIOS客體，指定只用KVM，經原本受限UID／cgroup／檔案隔離啟動，API actual_acceleration=kvm、QMP query-kvm enabled=true。沒有TCG回退。
- 一次性客體已停止並移除；原四台VM及資料保留。NAS維持運行，原客體未啟動。
- 證據：output/kvm-host-test/after-reboot.json及virtualbox-after.log。這只證明加速取得與空白客體啟動，不代表Windows/Linux完整安裝或穩定性壓測通過。
