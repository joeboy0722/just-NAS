# Docker 選配與 NAS 自行建置驗證

日期：2026-10-26。分支：`codex/nas-build-host`。

**NAS 自行建置已通過：Windows Docker Desktop 關閉時可完整建置，NAS 自製 ISO 可冷開機，冷開機後可再次建置。KVM 與 Windows 客體安裝驗收不包含在這項結論。**

## 實際環境與結果

| 項目 | 結果 |
|---|---|
| 外層 NAS | VirtualBox just-nas，4 vCPU／8GiB，Host-only 管理連線保留 |
| Docker 選配 | 官方離線 container-engine 1.0.0；實際 Docker29.9.2，ZFS 後端，未內建於核心 ISO |
| 持久空間 | homepool/.just-nas-extensions/engine quota8GiB；vmtest/.just-nas-build quota32GiB；開始前 vmtest可用量超過40GiB |
| 建置限制 | 容器2CPU／3GiB，SquashFS兩執行緒／512MiB，私有 Unix socket，無 privileged 容器 |
| NAS 工具鏈 | 固定 Go1.25.6／Alpine映像摘要；完整 Go test、vet、Linux server編譯通過 |
| 成品稽核 | 142個APK；核心無 Docker/containerd/runc/build tools、引擎包及映像；硬體保留內容、核心來源、modloop簽章、離線套件、BIOS/UEFI及授權全部通過 |
| 不依賴 Windows Docker | 正常停止 Docker Desktop，明確指定本地主機 named pipe 的 docker info 失敗後，NAS仍能重建 |
| 自製 ISO 冷開機 | 原帳號、ZFS池、SSH、Docker及VM管理器恢復，不需要RAM程式修補 |
| 冷開機後重建 | 再次完整測試、建置、稽核及取回SHA256核對通過 |
| 核心服務 | HTTPS登入、testparm、Samba／NFS服務及ZFS診斷通過；本輪未重做大檔讀寫壓測 |
| VM設定改版 | vm-manager1.0.0-rc2已恢復；TCG快取預留及真實NAT熱更新已用一次性空白客體通過，詳見VM-VERIFICATION.md |

## 成功建置與產物

| 工作ID | 來源commit | Windows Docker狀態 | 說明 |
|---|---|---|---|
| f55a9a9734aa4940b16a0be7fde94144 | 6f8ac8b | 尚未關閉 | 首次完整NAS建置及稽核 |
| 4fbdb11c90384888a74341e591c6524f | edd5b53 | 已關閉且daemon不可用 | 完整重建；此產物已作NAS冷開機 |
| 684fde51debe4cb5b0a68b22f8ea9d1b | 1696396 | 維持關閉 | 自製ISO冷開機後再建置 |

三份成功映像均為298,844,160bytes（285MiB），SHA256相同：

`249d8c611f868fdd313809ece9553527ecd869c11fe081c0ceb5081f64afb138`

後兩次重用NAS持久建置快取；這是本次快取重建及冷開機測試的相同內容證據，不宣稱所有外部環境的全新建置必定可重現。最後產物位於 `output/nas-self-build/684fde51debe4cb5b0a68b22f8ea9d1b/`，包含ISO、SHA256SUMS、建置日誌、映像ID、版本清單與來源封包校驗碼。當前NAS啟動自4fbdb11c產物，與最後產物逐位元相同。

完整本機證據：`output/nas-self-build-preparation/verification.json`、`self-built-boot.json`、`windows-daemon-stopped.txt`及快照／加密設定備份。這些產物與私有資料不進Git。

## 保護與已修正問題

- 使用者要求中止過慢TCG安裝後，Windows客體收到ACPI正常關機要求，超過60秒後自行退出，沒有強殺。NAS的Windows桌面及磁碟開機仍未驗收；使用明確中止測試例外，不偽造通過證據。
- 更新前建立Git基線bundle、加密設定匯出與關機快照 `pre-nas-selfbuild-20261026-1850`（UUID9ddd9cdb-7994-42f6-b254-a2fbef67514a）。原四台客體及資料均保留。
- 首次嘗試因授權集合中的完整Go原始碼被 ./... 誤掃而中止：已補原始碼封包output/go.mod邊界，新增回歸測試。
- 首次無Windows Docker重建命中modloop快取後，因稽核依賴未保存的split-inventory暫存檔中止：改從實際ISO模組樹辨識唯一核心版本，未略過硬體或簽章檢查。失敗日誌保留，不計為成功。
- 建置前後皆檢查沒有客體程序。NAT測試只使用白名單vmtest池內的新建空白客體，驗收後清除，沒有改原Windows安裝。
- 實驗用第二張NAT網卡每次冷開機後以DHCP啟用並補測試DNS；這是測試網路配置，不是程式熱修補，管理用Host-only未變。
- 額外本地驗證：Windows Go test/vet、7項封裝／門檻回歸測試、27項商店mock流程、16張VM設定亮暗／尺寸畫面、獨立無磁碟QEMU及真實NAS NAT測試均通過。

## 尚未完成的項目

KVM、主機Hyper-V/VBS/BCD/BIOS切換、Windows客體安裝驗收及30分鐘壓測未完成。沒有發布Release或合併main。Docker圖標目前只有引擎管理與日誌；任意容器操作使用管理員SSH及私有socket CLI，不是完整容器管理面板。

交付狀態：NAS運行、Docker與VM管理器active，沒有客體或建置容器在執行；Windows Docker Desktop保持停止。
