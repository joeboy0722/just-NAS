"use strict";
const $ = (id) => document.getElementById(id),
  apiRoot = "/api/app-services/vm-manager",
  labels = {
    running: "執行中",
    paused: "已暫停",
    stopped: "已關機",
    stopping: "關機中",
    failed: "異常",
    unknown: "無法確認",
    unavailable: "儲存不可用",
  },
  cards = new Map();
let csrf = "",
  caps = {},
  machines = [],
  isos = [],
  retained = [],
  flight = null,
  activeTab = "vms",
  rfb = null,
  lastSignature = "",
  stale = false,
  dialogResolve = null;
const el = (tag, text, cls) => {
  const n = document.createElement(tag);
  if (text !== undefined) n.textContent = text;
  if (cls) n.className = cls;
  return n;
};
const bytes = (n) => {
  n = Number(n) || 0;
  const i = n > 0 ? Math.min(4, Math.floor(Math.log(n) / Math.log(1024))) : 0;
  return (
    (n / 1024 ** i).toLocaleString("zh-TW", { maximumFractionDigits: 1 }) +
    " " +
    ["B", "KiB", "MiB", "GiB", "TiB"][i]
  );
};
function notice(t, error = false) {
  $("notice").textContent = t;
  $("notice").classList.toggle("error", error);
}
function theme() {
  const v = localStorage.getItem("nas-theme") || "system";
  $("theme").value = v;
  document.documentElement.dataset.theme =
    v === "system"
      ? matchMedia("(prefers-color-scheme:dark)").matches
        ? "dark"
        : "light"
      : v;
}
$("theme").onchange = () => {
  localStorage.setItem("nas-theme", $("theme").value);
  theme();
};
matchMedia("(prefers-color-scheme:dark)").addEventListener("change", theme);
window.addEventListener("storage", theme);
theme();
async function api(path, method = "GET", body) {
  const r = await fetch(apiRoot + path, {
    method,
    headers:
      method === "GET"
        ? {}
        : { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const value = await r.json();
  if (!r.ok) throw Error(value.error || "請求失敗");
  return value;
}
async function waitTask(id) {
  for (;;) {
    const j = await api("/tasks/" + id);
    notice(j.progress + (j.state === "running" ? "…" : ""));
    if (!["queued", "running"].includes(j.state)) {
      if (j.state !== "succeeded") throw Error(j.error || "任務未完成");
      return;
    }
    await new Promise((r) => setTimeout(r, 1000));
  }
}
async function perform(path, method = "POST", body = {}) {
  try {
    const r = await api(path, method, body);
    if (r.task_id) await waitTask(r.task_id);
    notice("操作完成");
    await refresh(true);
    return r;
  } catch (e) {
    notice(e.message, true);
    throw e;
  }
}
const button = (text, fn, cls) => {
  const b = el("button", text, cls);
  b.type = "button";
  b.onclick = () => Promise.resolve(fn()).catch((e) => notice(e.message, true));
  return b;
};
function field(parent, label, type, value, help = "") {
  const box = el(
      "label",
      undefined,
      "field" + (type === "checkbox" ? " check" : ""),
    ),
    span = el("span", label),
    input = el(type === "select" ? "select" : "input");
  if (type !== "select") input.type = type;
  input.setAttribute("aria-label", label);
  if (type === "checkbox") input.checked = !!value;
  else if (type !== "select") input.value = value ?? "";
  box.append(span, input);
  if (help) box.append(el("small", help));
  parent.append(box);
  return input;
}
function options(select, items, value) {
  for (const item of items) {
    const pair = Array.isArray(item) ? item : [item, item];
    select.add(new Option(pair[1], pair[0]));
  }
  if (value !== undefined) select.value = value;
}
function openDialog(title, help = "", save = "儲存") {
  const d = $("dialog");
  if (d.open) d.close();
  $("dialog-title").textContent = title;
  $("dialog-help").textContent = help;
  $("dialog-error").textContent = "";
  $("fields").replaceChildren();
  $("save").textContent = save;
  $("save").hidden = false;
  $("save").disabled = false;
  $("form").onsubmit = null;
  d.showModal();
  return $("fields");
}
$("cancel").onclick = () => {
  $("dialog").close();
  dialogResolve?.(false);
  dialogResolve = null;
};
$("dialog").addEventListener("cancel", () => {
  dialogResolve?.(false);
  dialogResolve = null;
});
async function confirmAction(text, name) {
  return new Promise((resolve) => {
    dialogResolve = resolve;
    const box = openDialog("確認操作", text, "確認");
    let input;
    if (name) input = field(box, "請輸入「" + name + "」確認", "text", "");
    $("form").onsubmit = (e) => {
      e.preventDefault();
      if (name && input.value !== name) {
        $("dialog-error").textContent = "名稱不一致";
        return;
      }
      $("dialog").close();
      dialogResolve = null;
      resolve(true);
    };
  });
}
function formSubmit(fn) {
  $("form").onsubmit = async (e) => {
    e.preventDefault();
    $("save").disabled = true;
    $("dialog-error").textContent = "";
    try {
      await fn();
      $("dialog").close();
      await refresh(true);
    } catch (error) {
      $("dialog-error").textContent = error.message;
    } finally {
      $("save").disabled = false;
    }
  };
}
function forwardsEditor(parent, rows) {
  const box = el("section", undefined, "fieldset");
  box.append(
    el("h3", "NAT 連接埠轉發"),
    el(
      "p",
      "選填。區網經 NAS 的指定連接埠連入 VM；主機連接埠須介於 1024–65535。",
    ),
  );
  const list = el("div");
  box.append(list);
  const controls = [];
  function row(
    value = {
      protocol: "tcp",
      host_ip: caps.ips?.[0] || "",
      host_port: 18090,
      guest_port: 80,
    },
  ) {
    const n = el("div", undefined, "forward-row"),
      protocol = el("select"),
      ip = el("select"),
      host = el("input"),
      guest = el("input");
    options(
      protocol,
      [
        ["tcp", "TCP"],
        ["udp", "UDP"],
      ],
      value.protocol,
    );
    options(ip, caps.ips || [], value.host_ip);
    protocol.setAttribute("aria-label", "協定");
    ip.setAttribute("aria-label", "NAS 位址");
    host.type = guest.type = "number";
    host.min = 1024;
    host.max = guest.max = 65535;
    guest.min = 1;
    host.value = value.host_port;
    guest.value = value.guest_port;
    host.setAttribute("aria-label", "主機連接埠");
    guest.setAttribute("aria-label", "客體連接埠");
    host.placeholder = "NAS 埠";
    guest.placeholder = "客體埠";
    const data = { n, protocol, ip, host, guest };
    controls.push(data);
    n.append(
      protocol,
      ip,
      host,
      guest,
      button(
        "×",
        () => {
          controls.splice(controls.indexOf(data), 1);
          n.remove();
        },
        "delete-forward",
      ),
    );
    list.append(n);
  }
  rows.forEach(row);
  box.append(button("＋ 新增轉發", () => row()));
  parent.append(box);
  return () =>
    controls.map((c) => ({
      protocol: c.protocol.value,
      host_ip: c.ip.value,
      host_port: Number(c.host.value),
      guest_port: Number(c.guest.value),
    }));
}
function machineForm(old) {
  const editing = !!old,
    box = openDialog(
      editing ? "編輯 " + old.name : "建立虛擬機",
      "CPU、記憶體、媒體與網路修改需先關機。擴大虛擬磁碟後，仍須在客體內擴充分割區。",
    );
  const name = field(box, "虛擬機名稱", "text", old?.name || ""),
    grid = el("div", undefined, "form-grid");
  box.append(grid);
  const os = field(grid, "作業系統", "select"),
    firmware = field(grid, "開機韌體", "select");
  options(
    os,
    [
      ["linux", "Linux"],
      ["windows10", "Windows 10"],
    ],
    old?.os || "linux",
  );
  options(
    firmware,
    [
      ["uefi", "UEFI（Secure Boot 關閉）"],
      ["bios", "傳統 BIOS"],
    ],
    old?.firmware || "uefi",
  );
  const cpu = field(grid, "虛擬 CPU", "number", old?.cpus || 2),
    memory = field(
      grid,
      "記憶體（GiB）",
      "number",
      (old?.memory_mib || 4096) / 1024,
    ),
    disk = field(grid, "磁碟容量（GiB）", "number", old?.disk_gib || 32),
    accel = field(grid, "加速方式", "select");
  cpu.min = 1;
  cpu.max = caps.cpus || 4;
  memory.min = 0.125;
  memory.step = 0.125;
  disk.min = old?.disk_gib || 4;
  options(
    accel,
    [
      ["auto", "自動：優先 KVM，否則 TCG"],
      ["kvm", "只用 KVM"],
      ["tcg", "只用 TCG"],
    ],
    old?.acceleration || "auto",
  );
  const media = field(box, "安裝 ISO", "select");
  options(
    media,
    [["", "不掛載安裝媒體"], ...isos.map((i) => [i.id, i.name])],
    old?.iso || "",
  );
  const bootISO = field(
    box,
    "下次優先從 ISO 開機（只用一次）",
    "checkbox",
    old?.boot_iso_once || false,
  );
  media.onchange = () => {
    bootISO.checked = !!media.value;
  };
  const autostart = field(
    box,
    "NAS 開機後自動啟動",
    "checkbox",
    old?.autostart || false,
  );
  const getForwards = forwardsEditor(box, old?.forwards || []);
  os.onchange = () => {
    if (!editing) disk.value = os.value === "windows10" ? 64 : 32;
  };
  formSubmit(async () => {
    const data = {
      name: name.value.trim(),
      os: os.value,
      cpus: Number(cpu.value),
      memory_mib: Math.round(Number(memory.value) * 1024),
      disk_gib: Number(disk.value),
      firmware: firmware.value,
      acceleration: accel.value,
      iso: media.value,
      boot_iso_once: bootISO.checked,
      autostart: autostart.checked,
      forwards: getForwards(),
    };
    if (
      editing &&
      data.firmware !== old.firmware &&
      !(await confirmBrowser("更換韌體可能讓既有系統無法開機，確定修改？"))
    )
      throw Error("已取消韌體變更");
    const r = await api(
      "/vms" + (editing ? "/" + old.id : ""),
      editing ? "PATCH" : "POST",
      data,
    );
    await waitTask(r.task_id);
  });
}
async function confirmBrowser(text) {
  return window.confirm(text);
}
async function vmAction(vm, action) {
  let confirm = "";
  if (
    action === "shutdown" &&
    !(await confirmAction(
      "向「" + vm.name + "」發出正常關機要求。等待超過 60 秒不會自動強制關閉。",
    ))
  )
    return;
  if (
    action === "restart" &&
    !(await confirmAction(
      "重新啟動「" + vm.name + "」會中斷目前工作，確定繼續？",
    ))
  )
    return;
  if (action === "force-stop") {
    if (!(await confirmAction("強制關機可能遺失客體尚未保存的資料。", vm.name)))
      return;
    confirm = vm.name;
  }
  await perform("/vms/" + vm.id + "/actions", "POST", { action, confirm });
}
async function removeVM(vm) {
  const box = openDialog(
    "移除 " + vm.name,
    "預設保留磁碟與設定，可在「設定與保留資料」重新登錄。",
    "移除",
  );
    const purge = field(box, "永久刪除此 VM 的磁碟、設定及資料集快照", "checkbox", !!vm.purge_pending),
    confirm = field(box, "永久刪除時請輸入 VM 名稱", "text", "");
  formSubmit(async () => {
    const r = await api("/vms/" + vm.id, "DELETE", {
      purge: purge.checked,
      confirm: confirm.value,
    });
    await waitTask(r.task_id);
  });
}
async function logs(vm) {
  const r = await api("/vms/" + vm.id + "/logs");
  const box = openDialog(vm.name + " · 日誌");
  box.append(el("pre", r.logs || "尚無日誌"));
  $("save").hidden = true;
}
function renderMachines() {
  const ids = new Set(machines.map((m) => m.id));
  for (const [id, c] of cards)
    if (!ids.has(id)) {
      c.node.remove();
      cards.delete(id);
    }
  if (!machines.length) {
    if (!$("machines").querySelector(".empty"))
      $("machines").append(
        el(
          "p",
          "尚未建立虛擬機。選擇儲存池並上傳 ISO 後，就能開始安裝。",
          "empty",
        ),
      );
  } else $("machines").querySelector(".empty")?.remove();
  for (const vm of machines) {
    let card = cards.get(vm.id);
    if (!card) {
      const node = el("article", undefined, "vm-card"),
        monitor = el("div", undefined, "monitor");
      monitor.innerHTML =
        '<svg viewBox="0 0 48 40" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="42" height="27" rx="4"/><path d="M17 37h14M24 30v7M10 10h8m-8 5h15"/></svg>';
      const badge = el("span", undefined, "badge"),
        name = el("h3"),
        resource = el("p", undefined, "detail"),
        storage = el("p", undefined, "detail"),
        reason = el("p", undefined, "reason"),
        actions = el("div", undefined, "actions"),
        open = button("開啟主控台", () => openConsole(card.vm)),
        start = button("啟動", () => vmAction(card.vm, "start")),
        menu = el("details"),
        summary = el("summary", "⋯");
      summary.setAttribute("aria-label", vm.name + " 的操作");
      const list = el("div", undefined, "menu");
      const operations = {};
      for (const [key, text, fn] of [
        ["shutdown", "正常關機", () => vmAction(card.vm, "shutdown")],
        ["restart", "重新啟動", () => vmAction(card.vm, "restart")],
        ["pause", "暫停", () => vmAction(card.vm, "pause")],
        ["resume", "繼續執行", () => vmAction(card.vm, "resume")],
        ["edit", "編輯設定", () => machineForm(card.vm)],
        ["logs", "查看日誌", () => logs(card.vm)],
        ["force-stop", "強制關機", () => vmAction(card.vm, "force-stop")],
        ["remove", "移除", () => removeVM(card.vm)],
      ]) {
        const b = button(
          text,
          () => {
            menu.open = false;
            return fn();
          },
          key === "force-stop" || key === "remove" ? "danger" : "",
        );
        list.append(b);
        operations[key] = b;
      }
      menu.append(summary, list);
      actions.append(open, start, menu);
      node.append(badge, monitor, name, resource, storage, reason, actions);
      $("machines").append(node);
      card = {
        node,
        badge,
        name,
        resource,
        storage,
        reason,
        open,
        start,
        operations,
        vm,
      };
      cards.set(vm.id, card);
    }
    card.vm = vm;
    card.name.textContent = vm.name;
    card.name.title = vm.name;
    card.badge.textContent = stale
      ? "資訊過期"
      : labels[vm.status] || vm.status;
    card.badge.dataset.state = stale ? "unknown" : vm.status;
    card.resource.textContent =
      vm.cpus +
      " vCPU · " +
      vm.memory_mib / 1024 +
      " GiB · " +
      (vm.actual_acceleration?.toUpperCase() || "尚未啟動");
    card.storage.textContent =
      "磁碟 " + vm.disk_gib + " GiB · 已用 " + bytes(vm.used_bytes);
    card.reason.textContent =
      vm.reason ||
      vm.error ||
      (vm.unclean ? "前次曾強制關機，請確認客體檔案系統。" : "");
    const on = ["running", "paused", "stopping", "unknown"].includes(vm.status),
      off = ["stopped", "failed"].includes(vm.status);
    card.open.disabled = stale || !["running", "paused"].includes(vm.status);
    card.start.hidden = on;
    card.start.disabled = stale || !off;
    for (const [key, b] of Object.entries(card.operations))
      b.disabled =
        stale ||
        (key === "pause"
          ? vm.status !== "running"
          : key === "resume"
            ? vm.status !== "paused"
            : ["edit", "remove"].includes(key)
              ? !off
              : key === "logs"
                ? false
                : !on);
  }
}
function renderLists(jobs) {
  const isoSig = JSON.stringify(isos);
  if ($("iso-list").dataset.sig !== isoSig) {
    $("iso-list").dataset.sig = isoSig;
    $("iso-list").replaceChildren();
    if (!isos.length) $("iso-list").append(el("p", "尚無安裝媒體", "empty"));
    for (const i of isos) {
      const row = el("div", undefined, "list-row"),
        text = el("div");
      text.append(
        el("strong", i.name),
        el("small", bytes(i.bytes) + " · SHA-256 " + i.sha256),
      );
      row.append(
        text,
        button("移除", async () => {
          if (await confirmAction("移除此 ISO？使用中的安裝媒體會拒絕刪除。"))
            await perform("/isos/" + i.id, "DELETE");
        }),
      );
      $("iso-list").append(row);
    }
  }
  const jobSig = JSON.stringify(jobs);
  if ($("jobs").dataset.sig !== jobSig) {
    $("jobs").dataset.sig = jobSig;
    $("jobs").replaceChildren();
    for (const j of jobs.slice(0, 50)) {
      const row = el("div", undefined, "list-row"),
        text = el("div");
      text.append(
        el("strong", j.progress),
        el(
          "small",
          new Date(j.created).toLocaleString("zh-TW") +
            " · " +
            ({
              queued: "排隊中",
              running: "執行中",
              succeeded: "完成",
              failed: "失敗",
              interrupted: "已中斷",
            }[j.state] || j.state),
        ),
      );
      if (j.error) text.append(el("p", j.error, "error"));
      row.append(text);
      $("jobs").append(row);
    }
    if (!jobs.length) $("jobs").append(el("p", "尚無任務", "empty"));
  }
  const retainedSig = JSON.stringify(retained);
  if ($("retained").dataset.sig !== retainedSig) {
    $("retained").dataset.sig = retainedSig;
    $("retained").replaceChildren();
    for (const m of retained) {
      const row = el("div", undefined, "list-row");
      row.append(
        el("strong", m.name),
          m.purge_pending ? el("span","永久刪除待完成；請重試刪除") : button("重新登錄", () => perform("/vms/" + m.id + "/restore")),
          button(m.purge_pending ? "重試永久刪除" : "刪除保留資料", () => removeVM(m)),
      );
      $("retained").append(row);
    }
    if (!retained.length)
      $("retained").append(el("p", "沒有保留的 VM 資料", "empty"));
  }
}
async function refresh(force = false) {
  if (document.hidden && !force) return;
  if (flight) return flight;
  flight = (async () => {
    try {
      const [c, v, i, j] = await Promise.all([
        api("/capabilities"),
        api("/vms"),
        api("/isos"),
        api("/tasks"),
      ]);
      caps = c;
      machines = v.vms;
      retained = v.retained;
      isos = i;
      stale = false;
      renderMachines();
      renderLists(j);
      $("storage").textContent = c.dataset
        ? c.dataset +
          " · 可用 " +
          bytes(c.available_bytes) +
          (c.storage_error ? " · " + c.storage_error : "")
        : "尚未設定 VM 儲存池";
      $("configure").hidden = !!c.dataset;
      $("create").disabled = !c.dataset || !!c.storage_error;
      $("upload").disabled = !c.dataset || !!c.storage_error;
      const sig = JSON.stringify([
        c.kvm,
        c.cpus,
        c.memory_mib,
        machines.map((m) => m.status),
      ]);
      if (sig !== lastSignature) {
        lastSignature = sig;
        $("summary").replaceChildren();
        for (const [title, value] of [
          [
            "虛擬機",
            machines.filter((m) => m.status === "running").length +
              " / " +
              machines.length +
              " 執行中",
          ],
          ["加速能力", c.kvm ? "KVM 可用" : "TCG 相容模式"],
          [
            "NAS 資源",
            c.cpus + " 核心 · " + Math.round(c.memory_mib / 1024) + " GiB",
          ],
        ]) {
          const box = el("div");
          box.append(el("span", title), el("strong", value));
          $("summary").append(box);
        }
      }
      if (force)
        notice(
          c.storage_error || "狀態已更新；關閉主控台不會停止虛擬機。",
          !!c.storage_error,
        );
    } catch (e) {
      stale = true;
      renderMachines();
      notice("資訊已過期：" + e.message, true);
    } finally {
      flight = null;
    }
  })();
  return flight;
}
async function openConsole(vm) {
  try {
    const { default: RFB } = await import("./novnc/rfb.js");
    rfb?.disconnect();
    $("screen").replaceChildren();
    $("console-title").textContent = vm.name + " · 主控台";
    $("console-status").textContent = "連線中…";
    $("console").showModal();
    const url = new URL(
      apiRoot + "/vms/" + vm.id + "/console",
      location.origin,
    );
    url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
    rfb = new RFB($("screen"), url.href);
    rfb.scaleViewport = true;
    rfb.resizeSession = false;
    rfb.addEventListener("connect", () => {
      $("console-status").textContent =
        "已連線 · " + (vm.actual_acceleration?.toUpperCase() || "");
    });
    rfb.addEventListener("disconnect", () => {
      $("console-status").textContent = "主控台已斷線，請確認登入或 VM 狀態。";
    });
  } catch (e) {
    notice(e.message, true);
  }
}
function closeConsole() {
  rfb?.disconnect();
  rfb = null;
  $("console").close();
  $("screen").replaceChildren();
}
$("close-console").onclick = closeConsole;
$("console").addEventListener("cancel", () => {
  rfb?.disconnect();
  rfb = null;
});
$("ctrl-alt-del").onclick = () => rfb?.sendCtrlAltDel();
$("fullscreen").onclick = () => $("console").requestFullscreen();
$("create").onclick = () => machineForm();
$("configure").onclick = () => {
  const box = openDialog(
    "選擇 VM 儲存池",
    "建立專用資料集，保存客體磁碟與 ISO。這個步驟不初始化或格式化任何硬碟。",
  );
  const pool = field(box, "儲存池", "select");
  options(pool, caps.pools || []);
  formSubmit(async () => {
    await api("/storage", "POST", { pool: pool.value });
  });
};
document.querySelectorAll("[data-tab]").forEach(
  (b) =>
    (b.onclick = () => {
      activeTab = b.dataset.tab;
      document
        .querySelectorAll("[data-tab]")
        .forEach((n) => n.setAttribute("aria-selected", String(n === b)));
      document
        .querySelectorAll(".panel")
        .forEach((n) => (n.hidden = n.id !== activeTab));
    }),
);
$("upload").onclick = () => $("iso-file").click();
$("iso-file").onchange = async () => {
  const file = $("iso-file").files[0];
  if (!file) return;
  if (file.size > 16 * 1024 ** 3) {
    notice("ISO 最大 16 GiB", true);
    return;
  }
  $("upload").disabled = true;
  $("upload-progress").hidden = false;
  try {
    await new Promise((resolve, reject) => {
      const x = new XMLHttpRequest();
      x.open("POST", apiRoot + "/isos?name=" + encodeURIComponent(file.name));
      x.setRequestHeader("X-CSRF-Token", csrf);
      x.setRequestHeader("Content-Type", "application/octet-stream");
      x.upload.onprogress = (e) => {
        if (e.lengthComputable) {
          $("upload-progress").value = (e.loaded / e.total) * 100;
          notice(
            "上傳 " +
              Math.floor((e.loaded / e.total) * 100) +
              "%；完成後計算校驗並登錄",
          );
        }
      };
      x.onload = () => {
        let v;
        try {
          v = JSON.parse(x.responseText);
        } catch {
          return reject(Error("上傳回應無效"));
        }
        if (x.status >= 200 && x.status < 300) resolve(v);
        else reject(Error(v.error || "上傳失敗"));
      };
      x.onerror = () => reject(Error("上傳連線中斷，可重新選擇檔案重試"));
      x.send(file);
    });
    notice("ISO 已完成校驗與登錄");
    await refresh();
  } catch (e) {
    notice(e.message, true);
  } finally {
    $("upload").disabled = false;
    $("upload-progress").hidden = true;
    $("iso-file").value = "";
  }
};
document.addEventListener("click", (e) => {
  document.querySelectorAll(".vm-card details[open]").forEach((d) => {
    if (!d.contains(e.target)) d.open = false;
  });
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape")
    document.querySelectorAll(".vm-card details[open]").forEach((d) => {
      d.open = false;
      d.querySelector("summary").focus();
    });
});
const auth = await (await fetch("/api/auth/status")).json();
if (!auth.authenticated) {
  location.assign("/");
} else {
  csrf = auth.csrf;
  await refresh(true);
  setInterval(() => refresh(), 5000);
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) refresh(true);
  });
}
