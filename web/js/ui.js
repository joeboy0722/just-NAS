/* Theme is applied before paint on both entry points. */
(() => {
  "use strict";
  const stored = localStorage.getItem("nas-theme") || "system";
  const media = matchMedia("(prefers-color-scheme: dark)");
  function apply(choice) {
    document.documentElement.dataset.theme =
      choice === "system" ? (media.matches ? "dark" : "light") : choice;
  }
  apply(stored);
  window.addEventListener("storage", (e) => {
    if (e.key === "nas-theme") {
      apply(e.newValue || "system");
      document
        .querySelectorAll(".theme-select")
        .forEach((s) => (s.value = e.newValue || "system"));
    }
  });
  media.addEventListener("change", () =>
    apply(localStorage.getItem("nas-theme") || "system"),
  );
  window.NAS = {};
  NAS.bytes = (v) => {
    v = Number(v) || 0;
    const i = v > 0 ? Math.min(4, Math.floor(Math.log(v) / Math.log(1024))) : 0;
    return (
      (v / 1024 ** i).toLocaleString("zh-TW", {
        maximumFractionDigits: i ? 1 : 0,
      }) +
      " " +
      ["B", "KiB", "MiB", "GiB", "TiB"][i]
    );
  };
  NAS.theme = () => {
    const s = document.createElement("select");
    s.className = "theme-select";
    s.setAttribute("aria-label", "外觀主題");
    for (const [v, t] of [
      ["system", "跟隨系統"],
      ["light", "亮色主題"],
      ["dark", "暗色主題"],
    ])
      s.add(new Option(t, v));
    s.value = localStorage.getItem("nas-theme") || "system";
    s.onchange = () => {
      localStorage.setItem("nas-theme", s.value);
      apply(s.value);
      document
        .querySelectorAll(".theme-select")
        .forEach((x) => (x.value = s.value));
    };
    return s;
  };
  NAS.icon = (name = "grid") => {
    const paths = {
      grid: "M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z",
      disk: "M4 3h16v18H4z M8 16h8 M8 7h8",
      folder: "M3 7V4h6l3 3h9v13H3z",
      users:
        "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2 M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8 M18 8a3 3 0 0 1 0 6 M22 21v-2a4 4 0 0 0-3-4",
      link: "M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-2 2 M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l2-2",
      shield: "M12 2l9 4v6c0 5-9 10-9 10S3 17 3 12V6z M8 12l3 3 5-6",
      clock: "M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18 M12 7v5l4 2",
      settings: "M4 6h16 M4 12h16 M4 18h16 M8 3v6 M16 9v6 M10 15v6",
      file: "M5 2h9l5 5v15H5z M14 2v6h5 M8 13h8 M8 17h6",
    };
    return (
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">' +
      (paths[name] || paths.grid)
        .split(" M")
        .map((p, i) => '<path d="' + (i ? "M" : "") + p + '"/>')
        .join("") +
      "</svg>"
    );
  };
  NAS.menu = (items) => {
    const d = document.createElement("details");
    d.className = "row-menu";
    const s = document.createElement("summary");
    s.textContent = "更多 ⋯";
    s.setAttribute("aria-label", "更多操作");
    const c = document.createElement("div");
    c.className = "menu-content";
    items.forEach((b) => {
      b.addEventListener("click", () => (d.open = false));
      c.append(b);
    });
    d.append(s, c);
    return d;
  };
  NAS.dialog = (title, fields = [], help = "") =>
    new Promise((resolve) => {
      const overlay = window.nasForm(
        title,
        fields,
        async (v) => {
          resolve(v);
        },
        help,
      );
      overlay.querySelector("button.btn-primary").textContent = "確認";
      overlay.addEventListener("nas-cancel", () => resolve(null), {
        once: true,
      });
    });
  NAS.confirm = async (text, name) => {
    const v = await NAS.dialog(
      "確認操作",
      name
        ? [
            {
              key: "confirm",
              label: "請輸入「" + name + "」確認",
              required: true,
            },
          ]
        : [],
      text,
    );
    return v !== null && (!name || v.confirm === name);
  };
  NAS.prompt = async (text, value = "") => {
    const v = await NAS.dialog("輸入資料", [
      { key: "value", label: text, value, required: true },
    ]);
    return v?.value ?? null;
  };
  document.addEventListener("DOMContentLoaded", () => {
    document
      .querySelectorAll("[data-theme-picker]")
      .forEach((x) => x.append(NAS.theme()));
    document.addEventListener(
      "toggle",
      (e) => {
        const d = e.target;
        if (!d.matches?.(".row-menu") || !d.open) return;
        const menu = d.querySelector(".menu-content");
        if (!menu) return;
        const rect = d.querySelector("summary").getBoundingClientRect();
        menu.style.right = "auto";
        menu.style.left =
          Math.max(8, Math.min(rect.right - 190, innerWidth - 198)) + "px";
        menu.style.width = "190px";
        menu.style.maxHeight = innerHeight - 24 + "px";
        menu.style.overflowY = "auto";
        menu.style.top =
          Math.min(
            rect.bottom + 5,
            Math.max(8, innerHeight - menu.offsetHeight - 10),
          ) + "px";
      },
      true,
    );
    document.addEventListener("click", (e) => {
      document.querySelectorAll(".row-menu[open]").forEach((d) => {
        if (!d.contains(e.target)) d.open = false;
      });
    });
    const original = new WeakMap();
    const setup = (el) => {
      if (original.has(el)) return;
      original.set(el, document.activeElement);
      el.setAttribute("role", "dialog");
      el.setAttribute("aria-modal", "true");
      const h = el.querySelector("h2,h3");
      if (h) {
        h.id = h.id || "dialog-title-" + Math.random().toString(36).slice(2);
        el.setAttribute("aria-labelledby", h.id);
      }
      setTimeout(
        () =>
          el
            .querySelector("input:not(:disabled),select:not(:disabled),button")
            ?.focus(),
        0,
      );
    };
    new MutationObserver((records) => {
      for (const record of records) {
        const el = record.target;
        if (el.classList?.contains("modal-backdrop")) {
          if (el.classList.contains("active")) setup(el);
          else {
            original.get(el)?.focus?.();
            original.delete(el);
          }
        }
        for (const node of record.addedNodes || [])
          if (node.nodeType === 1 && node.matches(".modal-backdrop.active"))
            setup(node);
        for (const node of record.removedNodes || [])
          if (original.has(node)) {
            original.get(node)?.focus?.();
            original.delete(node);
          }
      }
    }).observe(document.body, {
      subtree: true,
      childList: true,
      attributes: true,
      attributeFilter: ["class"],
    });
    document.addEventListener("keydown", (e) => {
      const overlays = [...document.querySelectorAll(".modal-backdrop.active")];
      const top = overlays.at(-1);
      if (!top) return;
      if (e.key === "Escape" && top.id !== "auth-modal") {
        e.preventDefault();
        const cancel = top.querySelector("[data-cancel],[data-close]");
        cancel?.click();
      }
      if (e.key === "Tab") {
        const focusable = [
          ...top.querySelectorAll(
            "input,select,button,textarea,a[href],summary",
          ),
        ].filter((x) => !x.disabled && !x.hidden && x.getClientRects().length);
        if (!focusable.length) return;
        const first = focusable[0],
          last = focusable.at(-1);
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    });
  });
})();
