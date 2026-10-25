"use strict";
window.initLauncher = function ({ api, button, action, logs }) {
  const root = document.getElementById("sec-apps"),
    cards = new Map();
  const label = {
    active: "執行中",
    stopped: "已停止",
    disabled: "已停用",
    starting: "啟動中",
    restoring: "恢復中",
    stopping: "停止中",
    failed: "執行失敗",
    incompatible: "版本不相容",
    unknown: "無法確認",
    unavailable: "儲存不可用",
  };
  const el = (tag, text, cls) => {
    const n = document.createElement(tag);
    if (text !== undefined) n.textContent = text;
    if (cls) n.className = cls;
    return n;
  };
  let flight = null,
    last = 0,
    stale = false,
    scroll = 0,
    selectedApp = "";
  const notice = el("p", "載入應用資訊…", "launcher-notice");
  notice.setAttribute("role", "status");
  const tools = el("div", undefined, "launcher-toolbar"),
    search = el("input"),
    filter = el("select"),
    count = el("span", "", "field-help");
  search.type = "search";
  search.placeholder = "搜尋應用程式";
  search.setAttribute("aria-label", "搜尋應用程式");
  filter.setAttribute("aria-label", "應用狀態篩選");
  for (const [v, t] of [
    ["all", "全部"],
    ["active", "執行中"],
    ["stopped", "已停止"],
    ["error", "異常"],
  ])
    filter.add(new Option(t, v));
  try {
    const saved = JSON.parse(sessionStorage.getItem("nas-launcher") || "{}");
    search.value = saved.search || "";
    filter.value = saved.filter || "all";
  } catch {}
  const grid = el("div", undefined, "app-grid"),
    empty = el("p", "前往商店安裝應用，之後就能從這裡開啟。", "launcher-empty");
  const shop = el("button", undefined, "app-tile app-store-tile");
  shop.type = "button";
  shop.setAttribute("aria-label", "應用商店");
  const shopIcon = el("span", undefined, "app-glyph shop-glyph");
  shopIcon.innerHTML =
    '<svg viewBox="0 0 48 48" fill="none" aria-hidden="true"><path d="M10 19h28l-2-9H12l-2 9Zm0 0v20h28V19M19 39V28h10v11M9 19c0 7 8 7 8 0 0 7 7 7 7 0 0 7 7 7 7 0 0 7 8 7 8 0" stroke="currentColor" stroke-width="2.4" stroke-linejoin="round"/></svg>';
  shop.append(
    shopIcon,
    el("span", "應用商店", "app-name"),
    el("span", "探索與安裝", "app-caption"),
  );
  shop.onclick = () => {
    scroll = window.scrollY;
    NAS.navigate("app-store/explore");
  };
  grid.append(shop);
  tools.append(search, filter, count);
  root.append(tools, notice, grid, empty);
  function applyFilter() {
    let visible = 0;
    const q = search.value.trim().toLocaleLowerCase();
    for (const row of cards.values()) {
      const a = row.model;
      const matches =
        filter.value === "all" ||
        (filter.value === "active" && a.status === "active") ||
        (filter.value === "stopped" &&
          ["stopped", "disabled"].includes(a.status)) ||
        (filter.value === "error" &&
          ["failed", "unknown", "unavailable", "incompatible"].includes(
            a.status,
          ));
      row.node.hidden = !(matches && a.name.toLocaleLowerCase().includes(q));
      if (!row.node.hidden) visible++;
    }
    count.textContent = cards.size
      ? visible + " / " + cards.size + " 個應用"
      : "";
    empty.hidden = visible > 0;
    empty.textContent = cards.size
      ? "沒有符合條件的應用。"
      : "前往商店安裝應用，之後就能從這裡開啟。";
    sessionStorage.setItem(
      "nas-launcher",
      JSON.stringify({ search: search.value, filter: filter.value }),
    );
  }
  search.oninput = filter.onchange = applyFilter;
  function glyph(a) {
    const box = el("span", undefined, "app-glyph");
    box.innerHTML = NAS.icon("grid");
    if (a.icon) {
      const img = el("img");
      img.src = a.icon;
      img.alt = "";
      img.loading = "lazy";
      img.onerror = () => {
        img.remove();
        box.classList.remove("has-image");
      };
      box.append(img);
      box.classList.add("has-image");
    }
    return box;
  }
  function open(a) {
    if (stale) return details(a);
    if (a.status !== "active" || !a.url) return details(a);
    if (a.open_mode === "external") {
      const u = new URL(a.url, location.origin);
      if (!["http:", "https:"].includes(u.protocol))
        throw Error("應用入口無效");
      window.open(u.href, "_blank", "noopener");
    } else if (a.open_mode === "internal" && a.url.startsWith("/apps/")) {
      location.assign(a.url);
    } else details(a);
  }
  function details(a) {
    const current = cards.get(a.id)?.model || a;
    const box = window.nasForm(
      current.name,
      [],
      async () => {},
      "版本 " +
        current.version +
        " · " +
        (label[current.status] || "無法確認") +
        "。" +
        (stale
          ? "狀態資訊已過期，請重新整理。"
          : current.reason || "背景服務狀態與瀏覽器分頁是否開啟無關。"),
    );
    const form = box.querySelector("form");
    form.querySelector(".form-footer .btn-primary")?.remove();
    form.querySelector("[data-cancel]").textContent = "關閉";
    const bar = el("div", undefined, "header-actions");
    for (const [id, name] of [
      ["open", "開啟應用"],
      ["start", "啟動"],
      ["stop", "停止"],
      ["logs", "查看日誌"],
      ["update", "前往商店"],
    ]) {
      if (!(current.actions || []).includes(id)) continue;
      const b = button(name, async () => {
        box.remove();
        await invoke(current.id, id);
      });
      b.disabled = stale && !["logs", "update"].includes(id);
      bar.append(b);
    }
    form.append(bar);
  }
  async function invoke(id, op) {
    const a = cards.get(id)?.model;
    if (!a) return;
    if (op === "details") return details(a);
    if (op === "update") {
      selectedApp = id;
      NAS.navigate("app-store/installed");
      return;
    }
    if (op === "open") return open(a);
    if (op === "logs") return logs(id);
    await action("apps", id, op);
    last = 0;
    await refresh();
  }
  function card(a) {
    const node = el("article", undefined, "app-tile"),
      main = el("button", undefined, "app-main"),
      name = el("span", a.name, "app-name"),
      status = el("span", "", "app-status"),
      badge = el("span", "", "app-badge"),
      external = el("span", "↗", "app-external");
    main.type = "button";
    main.append(glyph(a), name, status, external);
    main.onclick = () => open(cards.get(a.id).model);
    const menu = el("details", undefined, "app-menu row-menu"),
      summary = el("summary", "⋯"),
      content = el("div", undefined, "menu-content");
    summary.setAttribute("aria-label", a.name + " 的操作");
    menu.append(summary, content);
    const controls = {};
    for (const [op, title] of [
      ["open", "開啟"],
      ["start", "啟動"],
      ["stop", "停止"],
      ["details", "詳情"],
      ["logs", "日誌"],
      ["update", "前往商店更新"],
      ["remove", "解除安裝"],
    ]) {
      const b = button(title, async () => {
        menu.open = false;
        summary.focus();
        await invoke(a.id, op);
      });
      controls[op] = b;
      content.append(b);
    }
    menu.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        menu.open = false;
        summary.focus();
      }
    });
    node.append(main, badge, menu);
    const row = {
      node,
      main,
      name,
      status,
      badge,
      external,
      controls,
      menu,
      model: a,
      icon: a.icon,
    };
    cards.set(a.id, row);
    return row;
  }
  function render(data) {
    const ids = new Set(data.apps.map((a) => a.id));
    for (const [id, row] of cards) {
      if (!ids.has(id)) {
        if (row.node.contains(document.activeElement)) shop.focus();
        row.node.remove();
        cards.delete(id);
      }
    }
    const sorted = [...data.apps].sort(
      (a, b) =>
        a.name.localeCompare(b.name, "zh-Hant") || a.id.localeCompare(b.id),
    );
    let before = shop;
    for (const a of sorted) {
      const row = cards.get(a.id) || card(a);
      row.model = a;
      if (row.icon !== a.icon) {
        row.main.querySelector(".app-glyph").replaceWith(glyph(a));
        row.icon = a.icon;
      }
      row.name.textContent = a.name;
      row.name.title = a.name;
      row.main.setAttribute(
        "aria-label",
        a.name + "，" + (label[a.status] || "無法確認"),
      );
      row.status.textContent = label[a.status] || "無法確認";
      row.badge.dataset.state = a.status;
      row.badge.textContent = ["failed", "incompatible"].includes(a.status)
        ? "!"
        : ["unknown", "unavailable"].includes(a.status)
          ? "?"
          : "";
      row.badge.title =
        (label[a.status] || "無法確認") + (a.reason ? "：" + a.reason : "");
      row.badge.setAttribute("aria-label", row.badge.title);
      row.external.hidden = a.open_mode !== "external";
      row.external.title = "另開瀏覽器分頁";
      for (const [op, b] of Object.entries(row.controls))
        b.disabled = !(a.actions || []).includes(op);
      if (before.nextElementSibling !== row.node) before.after(row.node);
      before = row.node;
    }
    stale = false;
    grid.classList.remove("is-stale");
    notice.textContent = data.restoring
      ? "正在恢復應用，請稍候。"
      : !data.mounted
        ? data.storage
          ? "應用儲存位置尚未掛載，相關應用暫時無法使用。"
          : "先開啟應用商店，設定儲存位置並安裝應用。"
        : "狀態依實際背景服務更新；關閉網頁不會停止應用。";
    applyFilter();
  }
  function visible() {
    return (
      root.classList.contains("active") &&
      !document.hidden &&
      !document.getElementById("auth-modal")?.classList.contains("active")
    );
  }
  async function refresh(force = false) {
    if (!visible()) return;
    if (flight) return flight;
    if (!force && Date.now() - last < 4500) return;
    flight = (async () => {
      try {
        render(await api("apps/launcher"));
        last = Date.now();
      } catch (e) {
        stale = true;
        notice.textContent = "資訊已過期：" + e.message;
        grid.classList.add("is-stale");
        for (const row of cards.values()) {
          row.badge.dataset.state = "unknown";
          row.badge.textContent = "?";
          row.badge.setAttribute("aria-label", "資訊已過期");
          row.status.textContent = "資訊已過期";
          for (const [op, b] of Object.entries(row.controls))
            if (!["details", "update", "logs"].includes(op)) b.disabled = true;
        }
      } finally {
        flight = null;
      }
    })();
    return flight;
  }
  setInterval(() => refresh(), 5000);
  window.addEventListener(
    "scroll",
    () => {
      if (root.classList.contains("active")) scroll = window.scrollY;
    },
    { passive: true },
  );
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh(true);
  });
  window.addEventListener("nas:navigate", (e) => {
    if (e.detail.id === "apps") {
      requestAnimationFrame(() => window.scrollTo(0, scroll));
      refresh(true);
    }
  });
  return { refresh, details, selected: () => selectedApp };
};
