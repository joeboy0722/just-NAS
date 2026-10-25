# VM 應用的授權、來源與重建

just-NAS 自有程式採 MIT。QEMU、韌體、noVNC 與相依程式保留各自授權，並非整包改成 MIT。

## VM 新增元件

新的 stores-v2 預發布通過驗收後，其 `just-NAS-vm-support-and-sources.tar.gz` 提供：

- `source/qemu-runtime/recipes/<origin>/`：從 APK 記錄的 Alpine commit 取得的 APKBUILD、原始修補與本機來源。
- `source/qemu-runtime/upstream/<origin>/`：依該配方原有校驗碼驗證的來源檔。
- `build/packages.json` 與 `build/source-inventory.json`：新增 APK 的精確版本、配方提交、來源 URL、校驗碼及驗證結果。
- `licenses/qemu-runtime/`：相應授權、著作權及 NOTICE。
- `source/just-NAS-source.tar`：建置此候選的專案來源，含 vendored Gorilla WebSocket 與完整 noVNC 原始碼及授權。
- `build/vm-app-build-info.txt`：實際應用二進位的 Go 版本、模組版本、來源提交與 dirty 狀態。

QEMU 執行環境採 Alpine 3.24 x86_64。重建各 APK 時，使用相符 Alpine abuild 環境，設定 `SRCDEST` 指向對應 `upstream/<origin>`，依 `recipes/<origin>/APKBUILD` 建置；配方亦保留建置相依與原始修補。不要把不同版本的配方與上游來源混用。

專案的 VM 程式以 Go 1.25.6 建置，依 build info 為準：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o output/vm-manager-linux ./cmd/vm-manager
```

noVNC 1.6.0 的完整來源及相依授權位於 `third_party/novnc-source`；介面的本機 bundle 使用 esbuild 0.25.10 建置。`scripts/prepare-vm-runtime.sh` 記錄 runtime 的套件集合，`scripts/build-vm-packages.py` 與 `scripts/build-extension-packages.py` 記錄宣告式封裝方式。私人簽署金鑰不提供；重建者必須使用自己的金鑰，不能冒充官方套件。

## 沿用的核心相依

離線 runtime 包內沿用、版本未變的核心 APK，其完整來源、Go runtime 來源與授權仍公開保留在 [v2.0.0-rc4 Release](https://github.com/joeboy0722/just-NAS/releases/tag/v2.0.0-rc4)。套件的 `licenses/core-third-party/SOURCES.md` 及 `SOURCE-PARTS.json` 提供原始來源清單、分卷名稱及 SHA-256；來源分卷必須全部下載、按名稱順序合併，再解壓，不能單獨解開其中一卷。

```sh
cat just-NAS-v2.0.0-rc4-sources.tar.zst.part-* | zstd -d | tar -xf -
```

每個分卷先核對該 Release 的 SHA256SUMS；完整串流 SHA-256 為 `71e889c4a247333ab7e84469c47a135e50ea75bfdce663c0adc317a5a73feedd`。現有來源下載不隨新商店版本撤除。新加入或版本不同的元件，必須另外完成相應來源收集與驗證，不能以舊來源替代。

Windows／Ubuntu 安裝 ISO、客體磁碟、產品金鑰、測試帳密與私人金鑰不屬於發布附件。相容核心候選本輪只在本地交付，現有核心 Release 保持原樣。
