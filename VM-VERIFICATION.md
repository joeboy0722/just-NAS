# VM 應用驗證紀錄（進行中）

本文件記錄已發生的測試，不代表完整驗收通過。現有核心 Release 與 stores-v1 保持原樣，尚未發布 VM 測試包。

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

## 尚待完成

Ubuntu／Windows 安裝到磁碟、退出媒體開機、桌面操作、檔案持久化、NAT 與轉發、完整故障案例、三次外層冷開機、30 分鐘並行 NAS 功能檢查，以及乾淨環境的匿名線上重裝均尚待完成。

只有上述離線驗收通過後，才發布新的 GitHub Release 預發布包及 stores-v2 簽章目錄。核心候選僅本地交付，不覆蓋現有核心 Release。
