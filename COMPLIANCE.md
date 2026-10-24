# 第三方授權與對應來源

just-NAS 自有程式及文件採 MIT；ISO 是含多種授權元件的發行映像。根目錄 LICENSE 不會取代第三方的 GPL、LGPL、CDDL、BSD、Apache 或韌體廠商條款。

## v2.0.0-rc4 附件與映像內容

- ISO 根目錄：`LICENSE`、`THIRD-PARTY-NOTICES.md`、`THIRD-PARTY-LICENSES/`。
- 開機後：`/usr/share/licenses/just-nas/`，第三方資料在其 `third-party/` 子目錄。
- `THIRD-PARTY-LICENSES/licenses/`：從對應上游來源與 Alpine 配方保存的授權、著作權、NOTICE、COPYING、WHENCE、README 等文件，含韌體補充來源與 Go 模組。
- `packages.csv`／`packages.json`：實際 APK 及開機元件版本、授權表達式與 Alpine 提交。
- `source-inventory.json`：精確來源配方、上游檔案、校驗碼與取得位置。
- `go-modules.json`／`go-toolchains.json`：Restic 實際編入的 Go 模組及 Go runtime 對應來源。
- `SOURCES.md`／`SOURCE-PARTS.json`：原始碼下載、分卷校驗、合併與重建說明。

Release 額外附上獨立授權資料包及完整來源分卷。來源包括 84 組精確 Alpine 配方、其上游來源與修補檔、85 個 Restic Go 模組、兩個 Go runtime 來源版本。來源不塞進開機 ISO。

來源與 ISO 放在同一個 [Release](https://github.com/joeboy0722/just-NAS/releases/tag/v2.0.0-rc4)，提供相同可及性的下載；持續提供 ISO 時，也應保留完整來源附件。套件不會因與 just-NAS 一起發行而改為 MIT。

## v2.1.0-rc1 與 GitHub Packages

精簡核心沿用 rc4 相同版本的 Linux、OpenZFS、韌體及核心 APK；保留 rc4 的完整授權包與來源分卷，於新 Release 的 `SOURCES.md` 提供直接下載連結及校驗值。

容器引擎為選配，新增元件包含 Docker／containerd／runc（Apache-2.0）、iptables／libnftnl（GPL-2.0-or-later）、libmnl／libseccomp（LGPL-2.1-or-later）、tini（MIT）及憑證資料（MPL-2.0 AND MIT）。以包內精確套件中繼資料及各上游授權／NOTICE 為準。

GitHub Packages 的 `compliance-v1` 另附 `runtime-corresponding-sources.tar.zst`，含新增精確 Alpine 配方、修補、上游來源、授權與來源清單；容器擴充包本身亦附授權資料。自有程式與靜態網站示範採 MIT，不將上述元件重新授權為 MIT。

## 一般建置前準備

先從相符版本的 Release 下載 `just-NAS-v2.0.0-rc4-licenses.tar.gz` 與 `SHA256SUMS`，核對 SHA256 後解壓：

```text
mkdir output/compliance
tar -xzf just-NAS-v2.0.0-rc4-licenses.tar.gz -C output/compliance
```

完成後應有 `output/compliance/iso/READY`，再執行 README 的建置指令。建置只將授權目錄掛入容器；不將整份來源檔塞入 ISO 或 Docker build context。

建置會驗證授權檔案雜湊、ISO 與 runtime 兩份授權目錄、離線套件清單，以及對應來源所核對的 initramfs／modloop 身分。套件或核心更新導致不一致時須重新收集，不可略過檢查。

## 維護者：更新來源集合

工具需要先建立 `just-nas-builder` 與 `Dockerfile.compliance` 所定義的收集環境。對來源集合執行的步驟為：

1. 在本機候選 ISO 收集 APK 與開機套件的中繼資料。
2. `collect-compliance.py` 依 APK 內的 `commit` 取得原始 APKBUILD 及同目錄修補檔，核對配方版本，下載並驗證配方原有 SHA512／SHA256。
3. `complete-compliance.py` 讀取實際 Go 二進位 build info，保存並核對精確模組來源、Go module h1 與 Go 官方 toolchain source SHA256。
4. `package-compliance.py` 產生 ISO 授權目錄、來源索引與 256 MiB 分卷。
5. 重建最終 ISO，執行 `audit-iso.sh` 與 VM 實測，最後驗證每個分卷及完整串流後才發布。

上述 Python 腳本在隔離的收集容器中使用 `/input.iso`、`/work`、`/scripts`；Go 補件另掛唯讀 `/nas-server-linux`。來源與日誌保存在被 Git 忽略的 `output/compliance/`，不讀取 `output/vm-test/` 的資料、帳密或金鑰。

首次改套件且尚無相符集合時，可明確指定 `NAS_SOURCE_BOOTSTRAP=1` 建立**僅供本機盤點**的候選 ISO。此模式會留下 `output/UNPUBLISHABLE-BOOTSTRAP`，跳過來源集合匹配檢查；不得上傳。之後必須以完整集合重新執行正常建置並通過檢查，才會移除此標記。

每次重新收集應使用新的工作目錄；不可混用不同套件版本或手動變更配方原有校驗碼。不要在來源包加入 build container 的私人簽署金鑰。建置所需公開驗證金鑰與原配方內的公開檔案則可保留。

收集時的 `go-build-info.json` 記錄盤點輸入二進位，供辨識 runtime／依賴版本；最終映像的程式提交另以建置紀錄為準。修改 just-NAS 的版本資訊不會更換 Go 標準庫來源，但若 toolchain 或模組版本改變，必須重新收集。產物目錄另設 Go module 邊界，避免上游的 Go 來源與授權範例被 `go test ./...` 當作 NAS 程式套件。

## OpenZFS 的邊界與不同見解

目前使用獨立 `zfs.ko`，開機動態載入，沒有把 ZFS 編入 Linux 核心本體。[OpenZFS 官方文件](https://openzfs.github.io/openzfs-docs/License.html)認為可以散布獨立模組；[FSF 的解釋](https://www.fsf.org/licensing/zfs-and-linux)持不同見解。這個爭點不會因附上來源、改成動態載入或將 just-NAS 改授權而自動消失。

本次補件提供來源與授權資料及工程檢查，不宣稱取得法律意見或消除上述爭議。元件授權表達式也不能取代其逐檔聲明與例外條款；需要法律確定性的商業散布應另請熟悉開源授權的人員審閱具體做法。
