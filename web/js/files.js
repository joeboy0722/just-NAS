"use strict";
const $ = (id) => document.getElementById(id);
let csrf = "",
  spaces = [],
  current = "",
  folder = "",
  offset = 0,
  trash = false,
  loading = false,
  writable = false,
  writesAllowed = false,
  loadEpoch = 0;
function message(t) {
  $("message").textContent = t;
  $("message").hidden = !t;
}
function url(op, p = folder, extra = {}) {
  return (
    "/files-api/" +
    op +
    "?" +
    new URLSearchParams({ space: current, path: p, ...extra })
  );
}
async function request(op, method = "GET", body) {
  const r = await fetch(op, {
    method,
    headers: {
      "X-CSRF-Token": csrf,
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const b = await r.json().catch(() => ({ error: "服務回應失敗" }));
  if (!r.ok) {
    if (r.status === 401) {
      $("login").hidden = false;
      $("browser").hidden = true;
      $("logout").hidden = true;
    }
    const error = Error(b.error || "操作失敗 (" + r.status + ")");
    error.status = r.status;
    throw error;
  }
  return b;
}
function action(label, fn) {
  const b = document.createElement("button");
  b.textContent = label;
  b.onclick = async () => {
    b.disabled = true;
    try {
      await fn();
    } catch (e) {
      message(e.message);
    } finally {
      b.disabled = false;
    }
  };
  return b;
}
function join(name) {
  return folder ? folder + "/" + name : name;
}
function switchSpace(id) {
  current = id;
  folder = "";
  offset = 0;
  trash = false;
  document.body.classList.remove("nav-open");
  return load();
}
async function loadSpaces() {
  const r = await request("/files-api/spaces");
  csrf = r.csrf;
  spaces = r.spaces || [];
  $("who").textContent = r.user;
  $("space").replaceChildren(...spaces.map((s) => new Option(s.name, s.id)));
  $("login").hidden = true;
  $("browser").hidden = false;
  $("logout").hidden = false;
  current = spaces.some((s) => s.id === current)
    ? current
    : spaces[0]?.id || "";
  $("space-nav").replaceChildren(
    ...spaces.map((s) => {
      const b = action(s.name, () => switchSpace(s.id));
      b.className = "nav-item";
      b.dataset.space = s.id;
      const icon = document.createElement("span");
      icon.className = "icon";
      icon.innerHTML = NAS.icon("folder");
      b.prepend(icon);
      return b;
    }),
  );
  const recycle = action("回收區", async () => {
    trash = true;
    folder = "";
    offset = 0;
    await load();
  });
  recycle.className = "nav-item";
  $("space-nav").append(recycle);
  await load();
}
function crumbs(s) {
  $("breadcrumbs").replaceChildren();
  const root = action(trash ? "回收區 · " + s.name : s.name, async () => {
    folder = "";
    offset = 0;
    await load();
  });
  $("breadcrumbs").append(root);
  const parts = folder.split("/").filter(Boolean);
  parts.forEach((p, i) => {
    $("breadcrumbs").append(
      document.createTextNode("/"),
      action(p, async () => {
        folder = parts.slice(0, i + 1).join("/");
        offset = 0;
        await load();
      }),
    );
  });
}
async function pickFolder(title, initial = "") {
  return new Promise((resolve) => {
    const d = document.createElement("dialog");
    const h = document.createElement("h2");
    h.textContent = title;
    const path = document.createElement("p");
    path.className = "picker-current";
    const list = document.createElement("div");
    list.className = "picker-list";
    const err = document.createElement("p");
    err.className = "error";
    let at = initial;
    const up = action("上一層", async () => {
      at = at.split("/").slice(0, -1).join("/");
      await show();
    });
    const select = action("選擇此資料夾", async () => {
      d.close("selected");
    });
    select.className = "btn btn-primary";
    const cancel = action("取消", async () => d.close());
    async function show() {
      select.disabled = false;
      err.textContent = "";
      path.textContent = "/" + at;
      up.disabled = !at;
      list.replaceChildren();
      try {
        let page = 0;
        do {
          const r = await request(url("list", at, { offset: page }));
          r.entries
            .filter((e) => e.directory)
            .forEach((e) =>
              list.append(
                action(e.name, async () => {
                  at = at ? at + "/" + e.name : e.name;
                  await show();
                }),
              ),
            );
          if (!r.more) break;
          page = r.next_offset ?? page + 100;
        } while (page < 100000);
        if (!list.children.length) list.textContent = "此位置沒有子資料夾";
      } catch (e) {
        err.textContent = e.message;
        select.disabled = true;
      }
    }
    d.append(h, path, up, list, err, cancel, select);
    d.onclose = () => {
      resolve(d.returnValue === "selected" ? at : null);
      d.remove();
    };
    document.body.append(d);
    d.showModal();
    show();
  });
}
async function history(path) {
  const v = await request(url("versions", ""));
  if (!v.snapshots?.length) {
    message("尚無歷史快照；快照只保護拍攝時已存在的內容。");
    return;
  }
  const values = await NAS.dialog(
    "恢復歷史版本",
    [
      {
        key: "snapshot",
        label: "選擇快照版本",
        options: v.snapshots
          .slice()
          .reverse()
          .map((s) => ({
            value: s,
            label: (() => {
              const meta = v.versions?.find((x) => x.name === s);
              return (
                (meta?.created
                  ? new Date(meta.created * 1000).toLocaleString("zh-TW") +
                    " · "
                  : "") + (s.includes("@") ? s.split("@")[1] : s)
              );
            })(),
          })),
      },
      {
        key: "name",
        label: "恢復後檔名",
        value: path.split("/").at(-1) + ".restored",
        required: true,
      },
    ],
    "會另存新檔，不覆蓋目前內容。",
  );
  if (!values) return;
  const dest = await pickFolder("選擇恢復位置", folder);
  if (dest === null) return;
  await request(
    url("recover-version", path, {
      snapshot: values.snapshot,
      target: dest ? dest + "/" + values.name : values.name,
    }),
    "POST",
    {},
  );
  await load();
  message("歷史版本已恢復到新位置");
}
async function rename(path, e) {
  const name = await NAS.prompt("新的檔案或資料夾名稱", e.name);
  if (!name || name === e.name) return;
  if (name.includes("/") || name === "." || name === "..")
    throw Error("名稱不能包含路徑分隔符號");
  await request(url("move", path, { target: join(name) }), "POST", {});
  await load();
}
async function move(path, e) {
  const dest = await pickFolder("移動至其他資料夾");
  if (dest === null) return;
  const target = dest ? dest + "/" + e.name : e.name;
  if (target === path) return;
  await request(url("move", path, { target }), "POST", {});
  await load();
}
async function load() {
  const epoch = ++loadEpoch;
  loading = true;
  message("");
  if (!current) {
    $("entries").innerHTML =
      '<tr><td colspan="4" class="empty-row">尚無授權空間，請聯絡管理員。</td></tr>';
    return;
  }
  const s = spaces.find((s) => s.id === current);
  writable = !s.read_only && s.access?.[$("who").textContent] === "rw";
  $("upload-button").disabled = $("mkdir").disabled = !writable || trash;
  $("space-title").textContent = trash ? "回收區" : s.name;
  $("space-state").textContent = writable
    ? "可讀寫 · " + (s.mode === "private" ? "個人私有空間" : "家庭共用空間")
    : "僅可讀取與下載；寫入權限或容量配額受到限制。";
  $("trash-mode").textContent = trash ? "返回檔案" : "回收區";
  $("up").disabled = !folder;
  document
    .querySelectorAll("[data-space]")
    .forEach((b) => b.classList.toggle("active", b.dataset.space === current));
  crumbs(s);
  try {
    const [r, budget] = await Promise.all([
      request(url(trash ? "trash-list" : "list", folder, { offset })),
      request(url("budget")).catch(() => null),
    ]);
    const quotaLocked =
      !budget || (budget.limited && budget.used > budget.quota);
    const canWrite = writable && !quotaLocked;
    writesAllowed = canWrite;
    $("upload-button").disabled = $("mkdir").disabled = !canWrite || trash;
    if (quotaLocked && writable)
      $("space-state").textContent = budget
        ? "已超過配額：目前僅可讀取、下載與刪除；30 天寬限期後依規則清理。"
        : "無法確認容量狀態，暫停寫入，仍可讀取與刪除。";
    if (budget?.limited && !quotaLocked)
      $("space-state").textContent +=
        " · 配額用量 " +
        NAS.bytes(budget.used) +
        "／" +
        NAS.bytes(budget.quota);
    if (epoch !== loadEpoch) return;
    const rows = [];
    for (const e of r.entries) {
      const tr = document.createElement("tr"),
        name = document.createElement("td"),
        size = document.createElement("td"),
        modified = document.createElement("td"),
        acts = document.createElement("td");
      const wrap = document.createElement("div");
      wrap.className = "file-name";
      const icon = document.createElement("span");
      icon.className = "icon";
      icon.innerHTML = NAS.icon(e.directory ? "folder" : "file");
      const text = e.directory
        ? action(e.name, async () => {
            folder = join(e.name);
            offset = 0;
            await load();
          })
        : document.createElement("span");
      text.textContent = e.name;
      text.title = e.name;
      wrap.append(icon, text);
      name.append(wrap);
      if (trash) {
        const original = document.createElement("small");
        original.className = "recycle-original";
        original.textContent = "原位置：" + (e.original || join(e.name));
        original.title = original.textContent;
        name.append(original);
      }
      size.textContent = e.directory ? "資料夾" : NAS.bytes(e.size);
      modified.textContent = new Date(e.modified).toLocaleString("zh-TW", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
      });
      modified.title = modified.textContent;
      const path = join(e.name),
        items = [];
      if (!e.directory) {
        const a = document.createElement("a");
        a.href = url(trash ? "trash-download" : "download", path);
        a.textContent = "下載";
        a.className = "btn row-download";
        acts.append(a);
        const download = a.cloneNode(true);
        download.className = "btn";
        items.push(download);
      }
      if (trash) {
        if (writable) {
          if (canWrite)
            items.push(
              action("恢復到…", async () => {
                const targetName = await NAS.prompt(
                  "恢復後名稱",
                  (e.original || e.name).split("/").at(-1),
                );
                if (!targetName) return;
                if (targetName.includes("/")) throw Error("請使用單一檔案名稱");
                const dest = await pickFolder("選擇恢復位置");
                if (dest === null) return;
                await request(
                  url("restore", path, {
                    target: dest ? dest + "/" + targetName : targetName,
                  }),
                  "POST",
                  {},
                );
                await load();
              }),
            );
          items.push(
            action("永久刪除", async () => {
              if (
                !(await NAS.confirm(
                  "永久刪除「" + e.name + "」？此操作無法從回收區復原。",
                  e.name,
                ))
              )
                return;
              await request(url("purge", path), "POST", {});
              await load();
            }),
          );
        }
      } else {
        if (!e.directory && canWrite)
          items.push(action("歷史版本", () => history(path)));
        if (writable) {
          if (canWrite)
            items.push(
              action("重新命名", () => rename(path, e)),
              action("移動至…", () => move(path, e)),
            );
          items.push(
            action("移入回收區", async () => {
              if (!(await NAS.confirm("將「" + e.name + "」移入回收區？")))
                return;
              await request(url("trash", path), "POST", {});
              await load();
            }),
          );
        }
      }
      if (items.length) acts.append(NAS.menu(items));
      tr.append(name, size, modified, acts);
      rows.push(tr);
    }
    $("entries").replaceChildren(...rows);
    if (!rows.length)
      $("entries").innerHTML =
        '<tr><td colspan="4" class="empty-row">' +
        (trash ? "回收區目前沒有內容" : "此資料夾尚無檔案") +
        "</td></tr>";
    $("next").hidden = !r.more;
    $("previous").hidden = offset === 0;
    $("page-info").textContent = rows.length
      ? "第 " + (offset + 1) + "–" + (offset + rows.length) + " 筆"
      : "";
    $("next").onclick = async () => {
      offset = r.next_offset ?? offset + 100;
      await load();
    };
  } finally {
    loading = false;
  }
}
$("login-form").onsubmit = async (e) => {
  e.preventDefault();
  const b = e.submitter;
  b.disabled = true;
  try {
    const r = await request("/files-api/login", "POST", {
      name: $("username").value.trim(),
      password: $("password").value,
    });
    csrf = r.csrf;
    $("password").value = "";
    await loadSpaces();
  } catch (e) {
    message(e.message);
  } finally {
    b.disabled = false;
  }
};
$("space").onchange = () =>
  switchSpace($("space").value).catch((e) => message(e.message));
$("refresh").onclick = () => loadSpaces().catch((e) => message(e.message));
$("previous").onclick = () => {
  offset = Math.max(0, offset - 100);
  load().catch((e) => message(e.message));
};
$("up").onclick = () => {
  folder = folder.split("/").slice(0, -1).join("/");
  offset = 0;
  load().catch((e) => message(e.message));
};
$("trash-mode").onclick = () => {
  trash = !trash;
  folder = "";
  offset = 0;
  load().catch((e) => message(e.message));
};
$("mkdir").onclick = async () => {
  const name = await NAS.prompt("新資料夾名稱");
  if (!name) return;
  try {
    if (name.includes("/")) throw Error("名稱不能包含路徑分隔符號");
    await request(url("mkdir", join(name)), "POST", {});
    await load();
  } catch (e) {
    message(e.message);
  }
};
$("upload-button").onclick = () => $("upload").click();
function transfer(file, replace, destination) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", destination + "&replace=" + replace);
    xhr.setRequestHeader("X-CSRF-Token", csrf);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable)
        $("upload-progress").value = (e.loaded / e.total) * 100;
    };
    xhr.onload = () => {
      let b;
      try {
        b = JSON.parse(xhr.responseText);
      } catch {
        b = { error: "上傳回應失敗" };
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(b);
      else {
        const err = Error(b.error || "上傳失敗");
        err.status = xhr.status;
        reject(err);
      }
    };
    xhr.onerror = () => reject(Error("連線中斷，上傳未完成，請重新選擇檔案"));
    xhr.send(file);
  });
}
$("upload").onchange = async () => {
  const file = $("upload").files[0];
  if (!file) return;
  const saved = { space: current, path: folder },
    destination = url("upload", join(file.name));
  const navButtons = [
    ...document.querySelectorAll(
      "#space-nav button,#breadcrumbs button,#up,#refresh,#trash-mode,#mkdir,#previous,#next",
    ),
  ];
  const prior = navButtons.map((b) => b.disabled);
  navButtons.forEach((b) => (b.disabled = true));
  try {
    const budget = await request(url("budget"));
    if (budget.limited && file.size > budget.remaining)
      throw Error(
        "檔案超過剩餘配額 " + NAS.bytes(budget.remaining) + "，請先清理空間。",
      );
    $("upload-progress").hidden = false;
    $("upload-progress").value = 0;
    $("upload-button").disabled = true;
    try {
      await transfer(file, false, destination);
    } catch (e) {
      if (e.status !== 409 && !/file exists|already exists/i.test(e.message))
        throw e;
      if (
        !(await NAS.confirm(
          "已有「" + file.name + "」。替換會覆蓋目前檔案，是否繼續？",
        ))
      ) {
        message("已取消替換");
        return;
      }
      if (current !== saved.space || folder !== saved.path)
        throw Error("目前位置已變更，請重新上傳");
      await transfer(file, true, destination);
    }
    await load();
    message("上傳完成：" + file.name);
  } catch (e) {
    message(e.message);
  } finally {
    navButtons.forEach((b, i) => (b.disabled = prior[i]));
    $("upload-progress").hidden = true;
    $("upload-button").disabled = !writesAllowed || trash;
    $("upload").value = "";
  }
};
$("logout").onclick = async () => {
  try {
    await request("/files-api/logout", "POST", {});
    location.reload();
  } catch (e) {
    message(e.message);
  }
};
loadSpaces().catch((e) => {
  if (e.status !== 401) message(e.message);
});
