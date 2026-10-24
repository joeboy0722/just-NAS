"use strict";
NAS.layout = function () {
  const $ = (id) => document.getElementById(id),
    body = $("sec-dashboard").parentElement;
  const pages = [
    ["dashboard", "總覽", "grid", "掌握 NAS 健康、容量與資料保護狀態"],
    ["storage", "儲存管理", "disk", "管理儲存池、資料空間與硬碟"],
    ["users", "家庭帳號", "users", "管理家庭成員與個人檔案帳號"],
    ["shares", "分享與權限", "link", "設定存取權限與 SMB／NFS 連線"],
    ["snapshots", "快照與恢復", "clock", "查看時間點版本與恢復選項"],
    ["backup", "備份與恢復", "shield", "獨立備份裝置、加密版本與恢復"],
    ["tasks", "任務與排程", "clock", "追蹤執行結果與定期維護"],
    ["logs", "告警與紀錄", "file", "檢查異常、操作紀錄與系統日誌"],
    ["settings", "系統設定", "settings", "網路、安全、通知與系統維護"],
  ];
  for (const [id] of pages)
    if (!$("sec-" + id)) {
      const section = document.createElement("section");
      section.id = "sec-" + id;
      section.className = "section-page";
      body.append(section);
    }
  const accountCard = $("user-form").closest(".table-card");
  $("sec-users").prepend(accountCard);
  accountCard.querySelector("h3").textContent = "家庭檔案帳號";
  accountCard.querySelector("p").textContent =
    "家庭帳號用於 SMB 與網頁檔案入口；管理員控制台使用獨立密碼。";
  const snapshotCard = $("tbody-snapshots").closest(".table-card");
  const snapshotHeading = snapshotCard.previousElementSibling;
  $("sec-snapshots").append(snapshotHeading, snapshotCard);
  snapshotHeading.style.marginTop = "0";
  snapshotHeading.querySelector("h3").textContent = "快照版本";
  const filter = document.createElement("input");
  filter.placeholder = "篩選資料空間或快照名稱";
  filter.setAttribute("aria-label", "篩選快照");
  snapshotHeading.append(filter);
  filter.oninput = () =>
    document
      .querySelectorAll("#tbody-snapshots tr")
      .forEach(
        (r) =>
          (r.hidden = !r.textContent
            .toLowerCase()
            .includes(filter.value.toLowerCase())),
      );
  const nav = document.querySelector(".nav-menu");
  nav.replaceChildren(
    ...pages.map(([id, label, icon]) => {
      const a = document.createElement("a");
      a.href = "#" + id;
      a.className = "nav-item";
      a.innerHTML =
        '<span class="icon">' +
        NAS.icon(icon) +
        "</span><span>" +
        label +
        "</span>";
      return a;
    }),
  );
  const files = document.createElement("a");
  files.href = "/files.html";
  files.className = "nav-item";
  files.innerHTML =
    '<span class="icon">' +
    NAS.icon("folder") +
    "</span><span>家庭檔案入口 ↗</span>";
  nav.append(files);
  const tabs = () => {
    const d = document.createElement("div");
    d.className = "tabs";
    for (const [id, t] of [
      ["storage", "儲存池"],
      ["datasets", "資料空間"],
      ["disks", "硬碟健康"],
    ]) {
      const b = document.createElement("button");
      b.textContent = t;
      b.dataset.tab = id;
      b.onclick = () => navigate(id);
      d.append(b);
    }
    return d;
  };
  ["storage", "datasets", "disks"].forEach((id) =>
    $("sec-" + id).prepend(tabs()),
  );
  const more = document.createElement("details");
  more.className = "row-menu";
  const summary = document.createElement("summary");
  summary.textContent = "管理員 ⋯";
  const menu = document.createElement("div");
  menu.className = "menu-content";
  ["btn-password", "btn-diagnostics", "btn-reboot", "btn-logout"].forEach(
    (id) => menu.append($(id)),
  );
  more.append(summary, menu);
  document.querySelector(".top-header .header-actions").append(more);
  $("btn-refresh").textContent = "重新整理";
  $("ds-mount-input").closest(".form-group").hidden = true;
  $("ds-mount-input").value = "";
  const select = $("share-users-select");
  select.hidden = true;
  const checks = document.createElement("div");
  checks.className = "choice-list";
  select.after(checks);
  function syncUsers() {
    checks.replaceChildren(
      ...[...select.options].map((o) => {
        const l = document.createElement("label"),
          c = document.createElement("input");
        c.type = "checkbox";
        c.checked = o.selected;
        c.disabled = select.disabled;
        c.onchange = () => (o.selected = c.checked);
        l.append(c, document.createTextNode(o.text));
        return l;
      }),
    );
  }
  const hosts = $("share-hosts-input");
  hosts.hidden = true;
  const hostsBox = document.createElement("div");
  hosts.after(hostsBox);
  function syncHosts() {
    const values = hosts.value.split(/[\s,]+/).filter(Boolean);
    hostsBox.replaceChildren();
    function row(value = "") {
      const d = document.createElement("div");
      d.className = "host-row";
      const i = document.createElement("input");
      i.value = value;
      i.placeholder = "例如 192.168.1.0/24";
      i.setAttribute("aria-label", "允許的 NFS IP 或網段");
      const remove = document.createElement("button");
      remove.type = "button";
      remove.textContent = "移除";
      const save = () =>
        (hosts.value = [...hostsBox.querySelectorAll("input")]
          .map((x) => x.value.trim())
          .filter(Boolean)
          .join(","));
      i.oninput = save;
      remove.onclick = () => {
        d.remove();
        save();
      };
      d.append(i, remove);
      hostsBox.insertBefore(d, add);
    }
    const add = document.createElement("button");
    add.type = "button";
    add.textContent = "新增允許來源";
    add.onclick = () => row();
    hostsBox.append(add);
    (values.length ? values : [""]).forEach(row);
  }
  const path = $("share-path-input"),
    picker = document.createElement("select");
  picker.setAttribute("aria-label", "選擇資料空間");
  path.before(picker);
  picker.onchange = () => {
    if (picker.value) path.value = picker.value;
  };
  path.placeholder = "進階：既有資料夾的完整路徑";
  path.setAttribute("aria-label", "分享目標路徑");
  async function syncShare() {
    syncUsers();
    syncHosts();
    picker.disabled = path.disabled;
    try {
      const r = await fetch("/api/storage/datasets"),
        ds = await r.json();
      if (!r.ok) return;
      picker.replaceChildren(
        new Option("選擇資料空間（或保留自訂路徑）", ""),
        ...ds
          .filter((d) => d.name.includes("/"))
          .map((d) => new Option(d.name, d.mountpoint)),
      );
      picker.value = path.value;
    } catch {}
  }
  new MutationObserver(() => {
    if ($("modal-create-share").classList.contains("active")) syncShare();
  }).observe($("modal-create-share"), {
    attributes: true,
    attributeFilter: ["class"],
  });
  function navigate(id) {
    if (!$("sec-" + id)) id = "dashboard";
    document
      .querySelectorAll(".section-page")
      .forEach((s) => s.classList.toggle("active", s.id === "sec-" + id));
    const main = ["datasets", "disks"].includes(id) ? "storage" : id;
    document
      .querySelectorAll('.nav-item[href^="#"]')
      .forEach((a) => a.classList.toggle("active", a.hash === "#" + main));
    const item = pages.find((p) => p[0] === main);
    $("page-title").textContent = item[1];
    $("page-subtitle").textContent = item[3];
    document
      .querySelectorAll("[data-tab]")
      .forEach((b) =>
        b.setAttribute("aria-selected", String(b.dataset.tab === id)),
      );
    document.body.classList.remove("nav-open");
    if (location.hash !== "#" + id) history.pushState(null, "", "#" + id);
  }
  NAS.navigate = navigate;
  document.addEventListener(
    "click",
    (e) => {
      const a = e.target.closest('.nav-item[href^="#"]');
      if (a) {
        e.preventDefault();
        e.stopImmediatePropagation();
        navigate(a.hash.slice(1));
      }
    },
    true,
  );
  window.addEventListener("popstate", () => navigate(location.hash.slice(1)));
  navigate(location.hash.slice(1) || "dashboard");
  document
    .querySelectorAll(".card-header .icon")
    .forEach((e) => (e.innerHTML = NAS.icon("grid")));
  document.querySelectorAll(".section-header-bar h3").forEach((h) => {
    h.textContent = h.textContent
      .replace("ZFS Storage Pools 管理", "儲存池")
      .replace("ZFS Datasets 管理", "資料空間")
      .replace("SMART 硬碟健康與狀態監控", "硬碟健康")
      .replace(
        "系統即時日誌與修補事件 (System & Self-Healing Logs)",
        "系統日誌",
      );
  });
};
NAS.decorate = function () {
  const data = window.nasData?.();
  if (data) {
    document.querySelectorAll("#users-list>div").forEach((row) => {
      row.querySelector(".account-space")?.remove();
      row.querySelector("[data-space-action]")?.remove();
      const name = row.querySelector("strong")?.textContent;
      const share = data.shares.find(
        (s) => s.mode === "private" && (s.users || []).includes(name),
      );
      const ds =
        share && data.datasets.find((d) => d.mountpoint === share.path);
      const info = document.createElement("small");
      info.className = "account-space";
      info.textContent = share
        ? "個人空間：" +
          share.name +
          (ds
            ? " · 使用 " +
              NAS.bytes(ds.used) +
              " · 配額 " +
              (ds.quota ? NAS.bytes(ds.quota) : "不限制")
            : "")
        : "尚未建立個人空間";
      row.append(info);
      const menu = row.querySelector(".menu-content");
      if (menu) {
        const b = document.createElement("button");
        b.className = "btn btn-secondary";
        b.dataset.spaceAction = "true";
        b.textContent = share ? "管理個人空間" : "建立個人空間";
        b.onclick = () => {
          row.querySelector("details").open = false;
          if (share) {
            NAS.navigate("datasets");
            const match = [
              ...document.querySelectorAll("#tbody-datasets tr"),
            ].find((r) => r.cells[0]?.textContent === ds?.name);
            match?.querySelector("button")?.click();
          } else {
            document.getElementById("btn-create-space")?.click();
            const owner = document.querySelector('[id^="field-owner-"]');
            if (owner) owner.value = name;
          }
        };
        menu.append(b);
      }
    });
    document.querySelectorAll("#tbody-datasets tr").forEach((row) => {
      const ds = data.datasets.find(
        (d) => d.name === row.cells[0]?.textContent,
      );
      if (!ds) return;
      row.querySelector(".dataset-usage")?.remove();
      const detail = document.createElement("small");
      detail.className = "dataset-usage";
      detail.textContent =
        "資料 " +
        NAS.bytes(ds.referenced) +
        " · 快照 " +
        NAS.bytes(ds.snapshot_used);
      row.cells[1].append(detail);
    });
  }

  document.querySelectorAll(".table-responsive td").forEach((td) => {
    td.title = td.textContent;
  });
  const labels = {
    ONLINE: "正常",
    DEGRADED: "降級",
    FAULTED: "故障",
    OFFLINE: "離線",
    UNAVAIL: "無法使用",
    PASSED: "正常",
  };
  document
    .querySelectorAll("#dash-tbody-pools td,#tbody-pools td,#tbody-disks td")
    .forEach((td) => {
      const value = td.textContent;
      if (labels[value]) {
        td.textContent = "";
        const badge = document.createElement("span");
        badge.className =
          "status " + (["ONLINE", "PASSED"].includes(value) ? "ok" : "danger");
        badge.textContent = labels[value];
        badge.title = value;
        td.append(badge);
      }
    });
  for (const [id, index] of [
    ["dash-tbody-pools", 4],
    ["tbody-pools", 4],
  ])
    document.querySelectorAll("#" + id + " tr").forEach((row) => {
      const td = row.cells[index];
      if (!td) return;
      const value = parseFloat(td.textContent);
      if (!Number.isFinite(value)) return;
      td.querySelector(".progress-bar")?.remove();
      const progress = document.createElement("div");
      progress.className = "progress-bar";
      progress.style.marginTop = "6px";
      const fill = document.createElement("div");
      fill.className = "progress-fill";
      fill.style.width = Math.min(100, value) + "%";
      if (value >= 90) fill.style.background = "var(--danger)";
      progress.append(fill);
      td.append(progress);
    });
  document
    .querySelector("#sec-snapshots input")
    ?.dispatchEvent(new Event("input"));
};
