document.addEventListener("DOMContentLoaded", () => {
  "use strict";
  const $ = (id) => document.getElementById(id);
  let editingShare = null;
  let allUsers = [],
    allPools = [],
    allShares = [],
    allDatasets = [];
  window.nasData = () => ({
    users: allUsers,
    pools: allPools,
    shares: allShares,
    datasets: allDatasets,
  });
  let csrf = "",
    initialized = false,
    loggedIn = false,
    refreshing = false;
  const esc = (v) =>
    String(v ?? "").replace(
      /[&<>"']/g,
      (c) =>
        ({
          "&": "&amp;",
          "<": "&lt;",
          ">": "&gt;",
          '"': "&quot;",
          "'": "&#39;",
        })[c],
    );
  const bytes = NAS.bytes;
  const val = (id) => $(id).value.trim();
  function toast(message, error = false) {
    const el = document.createElement("div");
    el.className = "toast " + (error ? "toast-error" : "toast-success");
    el.textContent = message;
    $("toast-container").append(el);
    setTimeout(() => el.remove(), 7000);
  }
  function open(id) {
    $(id).classList.add("active");
  }
  function close(id) {
    $(id).classList.remove("active");
  }
  async function api(path, method = "GET", body) {
    const res = await fetch("/api/" + path, {
      method,
      headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(110000),
    });
    const data = await res.json();
    if (!res.ok) {
      if (res.status === 401) {
        loggedIn = false;
        open("auth-modal");
      }
      throw Error(data.error || "請求失敗 (" + res.status + ")");
    }
    return data;
  }
  function authUI(status) {
    if (status.recovery_error) {
      $("auth-error").textContent =
        "設定還原失敗，已停止初始化以保護原有設定：" + status.recovery_error;
      $("auth-submit").disabled = true;
      open("auth-modal");
      return;
    }
    initialized = status.initialized;
    loggedIn = status.authenticated;
    csrf = status.csrf || "";
    $("auth-title").textContent = initialized ? "管理員登入" : "首次設定";
    $("auth-token-group").hidden = initialized;
    $("auth-help").textContent = initialized
      ? "輸入管理員密碼。"
      : "從 VM 主控台取得 setup token，設定至少 12 字元的管理員密碼。建立第一個儲存池後，帳號才會永久保存。";
    if (!loggedIn) open("auth-modal");
    else close("auth-modal");
  }
  $("auth-form").onsubmit = async (e) => {
    e.preventDefault();
    const b = $("auth-submit");
    b.disabled = true;
    try {
      const result = await api(
        "auth/" + (initialized ? "login" : "setup"),
        "POST",
        { password: $("auth-password").value, token: val("auth-token") },
      );
      csrf = result.csrf;
      initialized = true;
      loggedIn = true;
      $("auth-password").value = "";
      $("auth-token").value = "";
      close("auth-modal");
      await refresh();
    } catch (err) {
      $("auth-error").textContent = err.message;
    } finally {
      b.disabled = false;
    }
  };
  $("btn-logout").onclick = async () => {
    try {
      await api("auth/logout", "POST");
      loggedIn = false;
      authUI(await api("auth/status"));
    } catch (err) {
      toast(err.message, true);
    }
  };
  $("btn-password").onclick = () => open("password-modal");
  $("password-form").onsubmit = async (e) => {
    e.preventDefault();
    const b = $("password-submit");
    b.disabled = true;
    try {
      if ($("password-new").value !== $("password-confirm").value)
        throw Error("兩次新密碼不一致");
      const r = await api("auth/password", "POST", {
        current_password: $("password-current").value,
        password: $("password-new").value,
      });
      csrf = r.csrf;
      $("password-form").reset();
      close("password-modal");
      toast("密碼已更新，其他登入工作階段已登出");
    } catch (err) {
      toast(err.message, true);
    } finally {
      b.disabled = false;
    }
  };
  document
    .querySelectorAll("[data-close]")
    .forEach((b) => (b.onclick = () => close(b.dataset.close)));
  document.querySelectorAll(".nav-item").forEach(
    (a) =>
      (a.onclick = (e) => {
        e.preventDefault();
        document
          .querySelectorAll(".nav-item,.section-page")
          .forEach((x) => x.classList.remove("active"));
        a.classList.add("active");
        $("sec-" + a.hash.slice(1)).classList.add("active");
        $("page-title").textContent = a.textContent.trim();
      }),
  );
  function render(id, rows, cols) {
    $(id).innerHTML = rows.length
      ? rows.join("")
      : '<tr><td colspan="' + cols + '" class="empty-row">尚無資料</td></tr>';
  }
  const cells = (values) =>
    values.map((v) => "<td>" + esc(v) + "</td>").join("");
  function button(label, fn) {
    const b = document.createElement("button");
    b.className = "btn btn-secondary btn-sm";
    b.textContent = label;
    b.onclick = async () => {
      b.disabled = true;
      try {
        await fn();
      } catch (err) {
        toast(err.message, true);
      } finally {
        b.disabled = false;
      }
    };
    return b;
  }
  function actions(tbody, index, items) {
    const row = $(tbody).rows[index];
    const td = row.insertCell();
    const buttons = items.map(([label, fn]) => button(label, fn));
    if (buttons.length) td.append(buttons.shift());
    if (buttons.length) td.append(NAS.menu(buttons));
  }
  async function destroy(path, name) {
    if (
      !(await NAS.confirm(
        "此操作會刪除 " + name + " 的資料，無法直接復原。",
        name,
      ))
    )
      return;
    await api(
      path +
        "?name=" +
        encodeURIComponent(name) +
        "&confirm=" +
        encodeURIComponent(name),
      "DELETE",
    );
    toast("已刪除");
    await refresh();
  }
  async function refresh() {
    if (
      !loggedIn ||
      refreshing ||
      document.querySelector(
        ".modal-backdrop.active:not(#auth-modal),.row-menu[open]",
      )
    )
      return;
    refreshing = true;
    const errors = [];
    async function load(label, fn) {
      try {
        await fn();
      } catch (err) {
        errors.push(label + "：" + err.message);
      }
    }
    await Promise.all([
      load("監控", async () => {
        const s = await api("system/status");
        $("val-cpu-pct").textContent = s.cpu_usage_pct.toFixed(1) + "%";
        $("fill-cpu").style.width = s.cpu_usage_pct + "%";
        $("val-loadavg").textContent =
          "Load: " + s.load_avg.map((n) => n.toFixed(2)).join(", ");
        $("val-ram-pct").textContent = s.ram_usage_pct.toFixed(1) + "%";
        $("fill-ram").style.width = s.ram_usage_pct + "%";
        $("val-ram-details").textContent =
          bytes(s.ram_used) + " / " + bytes(s.ram_total);
        $("val-uptime").textContent = s.uptime_hours.toFixed(1) + " 小時";
        $("val-os-info").textContent = s.os_info + " (" + s.arch + ")";
        $("sys-hostname").textContent = s.hostname;
      }),
      load("儲存池", async () => {
        const p = await api("storage/pools");
        allPools = p;
        render(
          "dash-tbody-pools",
          p.map(
            (s) =>
              "<tr>" +
              cells([
                s.name,
                bytes(s.size),
                bytes(s.allocated),
                bytes(s.free),
                s.capacity_pct + "%",
                s.health,
              ]) +
              "</tr>",
          ),
          6,
        );
        render(
          "tbody-pools",
          p.map(
            (s) =>
              "<tr>" +
              cells([
                s.name,
                bytes(s.size),
                bytes(s.allocated),
                bytes(s.free),
                s.capacity_pct + "%",
                s.fragmentation,
                s.health,
              ]) +
              "</tr>",
          ),
          8,
        );
        p.forEach((s, i) =>
          actions("tbody-pools", i, [
            [
              "狀態",
              async () => {
                const p = await api(
                  "storage/pool-status?name=" + encodeURIComponent(s.name),
                );
                $("diagnostics-output").textContent = p.status;
                open("diagnostics-modal");
              },
            ],
            [
              "Scrub",
              async () => {
                await api("tasks", "POST", { kind: "scrub", resource: s.name });
                toast("Scrub 已排入任務");
              },
            ],
            ["銷毀", () => destroy("storage/pools", s.name)],
          ]),
        );
        $("ds-parent-select").replaceChildren(
          ...p.map((s) => new Option(s.name, s.name)),
        );
      }),
      load("資料集", async () => {
        const d = await api("storage/datasets");
        allDatasets = d;
        render(
          "tbody-datasets",
          d.map(
            (s) =>
              "<tr>" +
              cells([
                s.name,
                bytes(s.used),
                bytes(s.available),
                s.compression,
                s.mountpoint,
                (s.quota ? bytes(s.quota) : "不限") +
                  (s.quota_locked
                    ? "；超額截止 " +
                      new Date(
                        (s.over_since + 30 * 86400) * 1000,
                      ).toLocaleString()
                    : ""),
              ]) +
              "</tr>",
          ),
          7,
        );
        d.forEach((s, i) =>
          actions(
            "tbody-datasets",
            i,
            s.name.includes("/")
              ? [
                  ["屬性／配額", () => editDataset(s)],
                  [
                    "快照",
                    () => {
                      $("snap-dataset-input").value = s.name;
                      open("modal-snapshot");
                    },
                  ],
                  ["刪除", () => destroy("storage/datasets", s.name)],
                ]
              : [],
          ),
        );
      }),
      load("快照", async () => {
        const snaps = await api("storage/snapshots");
        render(
          "tbody-snapshots",
          snaps.map(
            (s) =>
              "<tr>" +
              cells([
                s.name +
                  (s.created
                    ? " · " + new Date(s.created * 1000).toLocaleString("zh-TW")
                    : ""),
                bytes(s.used),
                bytes(s.referenced),
              ]) +
              "</tr>",
          ),
          4,
        );
        snaps.forEach((s, i) =>
          actions("tbody-snapshots", i, [
            [
              "還原",
              async () => {
                if (
                  !(await NAS.confirm(
                    "回滾會覆蓋目前內容。請先確認已有獨立備份。",
                    s.name,
                  ))
                )
                  return;
                await api("storage/snapshots/rollback", "POST", {
                  name: s.name,
                  confirm: s.name,
                });
                toast("快照已還原");
                await refresh();
              },
            ],
            ["刪除", () => destroy("storage/snapshots", s.name)],
          ]),
        );
      }),
      load("分享", async () => {
        const ss = await api("shares");
        allShares = ss;
        render(
          "tbody-shares",
          ss.map(
            (s) =>
              "<tr>" +
              cells([
                s.name + (s.disabled ? "（已停用）" : ""),
                s.protocol,
                s.path,
                s.read_only ? "唯讀" : "讀寫",
                s.protocol === "nfs"
                  ? s.allowed_hosts
                  : s.guest_ok
                    ? "訪客"
                    : (s.users || [])
                        .map(
                          (n) => n + (s.access?.[n] === "ro" ? "（唯讀）" : ""),
                        )
                        .join(", "),
                s.comment,
              ]) +
              "</tr>",
          ),
          7,
        );
        ss.forEach((s, i) =>
          actions("tbody-shares", i, [
            ["編輯", () => editShare(s)],
            [
              s.disabled ? "啟用" : "停用",
              async () => {
                await api("shares/" + s.id, "PATCH", { disabled: !s.disabled });
                await refresh();
              },
            ],
            ["連線資訊", () => showConnection(s)],
            ...(s.mode === "public"
              ? [
                  [
                    "權限",
                    () =>
                      window.nasForm(
                        "公共空間權限",
                        [
                          {
                            key: "access",
                            label: "選擇可存取的家庭帳號",
                            type: "permissions",
                            users: allUsers,
                            value: s.access || {},
                          },
                        ],
                        async (v) => {
                          const names = new Set([
                            ...Object.keys(s.access || {}),
                            ...Object.keys(v.access),
                          ]);
                          const labels = { rw: "讀寫", ro: "唯讀" };
                          const changes = [...names]
                            .filter((n) => s.access?.[n] !== v.access[n])
                            .map(
                              (n) =>
                                n +
                                "：" +
                                (labels[s.access?.[n]] || "不可存取") +
                                " → " +
                                (labels[v.access[n]] || "不可存取"),
                            );
                          if (!changes.length) return;
                          if (
                            !(await NAS.confirm(
                              changes.join("；") +
                                "。套用後 SMB 連線會重新建立。",
                            ))
                          )
                            throw Error("已取消套用");
                          await api("tasks", "POST", {
                            kind: "permissions",
                            resource: s.id + "|" + JSON.stringify(v.access),
                          });
                          toast("權限更新已排入任務");
                        },
                        "讀寫包含建立、修改與刪除。儲存後套用至既有檔案，SMB 連線將重新建立。",
                      ),
                  ],
                ]
              : []),
            [
              "移除",
              async () => {
                if (
                  !(await NAS.confirm("移除分享 " + s.name + "？檔案會保留。"))
                )
                  return;
                await api(
                  "shares?name=" +
                    encodeURIComponent(s.name) +
                    "&protocol=" +
                    s.protocol,
                  "DELETE",
                );
                toast("分享已移除");
                await refresh();
              },
            ],
          ]),
        );
      }),
      load("硬碟", async () => {
        const disks = await api("storage/smart");
        render(
          "tbody-disks",
          disks.map(
            (d) =>
              "<tr>" +
              cells([
                d.device,
                d.model || "虛擬硬碟／未知",
                bytes(d.size),
                d.temperature == null ? "不支援" : d.temperature + " °C",
                d.power_on_hours == null
                  ? "不支援"
                  : d.power_on_hours + " 小時",
                d.health === "UNKNOWN" ? "未知／不支援 SMART" : d.health,
              ]) +
              "</tr>",
          ),
          6,
        );
      }),
      load("日誌", async () => {
        const logs = await api("system/logs?limit=200");
        $("log-console").textContent =
          logs
            .map(
              (l) =>
                "[" +
                l.timestamp +
                "] [" +
                l.level +
                "] [" +
                l.tag +
                "] " +
                l.message,
            )
            .join("\n") || "尚無日誌";
      }),
      load("維護", async () => {
        if (window.refreshMaintenance) await window.refreshMaintenance();
      }),
      load("帳號", async () => {
        const users = await api("users");
        allUsers = users;
        $("users-list").replaceChildren(
          ...users.map((u) => {
            const d = document.createElement("div");
            const identity = document.createElement("strong");
            identity.textContent = u.name;
            const status = document.createElement("span");
            status.className = "status";
            status.textContent = u.disabled ? "已停用" : "已啟用";
            d.append(identity, status);
            d.append(
              button("變更密碼", async () => {
                window.nasForm(
                  "設定 " + u.name + " 的 SMB／網頁密碼",
                  [
                    {
                      key: "password",
                      label: "新密碼（12–128 字元）",
                      type: "password",
                      required: true,
                    },
                    {
                      key: "confirm",
                      label: "再次輸入密碼",
                      type: "password",
                      required: true,
                    },
                  ],
                  async (v) => {
                    if (v.password !== v.confirm) throw Error("兩次密碼不一致");
                    await api("users/" + u.id, "PATCH", {
                      password: v.password,
                    });
                    toast("密碼已變更");
                  },
                  "套用時 SMB 連線會重新建立。",
                );
              }),
              NAS.menu([
                button(u.disabled ? "啟用" : "停用", async () => {
                  if (
                    !(await NAS.confirm("更新帳號狀態，SMB 連線將重新建立？"))
                  )
                    return;
                  await api("users/" + u.id, "PATCH", {
                    disabled: !u.disabled,
                  });
                  await refresh();
                }),
                button("移除", async () => {
                  if (!(await NAS.confirm("移除帳號 " + u.name + "？"))) return;
                  await api(
                    "users?name=" + encodeURIComponent(u.name),
                    "DELETE",
                  );
                  await refresh();
                }),
              ]),
            );
            return d;
          }),
        );
        if (!$("modal-create-share").classList.contains("active"))
          $("share-users-select").replaceChildren(
            ...users.map((u) => new Option(u.name, u.name)),
          );
      }),
      load("登入狀態", async () => {
        const s = await api("auth/status");
        $("persistence-status").textContent = s.persistent
          ? "設定已保存於 ZFS"
          : "尚未建立設定池：目前管理員設定暫存於記憶體，請先建立儲存池。";
      }),
    ]);
    $("connection-status").textContent = errors.join("；");
    NAS.decorate?.();
    refreshing = false;
  }
  $("btn-refresh").onclick = refresh;
  $("btn-reboot").onclick = async () => {
    if (!(await NAS.confirm("確定重新啟動 NAS？分享會暫時中斷。"))) return;
    try {
      await api("system/reboot", "POST", { confirm: "reboot" });
      toast("NAS 正在重新啟動，請稍後重新整理並登入");
    } catch (err) {
      toast(err.message, true);
    }
  };
  $("btn-refresh-logs").onclick = refresh;
  $("btn-clear-logs").onclick = async () => {
    try {
      if (await NAS.confirm("清除記憶體日誌？")) {
        await api("system/logs", "DELETE");
        await refresh();
      }
    } catch (err) {
      toast(err.message, true);
    }
  };
  $("btn-diagnostics").onclick = async () => {
    try {
      const d = await api("system/diagnostics");
      $("diagnostics-output").textContent = d.checks
        .map((c) => (c.ok ? "✓ " : "✕ ") + c.name + "\n" + (c.detail || ""))
        .join("\n\n");
      open("diagnostics-modal");
    } catch (err) {
      toast(err.message, true);
    }
  };
  $("btn-open-create-pool").onclick = async () => {
    open("modal-create-pool");
    $("pool-disks-container").textContent = "掃描中…";
    try {
      const disks = await api("storage/disks");
      $("pool-disks-container").replaceChildren(
        ...disks.map((d) => {
          const l = document.createElement("label");
          l.className = "disk-item checkbox-group";
          const c = document.createElement("input");
          c.type = "checkbox";
          c.name = "pool-disks";
          c.value = d.path;
          c.disabled = !d.available;
          l.append(
            c,
            document.createTextNode(
              d.name + " — " + bytes(d.size) + " " + (d.reason || "可用"),
            ),
          );
          return l;
        }),
      );
      if (!disks.length)
        $("pool-disks-container").textContent = "找不到資料硬碟";
    } catch (err) {
      $("pool-disks-container").textContent = err.message;
    }
  };
  $("btn-open-create-ds").onclick = () => open("modal-create-dataset");
  $("btn-open-create-share").onclick = () => {
    editingShare = null;
    document.querySelector("#modal-create-share h3").textContent =
      "新建檔案共享 (SMB / NFS)";
    $("share-guest-input").disabled = false;
    $("share-path-input").disabled = false;
    $("share-name-input").disabled = false;
    $("share-proto-select").disabled = false;
    $("share-users-select").disabled = false;
    [
      "share-name-input",
      "share-path-input",
      "share-comment-input",
      "share-hosts-input",
    ].forEach((id) => ($(id).value = ""));
    $("share-readonly-input").checked = false;
    $("share-guest-input").checked = false;
    $("btn-submit-create-share").textContent = "確認發布";
    open("modal-create-share");
  };
  $("share-proto-select").onchange = () => {
    const smb = val("share-proto-select") === "smb";
    $("group-guest-ok").hidden = !smb;
    $("group-share-users").hidden = !smb;
    $("group-allowed-hosts").style.display = smb ? "none" : "block";
  };
  function submit(id, modal, fn) {
    $(id).onclick = async () => {
      const b = $(id);
      b.disabled = true;
      try {
        const result = await fn();
        if (result === false) return;
        if (modal) close(modal);
        toast("操作完成");
        await refresh();
      } catch (err) {
        toast(err.message, true);
      } finally {
        b.disabled = false;
      }
    };
  }
  submit("btn-submit-create-pool", "modal-create-pool", async () => {
    const name = val("pool-name-input");
    const devices = [
      ...document.querySelectorAll('input[name="pool-disks"]:checked'),
    ].map((c) => c.value);
    if (!name || !devices.length) throw Error("請輸入名稱並選擇硬碟");
    if (!(await NAS.confirm("將初始化選取的 NAS 裝置並清除其資料。", name)))
      return false;
    return api("storage/pools", "POST", {
      name,
      raid_type: val("pool-raid-select"),
      devices,
      confirm: name,
    });
  });
  submit("btn-submit-create-ds", "modal-create-dataset", () =>
    api("storage/datasets", "POST", {
      name: val("ds-parent-select") + "/" + val("ds-name-input"),
      compression: val("ds-comp-select"),
      mountpoint: val("ds-mount-input"),
    }),
  );
  submit("btn-submit-snap", "modal-snapshot", () =>
    api("storage/snapshots", "POST", {
      dataset: val("snap-dataset-input"),
      tag: val("snap-tag-input"),
    }),
  );
  submit("btn-submit-create-share", "modal-create-share", () => {
    const body = {
      path: val("share-path-input"),
      comment: val("share-comment-input"),
      read_only: $("share-readonly-input").checked,
      guest_ok: $("share-guest-input").checked,
      allowed_hosts: val("share-hosts-input"),
    };
    if (!editingShare || editingShare.mode === "legacy")
      body.users = [...$("share-users-select").selectedOptions].map(
        (o) => o.value,
      );
    if (editingShare) return api("shares/" + editingShare.id, "PATCH", body);
    return api("shares", "POST", {
      ...body,
      name: val("share-name-input"),
      protocol: val("share-proto-select"),
    });
  });
  $("user-form").onsubmit = async (e) => {
    e.preventDefault();
    const b = $("btn-create-user");
    b.disabled = true;
    try {
      await api("users", "POST", {
        name: val("user-name"),
        password: $("user-password").value,
      });
      $("user-password").value = "";
      toast("帳號已建立");
      await refresh();
    } catch (err) {
      toast(err.message, true);
    } finally {
      b.disabled = false;
    }
  };

  function editShare(s) {
    editingShare = s;
    document.querySelector("#modal-create-share h3").textContent =
      "編輯分享：" + s.name;
    $("share-name-input").value = s.name;
    $("share-name-input").disabled = true;
    $("share-proto-select").value = s.protocol;
    $("share-proto-select").disabled = true;
    $("share-proto-select").onchange();
    $("share-path-input").value = s.path;
    $("share-path-input").disabled = s.mode !== "legacy";
    $("share-comment-input").value = s.comment || "";
    $("share-readonly-input").checked = s.read_only;
    $("share-guest-input").checked = s.guest_ok;
    $("share-guest-input").disabled = s.mode !== "legacy";
    $("share-hosts-input").value = s.allowed_hosts || "";
    [...$("share-users-select").options].forEach(
      (o) => (o.selected = (s.users || []).includes(o.value)),
    );
    $("share-users-select").disabled = s.mode !== "legacy";
    $("btn-submit-create-share").textContent = "儲存修改";
    open("modal-create-share");
  }
  function showConnection(s) {
    const path =
      s.protocol === "smb"
        ? "\\\\" + location.hostname + "\\" + s.name
        : location.hostname + ":" + s.path;
    window.nasForm(
      "連線至 " + s.name,
      [
        {
          key: "path",
          label: s.protocol === "smb" ? "Windows 檔案總管路徑" : "NFS 掛載位置",
          value: path,
          readonly: true,
        },
      ],
      async () => {},
      (s.disabled ? "此分享目前已停用。" : "分享已啟用。") +
        (s.protocol === "smb"
          ? "在檔案總管網址列貼上路徑，使用家庭帳號登入。"
          : "使用 NFS 用戶端掛載，來源位址必須已獲授權。"),
    );
    const field = document.querySelector(".modal-backdrop:last-child input");
    const copy = button("複製連線路徑", async () => {
      try {
        await navigator.clipboard.writeText(path);
        toast("已複製");
      } catch {
        field.select();
        document.execCommand("copy");
      }
    });
    field.after(copy);
  }

  const spaceButton = button("建立個人／公共空間", () =>
    window.nasForm(
      "建立資料空間",
      [
        {
          key: "pool",
          label: "儲存池",
          options: allPools.map((p) => p.name),
          required: true,
        },
        { key: "name", label: "空間名稱（英文開頭）", required: true },
        {
          key: "kind",
          label: "空間類型",
          options: [
            { value: "private", label: "個人私有空間" },
            { value: "public", label: "家庭公共空間" },
          ],
        },
        {
          key: "owner",
          label: "擁有者",
          options: allUsers.filter((u) => !u.disabled).map((u) => u.name),
          when: (v) => v.kind === "private",
        },
        { key: "quota", label: "容量配額", type: "quota", value: "none" },
        {
          key: "access",
          label: "公共空間授權",
          type: "permissions",
          users: allUsers,
          value: {},
          when: (v) => v.kind === "public",
        },
      ],
      async (v) => {
        if (v.kind === "public" && !Object.keys(v.access).length)
          throw Error("請至少授權一位家庭帳號");
        await api("spaces", "POST", v);
        await refresh();
      },
      "個人空間只供擁有者存取；公共空間請逐一勾選授權。",
    ),
  );
  $("btn-open-create-share").after(spaceButton);

  function editDataset(s) {
    api("storage/dataset-properties?name=" + encodeURIComponent(s.name))
      .then((p) =>
        window.nasForm(
          "資料集屬性：" + s.name,
          [
            {
              key: "compression",
              label: "壓縮",
              options: [
                { value: "lz4", label: "快速壓縮（LZ4）" },
                { value: "zstd", label: "高壓縮率（Zstandard）" },
                { value: "off", label: "不壓縮" },
              ],
              value: p.compression,
            },
            {
              key: "quota",
              label: "容量配額",
              type: "quota",
              value:
                p["org.just-nas:quota"] === "0" ||
                p["org.just-nas:quota"] === "-"
                  ? "none"
                  : p["org.just-nas:quota"],
            },
          ],
          async (v) => {
            if (v.quota !== "none") {
              const size = NAS.quotaBytes(v.quota);
              if (
                size < Number(p.referenced) &&
                !(await NAS.confirm(
                  "目前檔案 " +
                    bytes(Number(p.referenced)) +
                    " 已超過新配額 " +
                    bytes(size) +
                    "。套用後只能讀取與刪除，30 天寬限期後依清理規則移除檔案。預計清理日期：" +
                    new Date(
                      (Number(p["org.just-nas:over-since"]) ||
                        Date.now() / 1000) *
                        1000 +
                        30 * 86400000,
                    ).toLocaleDateString("zh-TW"),
                ))
              )
                throw Error("已取消配額修改");
            }
            await api(
              "storage/dataset-properties?name=" + encodeURIComponent(s.name),
              "PATCH",
              v,
            );
            await refresh();
          },
          "目前引用 " +
            bytes(Number(p.referenced)) +
            "；快照占用 " +
            bytes(Number(p.usedbysnapshots)) +
            "。超額開始：" +
            (Number(p["org.just-nas:over-since"])
              ? new Date(
                  Number(p["org.just-nas:over-since"]) * 1000,
                ).toLocaleString()
              : "尚未超額") +
            "。調低配額不立即刪除；超額後保留讀取與刪除，30 天後先清到期回收內容，再刪最舊檔案直到符合配額，保留管理員快照。",
        ),
      )
      .catch((e) => toast(e.message, true));
  }
  window.initMaintenance({ api, refresh, button });
  NAS.layout();
  api("auth/status")
    .then((s) => {
      authUI(s);
      if (loggedIn) refresh();
    })
    .catch((err) => {
      $("auth-error").textContent = err.message;
      open("auth-modal");
    });
  setInterval(refresh, 10000);
});
