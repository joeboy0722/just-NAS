"use strict";
window.initStores = function ({ api, button, upload }) {
  const $ = (id) => document.getElementById(id);
  let state = null,
    busy = false;
  const sections = {};
  const labels = {
    active: "運作中",
    disabled: "已停用",
    stopped: "已停止",
    failed: "失敗",
    unavailable: "無法使用",
    restoring: "恢復中",
    unknown: "無法確認",
    incompatible: "版本不相容",
    "pending-reboot": "重新啟動後套用",
  };
  function element(tag, text, cls) {
    const n = document.createElement(tag);
    if (text !== undefined) n.textContent = text;
    if (cls) n.className = cls;
    return n;
  }
  function message(parent, text) {
    parent.append(element("p", text, "field-help"));
  }
  function showError(e) {
    for (const s of Object.values(sections)) {
      s.notice.textContent = e.message;
      s.notice.setAttribute("role", "alert");
    }
  }
  for (const [id, title, description] of [
    ["extensions", "系統擴充", "為 NAS 添加硬體支援與系統能力。"],
    ["apps", "應用商店", "探索應用，確認需求，再安裝至你的 NAS。"],
  ]) {
    const section = $(id === "apps" ? "sec-app-store" : "sec-extensions");
    const panel = element("div", undefined, "panel store-panel");
    if (id === "apps")
      panel.append(
        button("← 返回應用程式", () => {
          NAS.navigate("apps");
          document.querySelector(".app-store-tile").focus();
        }),
      );
    panel.append(element("h3", title));
    message(panel, description);
    const notice = element("div", "登入後載入套件資訊", "notice");
    notice.setAttribute("aria-live", "polite");
    panel.append(notice);
    const actions = element("div", undefined, "header-actions");
    panel.append(actions);
    actions.append(button("設定儲存位置", () => configure()));
    const installed = element("div"),
      imports = element("div"),
      available = element("div"),
      downloads = element("div"),
      tasks = element("div"),
      retained = element("div");
    if (id === "extensions") {
      actions.append(
        button("線上目錄", () => catalog(id)),
        button("離線匯入", () => importFile(id)),
        button("SSH 公鑰", () =>
          window.nasForm(
            "SSH 登入公鑰",
            [
              {
                key: "public_key",
                label: "Ed25519 公鑰",
                value: state?.state.ssh_public_key || "",
                required: true,
              },
            ],
            (v) => api("extensions/ssh-key", "POST", v),
            "SSH 使用 root 帳號與公鑰登入。伺服器指紋：" +
              (state?.ssh_fingerprint || "安裝並啟用後提供"),
          ),
        ),
      );
      panel.append(
        element("h3", "已安裝"),
        installed,
        element("h3", "待安裝套件"),
        imports,
        available,
      );
    } else {
      const tabs = element("div", undefined, "tabs store-tabs");
      tabs.setAttribute("role", "tablist");
      tabs.setAttribute("aria-label", "商店內容");
      for (const [key, label] of [
        ["explore", "探索"],
        ["installed", "已安裝"],
        ["downloads", "下載與匯入"],
      ]) {
        const tab = button(label, () => NAS.navigate("app-store/" + key));
        tab.dataset.storeTab = key;
        tab.setAttribute("role", "tab");
        tab.id = "shop-tab-" + key;
        tabs.append(tab);
        tab.addEventListener("keydown", (e) => {
          const all = [...tabs.querySelectorAll("button")];
          let n = all.indexOf(tab);
          if (e.key === "ArrowRight") n = (n + 1) % all.length;
          else if (e.key === "ArrowLeft") n = (n + all.length - 1) % all.length;
          else if (e.key === "Home") n = 0;
          else if (e.key === "End") n = all.length - 1;
          else return;
          e.preventDefault();
          all[n].focus();
          all[n].click();
        });
      }
      downloads.append(
        button("離線匯入 .jnpkg", () => importFile("apps")),
        element("h3", "下載與安裝任務"),
        tasks,
        element("h3", "待安裝套件"),
        imports,
      );
      const installedPanel = element("div");
      installedPanel.append(installed, element("h3", "保留資料"), retained);
      for (const [key, node] of [
        ["explore", available],
        ["installed", installedPanel],
        ["downloads", downloads],
      ]) {
        node.dataset.storePanel = key;
        node.id = "shop-panel-" + key;
        node.setAttribute("role", "tabpanel");
        node.setAttribute("aria-labelledby", "shop-tab-" + key);
        tabs
          .querySelector('[data-store-tab="' + key + '"]')
          .setAttribute("aria-controls", node.id);
      }
      panel.append(tabs, available, installedPanel, downloads);
    }
    section.append(panel);
    sections[id] = {
      section,
      title,
      notice,
      installed,
      imports,
      available,
      tasks,
      retained,
    };
  }
  let catalogLoaded = false,
    storeFlight = null,
    lastStore = 0,
    rendered = "";
  function routeShop() {
    const key = location.hash.split("/")[1] || "explore";
    document
      .querySelectorAll("[data-store-tab]")
      .forEach((b) =>
        b.setAttribute("aria-selected", String(b.dataset.storeTab === key)),
      );
    document
      .querySelectorAll("[data-store-panel]")
      .forEach((n) => (n.hidden = n.dataset.storePanel !== key));
    if (
      $("sec-app-store").classList.contains("active") &&
      key === "explore" &&
      !catalogLoaded
    ) {
      catalogLoaded = true;
      catalog("apps");
    }
  }
  const launcher = window.initLauncher({ api, button, action, logs: showLogs });
  window.addEventListener("nas:navigate", (e) => {
    if (["app-store", "extensions"].includes(e.detail.id)) lastStore = 0;
    if (e.detail.id === "app-store") routeShop();
  });
  async function showLogs(id) {
    const r = await api("apps/" + encodeURIComponent(id) + "/logs");
    const box = window.nasForm(
      "應用日誌",
      [],
      async () => {},
      "僅顯示最近 150 筆輸出",
    );
    box.querySelector(".form-footer .btn-primary")?.remove();
    box.querySelector("[data-cancel]").textContent = "關閉";
    const pre = element("pre", r.logs);
    pre.style.whiteSpace = "pre-wrap";
    box
      .querySelector("form")
      .insertBefore(pre, box.querySelector(".form-footer"));
  }
  async function configure() {
    const pools = window
      .nasData()
      .pools.map((p) => ({ value: p.name, label: p.name }));
    if (!pools.length) throw Error("請先建立儲存池");
    return window.nasForm(
      "擴充儲存位置",
      [{ key: "pool", label: "儲存池", type: "select", options: pools }],
      async (v) => {
        await api("extensions/storage", "POST", v);
        await refresh();
      },
      "建立專用系統資料集，保存套件、映像及應用資料。第一版不提供搬移。既有同名資料集不會被接管。",
    );
  }
  async function importFile(id) {
    if (!state?.mounted) throw Error("請先設定可用的擴充儲存位置");
    const input = element("input");
    input.type = "file";
    input.accept = ".jnpkg";
    input.onchange = async () => {
      if (!input.files.length) return;
      const s = sections[id];
      busy = true;
      try {
        await upload(
          id + "/import",
          input.files[0],
          (p) => (s.notice.textContent = "上傳套件 " + p + "%；完成後校驗內容"),
        );
        await refresh(true);
      } catch (e) {
        showError(e);
      } finally {
        busy = false;
      }
    };
    input.click();
  }
  async function dependencyPlan(manifest) {
    const fresh = await api("extensions");
    state = { ...fresh, imports: state?.imports || [] };
    let remote = null,
      remoteError = "";
    const steps = [],
      errors = [],
      seen = new Map(),
      visiting = new Set(),
      notes = [];
    async function visit(id, version) {
      if (seen.has(id)) {
        if (seen.get(id) !== version) errors.push(id + " 相依版本衝突");
        return;
      }
      if (visiting.has(id)) {
        errors.push(id + " 存在循環相依");
        return;
      }
      visiting.add(id);
      const current = fresh.state.installed[id];
      if (
        current?.enabled &&
        current.status === "active" &&
        current.package.manifest.version === version
      ) {
        notes.push(current.package.manifest.name + " " + version + " 已就緒");
        seen.set(id, version);
        visiting.delete(id);
        return;
      }
      let pending = fresh.imports.find(
          (x) =>
            x.package.manifest.id === id &&
            x.package.manifest.version === version &&
            x.package.official,
        ),
        entry = null;
      const enable =
        current?.package.manifest.version === version && !current.enabled;
      if (!pending && !enable) {
        if (!remote) {
          try {
            remote = await api("extensions/catalog");
          } catch (e) {
            remote = { entries: [] };
            remoteError = e.message;
          }
        }
        entry = remote.entries.find(
          (x) => x.id === id && x.version === version && x.kind !== "app",
        );
      }
      const info =
        pending?.package.manifest ||
        (enable ? current.package.manifest : entry);
      if (!info) {
        errors.push(
          id +
            " " +
            version +
            " 缺少相容套件，請線上下載或先離線匯入。" +
            (remoteError ? " " + remoteError : ""),
        );
      } else {
        for (const [child, v] of Object.entries(info.dependencies || {}))
          await visit(child, v);
        const reason = !current
          ? "尚未安裝"
          : current.package.manifest.version !== version
            ? "版本不符"
            : !current.enabled
              ? "尚未啟用"
              : current.status === "pending-reboot"
                ? "等待重新啟動"
                : "尚未就緒";
        if (
          current?.package.manifest.version === version &&
          current.status === "pending-reboot"
        )
          errors.push(info.name + " 需要先重新啟動 NAS");
        else
          steps.push({
            id,
            version,
            name: info.name,
            pending,
            enable,
            reboot: info.reboot,
            bytes: pending || enable ? 0 : info.bytes || 0,
          });
        notes.push(
          info.name +
            " " +
            version +
            "（" +
            reason +
            "；" +
            (enable
              ? "啟用現有套件"
              : pending
                ? "使用已驗證離線包，內容 " + NAS.bytes(info.installed_bytes)
                : "需下載 " + NAS.bytes(info.bytes)) +
            "；" +
            (info.kind === "driver" ? "核心與硬體權限" : "系統服務權限") +
            (info.reboot ? "；需重新啟動" : "") +
            "）",
        );
      }
      visiting.delete(id);
      seen.set(id, version);
    }
    for (const [id, version] of Object.entries(manifest.dependencies || {}))
      await visit(id, version);
    return { steps, errors, notes };
  }
  async function install(id, item) {
    const m = item.package.manifest,
      fields = [];
    const isApp = m.kind === "app",
      currentApp = state.apps.apps[m.id];
    const dependencies = await dependencyPlan(m);
    if (dependencies.steps.length)
      fields.push({
        key: "install_dependencies",
        label: "我同意一併安裝／啟用以下系統相依套件",
        type: "checkbox",
        value: false,
      });
    if (isApp) {
      fields.push(
        {
          key: "reuse_data",
          label: "若有此應用的保留資料，使用保留資料",
          type: "checkbox",
          value: false,
        },
        {
          key: "bind_ip",
          label: "NAS 區網位址",
          type: "select",
          options: state.ips,
          value: currentApp?.bind_ip || state.ips[0],
        },
        {
          key: "port",
          label: "網頁連接埠",
          type: "number",
          value: currentApp?.port || 18080,
          min: 1024,
          max: 65535,
        },
        {
          key: "mount_dataset",
          label: "授權內容資料空間（既有空間須具備檔案權限）",
          type: "select",
          value: currentApp?.mount_dataset || "",
          options: [
            { value: "", label: "應用自己的內容資料夾（建議）" },
            ...window
              .nasData()
              .datasets.filter(
                (d) =>
                  d.name.includes("/") &&
                  !d.name.split("/").some((n) => n.startsWith(".")),
              )
              .map((d) => ({ value: d.name, label: d.name })),
          ],
        },
        {
          key: "read_only",
          label: "內容資料僅可讀取",
          type: "checkbox",
          value: currentApp?.read_only ?? true,
        },
      );
    }
    if (!item.package.official)
      fields.push({
        key: "confirm_unofficial",
        label: "我信任此非官方套件及其宣告權限",
        type: "checkbox",
        value: false,
      });
    const help = [
      m.name + " " + m.version,
      item.package.official
        ? "官方簽章已驗證"
        : "非官方套件；驅動可取得系統核心權限",
      "解壓內容：" + NAS.bytes(m.installed_bytes),
      "需求：" +
        (Object.entries(m.dependencies || {})
          .map(
            ([k, v]) =>
              (k === "container-engine" ? "Docker 容器引擎" : k) + " " + v,
          )
          .join("、") || "無"),
      "權限：" +
        (m.permissions || [])
          .map(
            (p) =>
              ({
                "kernel-modules": "系統核心與硬體",
                "system-service": "系統服務管理",
                "selected-content-dataset": "選定的內容資料空間",
                "lan-port": "區網服務連接埠",
              })[p] || p,
          )
          .join("、"),
      "授權：" + m.license,
      isApp && m.app
        ? "資源上限：" + m.app.cpus + " CPU、" + m.app.memory_mb + " MiB 記憶體"
        : "",
      m.reboot ? "安裝後需要重新啟動" : "",
      dependencies.errors.join("；"),
      isApp
        ? "應用更新會先停止服務並拍攝資料快照；更新失敗嘗試恢復舊版。應用入口由應用自行管理登入，靜態網站示範不需登入。"
        : "",
    ]
      .filter(Boolean)
      .join("。 ");
    const form = window.nasForm(
      "安裝／更新 " + m.name,
      fields,
      async (v) => {
        if (dependencies.errors.length)
          throw Error(dependencies.errors.join("；"));
        if (dependencies.steps.length && !v.install_dependencies)
          throw Error("請先勾選同意安裝系統相依套件，或取消安裝。");
        for (const step of dependencies.steps) {
          if (step.enable) {
            const r = await api("extensions/action", "POST", {
              id: step.id,
              action: "enable",
            });
            await waitTask(r.task_id);
          } else {
            let pending = step.pending;
            if (!pending) {
              const r = await api("extensions/download", "POST", {
                id: step.id,
              });
              await waitTask(r.task_id);
              const fresh = await api("extensions");
              pending = fresh.imports.find(
                (x) =>
                  x.package.manifest.id === step.id &&
                  x.package.manifest.version === step.version,
              );
              if (!pending) throw Error("下載後未找到相容套件：" + step.name);
            }
            const r = await api("extensions/install", "POST", {
              token: pending.token,
            });
            await waitTask(r.task_id);
          }
          const fresh = await api("extensions"),
            current = fresh.state.installed[step.id];
          if (!current?.enabled || current.status !== "active")
            throw Error(
              step.name +
                " 尚未就緒" +
                (current?.status === "pending-reboot"
                  ? "，請先重新啟動 NAS，再回來安裝應用。"
                  : "：" +
                    (current?.error || current?.status || "請查看任務中心")),
            );
        }
        const r = await api(id + "/install", "POST", {
          ...v,
          token: item.token,
          port: Number(v.port || 0),
        });
        sections[id].notice.textContent =
          "安裝任務已建立：" + r.task_id + "。請至任務中心查看結果。";
        await refresh(true);
      },
      help,
    );
    if (dependencies.notes.length) {
      const box = element("section", undefined, "dependency-summary");
      box.append(element("h3", "系統相依檢查"));
      message(
        box,
        dependencies.steps.length +
          " 項需要處理 · 另需下載 " +
          NAS.bytes(dependencies.steps.reduce((n, x) => n + x.bytes, 0)),
      );
      const list = element("ul");
      for (const note of dependencies.notes) list.append(element("li", note));
      box.append(list);
      form.querySelector(".field-help").after(box);
    }
    if (dependencies.errors.length)
      form.querySelector(".form-footer .btn-primary").disabled = true;
  }

  async function waitTask(id) {
    const until = Date.now() + 30 * 60 * 1000;
    while (Date.now() < until) {
      const jobs = await api("tasks"),
        job = jobs.find((j) => j.id === id);
      if (!job) throw Error("找不到安裝任務，請至任務中心確認");
      const note = document.querySelector(
        ".modal-backdrop.active:last-child .field-help",
      );
      if (note) note.textContent = job.progress || "等待系統回報執行階段";
      if (!["running", "queued"].includes(job.state)) {
        if (job.state !== "succeeded")
          throw Error(job.error || "系統套件安裝失敗");
        return;
      }
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
    throw Error("等待時間已到；任務仍由系統管理，請至任務中心確認結果");
  }
  async function action(id, resource, action) {
    if (
      action === "stop" &&
      !(await NAS.confirm("停止此應用會中斷目前的服務連線，資料會保留。"))
    )
      return;
    if (
      action === "remove" &&
      !(await NAS.confirm(
        "移除 " + resource + "？應用資料與離線快取預設保留。",
        resource,
      ))
    )
      return;
    if (action === "remove" && id === "apps") {
      return window.nasForm(
        "移除應用",
        [
          {
            key: "delete_data",
            label: "同時永久刪除應用自己的資料（不刪除外部授權資料空間）",
            type: "checkbox",
            value: false,
          },
          {
            key: "confirm",
            label: "如要刪除資料，請輸入 " + resource,
            value: "",
          },
        ],
        async (v) => {
          await api(id + "/action", "POST", { ...v, id: resource, action });
          await refresh();
        },
        "預設保留資料。仍被快照保護的資料集可能無法刪除，將明確回報失敗。",
      );
    }
    const r = await api(id + "/action", "POST", { id: resource, action });
    sections[id].notice.textContent = "已建立任務 " + r.task_id;
    await refresh();
  }
  async function catalog(id) {
    const s = sections[id];
    s.available.replaceChildren(element("p", "正在取得官方簽章目錄…"));
    try {
      const c = await api(id + "/catalog");
      s.available.replaceChildren();
      if (c.source === "bundled")
        message(
          s.available,
          "使用映像附帶的已簽署目錄。線上下載需要網路，離線時可匯入完整套件。",
        );
      const entries = c.entries.filter(
        (v) => (v.kind === "app") === (id === "apps"),
      );
      if (!entries.length) message(s.available, "目前尚無可用套件。");
      const grid = element("div", undefined, "catalog-grid");
      s.available.append(grid);
      for (const e of entries) {
        const card = button("", () => {
          const help = [
            "官方簽章目錄",
            e.version,
            "下載 " + NAS.bytes(e.bytes),
            "授權 " + e.license,
            "相依：" +
              (Object.entries(e.dependencies || {})
                .map(([k, v]) => k + " " + v)
                .join("、") || "無"),
            e.kernel ? "需要核心 " + e.kernel : "架構 x86_64",
            e.reboot ? "需要重新啟動" : "",
            "下載後會驗證套件，安裝前再列出完整權限與缺少的系統套件。",
          ]
            .filter(Boolean)
            .join("。 ");
          const box = window.nasForm(
            e.name,
            [],
            async () => {
              const r = await api(id + "/download", "POST", { id: e.id });
              s.notice.textContent = "下載任務已建立：" + r.task_id;
              if (id === "apps") NAS.navigate("app-store/downloads");
              await refresh(true);
            },
            help,
          );
          box.querySelector(".form-footer .btn-primary").textContent =
            "下載套件";
        });
        card.className = "catalog-card";
        const icon = element("span", undefined, "catalog-glyph");
        icon.innerHTML = NAS.icon(id === "apps" ? "grid" : "settings");
        card.append(
          icon,
          element("strong", e.name),
          element("span", e.version + " · " + NAS.bytes(e.bytes)),
          element("small", e.kind === "app" ? "容器應用 · x86_64" : "系統擴充"),
        );
        grid.append(card);
      }
    } catch (e) {
      catalogLoaded = false;
      s.available.replaceChildren(
        element("p", e.message, "notice"),
        button("重新載入目錄", () => catalog(id)),
      );
    }
  }
  async function refresh(force = false) {
    if (document.hidden) return;
    if ($("sec-apps").classList.contains("active"))
      return launcher.refresh(force);
    if (storeFlight) return storeFlight;
    if (!force && Date.now() - lastStore < 4500) return;
    storeFlight = refreshContent(force).finally(() => {
      storeFlight = null;
    });
    return storeFlight;
  }
  async function refreshContent(force = false) {
    if (busy && !force) return;
    const selected = Object.values(sections).some((s) =>
      s.section.classList.contains("active"),
    );
    if (!selected && !force) return;
    const activeID = sections.apps.section.classList.contains("active")
      ? "apps"
      : "extensions";
    state = await api(activeID);
    const appData = state;
    lastStore = Date.now();
    let jobs = [];
    if (activeID === "apps")
      jobs = (await api("tasks")).filter(
        (j) =>
          j.kind === "store-download" ||
          j.kind === "store-install" ||
          j.kind === "store-action",
      );
    const signature = JSON.stringify({ state, jobs, activeID });
    if (signature === rendered && !force) return;
    rendered = signature;
    for (const [id, s] of Object.entries(sections)) {
      if (id !== activeID) continue;
      s.notice.textContent = state.restoring
        ? "正在恢復系統擴充與應用，完成前暫停商店操作…"
        : state.state.dataset
          ? state.mounted
            ? "儲存位置：" + state.state.dataset
            : "儲存位置未掛載：相關擴充與應用無法使用"
          : "尚未設定擴充儲存位置；請先選擇儲存池。";
      s.installed.replaceChildren();
      s.imports.replaceChildren();
      const items =
        id === "apps"
          ? Object.values(state.apps.apps)
          : Object.values(state.state.installed);
      if (!items.length) message(s.installed, "尚未安裝" + s.title + "項目。");
      for (const v of items) {
        const m = v.package.manifest;
        const row = element("div", undefined, "store-card");
        row.append(
          element("strong", m.name),
          element(
            "span",
            m.version +
              " · " +
              (state.restoring && v.enabled
                ? "恢復中"
                : labels[v.status] || v.status),
            "status " +
              (v.status === "active"
                ? "ok"
                : ["failed", "unavailable", "incompatible"].includes(v.status)
                  ? "danger"
                  : ""),
          ),
        );
        if (v.error) message(row, v.error);
        const controls = element("div", undefined, "header-actions");
        if (id === "apps") {
          if (v.status === "active") {
            const a = element("a", "開啟應用", "btn btn-secondary btn-sm");
            a.href = "http://" + v.bind_ip + ":" + v.port + "/";
            a.target = "_blank";
            a.rel = "noopener";
            controls.append(a);
          }
          controls.append(
            button(v.status === "active" ? "停止" : "啟動", () =>
              action(id, m.id, v.status === "active" ? "stop" : "start"),
            ),
            button("日誌", async () => {
              const r = await api("apps/" + encodeURIComponent(m.id) + "/logs");
              const box = window.nasForm(
                "應用日誌",
                [],
                async () => {},
                "僅顯示最近 150 筆輸出",
              );
              const pre = document.createElement("pre");
              pre.textContent = r.logs;
              pre.style.whiteSpace = "pre-wrap";
              box.querySelector("form").prepend(pre);
            }),
          );
        } else
          controls.append(
            button(
              v.status === "failed" ? "重試啟動" : v.enabled ? "停用" : "啟用",
              () =>
                action(
                  id,
                  m.id,
                  v.status === "failed"
                    ? "enable"
                    : v.enabled
                      ? "disable"
                      : "enable",
                ),
            ),
          );
        controls.append(
          button(id === "apps" ? "解除安裝" : "移除", () =>
            action(id, m.id, "remove"),
          ),
        );
        row.append(controls);
        s.installed.append(row);
        if (id === "apps" && launcher.selected() === m.id)
          row.classList.add("store-selected");
      }
      if (id === "apps") {
        s.tasks.replaceChildren();
        for (const j of jobs.slice().reverse().slice(0, 20)) {
          const row = element("div", undefined, "store-task");
          const names = {
            "store-download": "套件下載",
            "store-install": "套件安裝",
            "store-action": "應用／擴充操作",
          };
          const states = {
            running: "執行中",
            queued: "等待中",
            succeeded: "已完成",
            failed: "失敗",
            interrupted: "已中斷",
          };
          row.append(
            element("strong", names[j.kind]),
            element("span", states[j.state] || j.state),
            element("small", j.error || j.progress || ""),
          );
          if (["failed", "interrupted"].includes(j.state)) {
            if (j.kind === "store-download") {
              const id = j.resource.split("|")[1];
              row.append(
                button("重試下載", async () => {
                  await api("apps/download", "POST", { id });
                  await refresh(true);
                }),
              );
            } else row.append(button("查看任務", () => NAS.navigate("tasks")));
          }
          s.tasks.append(row);
        }
        if (!jobs.length) message(s.tasks, "目前沒有下載或安裝任務。");
        s.retained.replaceChildren();
        if (state.retained_error) message(s.retained, state.retained_error);
        else if (!state.retained_data?.length)
          message(s.retained, "沒有已解除安裝應用的保留資料。");
        else
          for (const ds of state.retained_data)
            message(s.retained, ds + " — 重新安裝時可選擇使用保留資料。");
      }
      const imports = (id === "apps" ? appData : state).imports;
      if (!imports.length)
        message(s.imports, "尚無套件。可線上下載，或匯入完整 .jnpkg 離線包。");
      for (const v of imports) {
        const m = v.package.manifest;
        const row = element("div", undefined, "store-card");
        row.append(
          element("strong", m.name),
          element(
            "span",
            m.version +
              " · " +
              NAS.bytes(m.installed_bytes) +
              " · " +
              (v.package.official ? "官方簽章" : "非官方"),
          ),
          button("檢視與安裝", () => install(id, v)),
          button("移除匯入", async () => {
            await api(id + "/discard", "POST", { token: v.token });
            await refresh();
          }),
        );
        s.imports.append(row);
      }
    }
  }
  window.refreshStores = () => refresh();
  routeShop();
  setInterval(() => {
    if (!document.hidden && sections.apps.section.classList.contains("active"))
      refresh().catch(showError);
  }, 5000);
};
