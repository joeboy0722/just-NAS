# 儲存池刪除關聯清理

修復程式基線：`6a2ecdc`，分支 `fix/pool-cascade-delete`。修改前 Git 標記為 `before-pool-cascade-20261024`。

## 行為

刪池前會列出影響摘要，並要求輸入池名稱確認。成功刪除時，同步清除：

- 該池及其子目錄的 SMB／NFS 分享、授權與檔案入口空間。
- 該池的歷史任務、快照與配額排程、scrub、換碟、權限更新及以此池為目的地的備份恢復任務。
- 僅屬於此池磁碟的 SMART 任務與排程；如果實體裝置仍與其他池共用，保留其磁碟維護設定。
- 備份來源中的該池資料集。混合來源備份保留其他來源；若來源全部消失，刪除備份設定及 backup／backup-check 排程。獨立備份裝置上的歷史資料不會被擦除。
- 該池相關的健康、容量、配額、任務與磁碟告警，以及帳號指向該池的家目錄關聯。
- 池內資料集、快照、配額與權限等 ZFS 屬性隨 ZFS 池一起消失。

只匹配完整池名稱及子路徑，`tank` 不會誤匹配 `tank2`。其他池、全域帳號、SMTP、TLS、網路及管理員設定保留。全域稽核紀錄仍記下刪池結果。

## 一致性與保護

- 目前有背景任務執行時先拒絕刪除，提示等待或中止；不在任務尚未結束時強制銷毀。
- 暫停排程，封鎖該池新的網頁寫入，取消並等待原寫入結束，移除分享並關閉相應 SMB 連線後才銷毀。
- 清理前保存持久 undo／redo 日誌。銷毀失敗或服務套用失敗會恢復原設定。
- 中斷後開機依實際池 GUID 判定回復舊設定或完成清理；不會把同名但不同 GUID 的新池當成原池。
- 未完成交易時暫停設定修改，要求重啟恢復；不顯示假成功。
- 排程器會重新驗證已讀取的舊排程，防止刪除後以快取清單重新建立任務。
- 系統設定所在池仍保留既有刪除保護，此次未加入設定池遷移或整機重置功能。

## 驗證

`go test ./...`、`go vet ./...`、JavaScript 語法檢查及既有六個 UI 配額單位回歸通過。新增測試涵蓋：

- 跨池與同名前綴邊界、SMB／NFS 分享、舊任務告警、權限與裝置關聯。
- 混合來源備份保留、最後一個來源移除後清除設定與排程。
- 銷毀失敗、服務重載失敗、無法讀取保護資訊時不銷毀。
- 執行中檔案寫入停止與新寫入封鎖。
- 中斷發生在實際銷毀前／後的恢復、同名不同 GUID 拒絕恢復。
- 已移除、停用或變更的排程快照無法再啟動。

VM 實測只初始化全新白名單 VDI：`output/pool-delete-test/cascade-delete-fixture.vdi`，512 MiB。沒有使用既有資料碟或主機實體磁碟。

實測建立 `CascadeDeleteTest` 池、兩個資料集、SMB／NFS 分享、檔案、快照、六種歷史任務與六個排程，透過網頁確認預覽、取消，再正式刪池。立即檢查、重開機及再觀察 70 秒後，所有相關池、分享、資料集、快照、任務、排程與告警均不存在；其他池、帳號、分享、排程、備份和 SMB 讀取正常。臨時測試 VDI 已從 VM 移除，原虛擬碟路徑與 UUID 比對一致。

本機證據：

- [清理影響摘要](output/pool-delete-test/fixture.json)
- [VM 驗證紀錄](output/pool-delete-test/verification.json)
- [網頁刪除確認](output/pool-delete-test/deletion-preview.png)
- [網頁刪除結果](output/pool-delete-test/deletion-complete.png)
- [全域設定與 SMB 核對](output/ui-review/vm-final-verification.json)

交付 ISO：`output/alpine-nas-full.iso`；VM 實際掛載同內容的 `output/pool-cleanup-release/alpine-nas-full.iso`。前一版映像仍保留於 `output/ui-release/`。
