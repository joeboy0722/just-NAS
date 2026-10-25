/* Browser fixtures deliberately separate UI acceptance from real VM acceptance. */
const { chromium } = require("playwright");
const http = require("http"),
  fs = require("fs"),
  path = require("path"),
  assert = require("assert/strict");
const root = path.resolve(__dirname, ".."),
  out = path.join(root, "output/launcher-review");
fs.mkdirSync(out, { recursive: true });
const manifest = {
  id: "static-site",
  name: "靜態網站示範",
  version: "1.0.0",
  kind: "app",
  installed_bytes: 5000000,
  license: "MIT",
  permissions: ["selected-content-dataset", "lan-port"],
  dependencies: { "container-engine": "1.0.0" },
};
const engine = {
  id: "container-engine",
  name: "Docker 容器引擎",
  version: "1.0.0",
  kind: "runtime",
  installed_bytes: 10000000,
  license: "See licenses",
  permissions: ["system-service"],
  dependencies: {},
};
const app = {
  id: "static-site",
  package: { manifest },
  enabled: true,
  status: "active",
  bind_ip: "192.168.56.101",
  port: 18080,
};
const state = {
  mounted: true,
  ips: ["192.168.56.101"],
  state: {
    dataset: "tank/.just-nas-extensions",
    installed: {
      "container-engine": {
        package: { manifest: engine },
        enabled: true,
        status: "active",
      },
    },
  },
  apps: { apps: { "static-site": app } },
  imports: [],
  retained_data: [],
};
const entry = (id, name, status = "active") => ({
  id,
  name,
  version: "1.0.0",
  status,
  open_mode: status === "active" ? "external" : "none",
  url: status === "active" ? "http://127.0.0.1:19000/" : "",
  actions: [
    "details",
    "logs",
    "update",
    "remove",
    status === "active" ? "stop" : "start",
    ...(status === "active" ? ["open"] : []),
  ],
});
let entries = [entry("static-site", "靜態網站示範")],
  offline = false,
  catalogFail = false,
  launcherDelay = 0,
  activeQueries = 0,
  maxQueries = 0,
  queryCount = 0;
const mutations = [],
  jobs = [];
const fixtures = {
  "auth/status": {
    initialized: true,
    authenticated: true,
    persistent: true,
    csrf: "fixture",
  },
  "system/status": {
    cpu_usage_pct: 1,
    load_avg: [0, 0, 0],
    ram_usage_pct: 20,
    ram_used: 200,
    ram_total: 1000,
    uptime_hours: 1,
    os_info: "Alpine",
    arch: "x86_64",
    hostname: "just-nas",
  },
  "storage/pools": [
    {
      name: "tank",
      health: "ONLINE",
      size: 1e10,
      allocated: 100,
      free: 1e10 - 100,
    },
  ],
  "storage/datasets": [
    {
      name: "tank/files",
      mountpoint: "/mnt/tank/files",
      used: 0,
      available: 1e9,
    },
  ],
  network: { settings: {}, interfaces: [] },
  backup: {},
};
async function main() {
  const server = http.createServer((q, r) => {
    const file = path.join(
      root,
      "web",
      q.url === "/" ? "index.html" : q.url.split("?")[0],
    );
    try {
      r.setHeader(
        "Content-Type",
        file.endsWith(".js")
          ? "application/javascript"
          : file.endsWith(".css")
            ? "text/css"
            : "text/html",
      );
      r.end(fs.readFileSync(file));
    } catch {
      r.writeHead(404);
      r.end();
    }
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const browser = await chromium.launch({
    executablePath:
      process.env.NAS_CHROMIUM_PATH ||
      "C:/Program Files/Google/Chrome/Application/chrome.exe",
    headless: true,
  });
  const checks = [],
    errors = [];
  try {
    const context = await browser.newContext(),
      page = await context.newPage();
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/api/**", async (route) => {
      const req = route.request(),
        ep = new URL(req.url()).pathname.slice(5);
      if (ep === "apps/launcher") {
        queryCount++;
        activeQueries++;
        maxQueries = Math.max(maxQueries, activeQueries);
        if (launcherDelay)
          await new Promise((r) => setTimeout(r, launcherDelay));
        activeQueries--;
        return route.fulfill({
          status: offline ? 503 : 200,
          json: offline
            ? { error: "連線中斷" }
            : {
                apps: entries,
                mounted: state.mounted,
                storage: state.state.dataset,
              },
        });
      }
      if (ep.endsWith("/icon"))
        return route.fulfill({ status: 404, body: "missing" });
      if (ep.endsWith("/catalog"))
        return route.fulfill({
          status: catalogFail ? 503 : 200,
          json: catalogFail
            ? { error: "目錄暫時無法下載" }
            : {
                source: "network",
                entries: [
                  { ...manifest, bytes: 10240 },
                  { ...engine, bytes: 1e7 },
                ],
              },
        });
      if (req.method() !== "GET") {
        const data = req.postDataJSON();
        mutations.push({ ep, data });
        if (ep === "extensions/install")
          state.state.installed["container-engine"] = {
            package: { manifest: engine },
            enabled: true,
            status: "active",
          };
        if (ep === "apps/install") {
          state.apps.apps["static-site"] = app;
          entries = [entry("static-site", "靜態網站示範")];
        }
        if (ep === "apps/action") {
          if (data.action === "stop")
            entries = [entry("static-site", "靜態網站示範", "stopped")];
          if (data.action === "start")
            entries = [entry("static-site", "靜態網站示範")];
          if (data.action === "remove") {
            entries = [];
            state.apps.apps = {};
            state.retained_data = [
              "tank/.just-nas-extensions/apps/static-site",
            ];
          }
        }
        const id = "task-" + mutations.length;
        jobs.push({
          id,
          kind: ep.includes("install") ? "store-install" : "store-action",
          resource: "tank|{}",
          state: "succeeded",
          progress: "已完成",
        });
        return route.fulfill({ json: { task_id: id } });
      }
      if (ep === "tasks") return route.fulfill({ json: jobs });
      if (ep.startsWith("apps/") && ep.endsWith("/logs"))
        return route.fulfill({ json: { logs: "static service running" } });
      if (ep === "extensions")
        return route.fulfill({
          json: { ...state, imports: state.engineImports || [] },
        });
      if (ep === "apps")
        return route.fulfill({
          json: {
            ...state,
            imports: [
              { token: "a".repeat(48), package: { manifest, official: true } },
            ],
          },
        });
      return route.fulfill({ json: fixtures[ep] || [] });
    });
    const url = "http://127.0.0.1:" + server.address().port;
    await page.goto(url + "/#apps");
    await page.locator(".app-main").waitFor();
    assert.equal(await page.locator("#nav-apps").count(), 1);
    assert.equal(await page.locator("#sec-apps").count(), 1);
    checks.push("single navigation owner and direct launcher route");
    entries = [
      entry("static-site", "靜態網站示範"),
      entry(
        "stopped",
        "很長的中文應用名稱用來確認兩行省略不會影響版面與其他圖標排列",
        "stopped",
      ),
      entry("broken", "媒體管理", "failed"),
      {
        ...entry("unknown", "下載服務", "unknown"),
        icon: "/api/apps/unknown/icon",
      },
      entry("restoring", "自動化服務", "restoring"),
    ];
    for (const theme of ["light", "dark"])
      for (const width of [1920, 1366, 768, 390]) {
        await page.setViewportSize({ width, height: 900 });
        await page.evaluate(
          (t) => (document.documentElement.dataset.theme = t),
          theme,
        );
        await page.evaluate(() => NAS.navigate("apps"));
        await page.waitForFunction(
          () => document.querySelectorAll(".app-main").length === 5,
        );
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth + 1,
          ),
          false,
          `${theme} ${width} launcher overflow`,
        );
        await page.screenshot({
          path: path.join(out, `launcher-${theme}-${width}.png`),
          fullPage: true,
        });
        await page
          .getByRole("button", { name: "應用商店", exact: true })
          .click();
        await page.locator(".catalog-card").waitFor();
        assert.equal(
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth + 1,
          ),
          false,
          `${theme} ${width} shop overflow`,
        );
        await page.screenshot({
          path: path.join(out, `store-${theme}-${width}.png`),
          fullPage: true,
        });
        checks.push(`${theme} ${width}px launcher and store no overflow`);
      }
    await page.setViewportSize({ width: 1366, height: 900 });
    await page.evaluate(() => NAS.navigate("apps"));
    await page.getByLabel("搜尋應用程式").fill("靜態");
    assert.equal(await page.locator(".app-tile:not([hidden])").count(), 2);
    await page.getByRole("button", { name: "應用商店", exact: true }).click();
    await page.goBack();
    assert.equal(await page.getByLabel("搜尋應用程式").inputValue(), "靜態");
    checks.push("browser Back retains search and app selection");
    await page.getByLabel("搜尋應用程式").fill("");
    await page.getByLabel("應用狀態篩選").selectOption("error");
    assert.equal(await page.locator(".app-main:visible").count(), 2);
    await page.getByLabel("應用狀態篩選").selectOption("all");
    const menu = page
      .locator(".app-tile")
      .filter({ has: page.locator(".app-name", { hasText: "靜態網站示範" }) })
      .locator("summary");
    await menu.click();
    await menu.focus();
    await page.evaluate(() => NAS.navigate("apps"));
    assert.equal(await menu.evaluate((n) => n.parentElement.open), true);
    assert.equal(
      await menu.evaluate((n) => n === document.activeElement),
      true,
    );
    await page.keyboard.press("Escape");
    checks.push(
      "background update preserves open menu and keyboard focus; Escape closes",
    );
    const popup = page.waitForEvent("popup");
    await page
      .getByRole("button", { name: "靜態網站示範，執行中", exact: true })
      .click();
    await (await popup).close();
    assert.equal(
      await page.locator('.app-badge[data-state="active"]').count(),
      1,
    );
    checks.push("closing external app page does not stop background marker");
    offline = true;
    await page.evaluate(() => NAS.navigate("apps"));
    await page.getByText("資訊已過期：連線中斷").waitFor();
    assert.equal(
      await page.locator(".app-status", { hasText: "資訊已過期" }).count(),
      5,
    );
    offline = false;
    await page.evaluate(() => NAS.navigate("apps"));
    await page
      .getByRole("button", { name: "靜態網站示範，執行中", exact: true })
      .waitFor();
    checks.push("failed polling marks every cached state stale");
    launcherDelay = 300;
    await page.evaluate(() => {
      for (let i = 0; i < 5; i++) NAS.navigate("apps");
    });
    await page.waitForTimeout(500);
    assert.equal(maxQueries, 1);
    launcherDelay = 0;
    checks.push("only one concurrent launcher request");
    await page.evaluate(() => NAS.navigate("dashboard"));
    const prior = queryCount;
    await page.waitForTimeout(5500);
    assert.equal(queryCount, prior);
    checks.push("polling paused outside launcher");
    await page.goto(url + "/#app-store/downloads");
    await page.getByRole("button", { name: "檢視與安裝" }).waitFor();
    assert.equal(
      await page.locator('[data-store-panel="downloads"]').isVisible(),
      true,
    );
    checks.push("shop deep link survives reload");
    state.state.installed = {};
    state.engineImports = [
      { token: "b".repeat(48), package: { manifest: engine, official: true } },
    ];
    catalogFail = true;
    await page.getByRole("button", { name: "檢視與安裝" }).click();
    await page
      .getByRole("heading", { name: "安裝／更新 靜態網站示範" })
      .waitFor();
    await page.getByLabel("網頁連接埠").fill("18081");
    await page.evaluate(() => window.refreshStores());
    assert.equal(await page.getByLabel("網頁連接埠").inputValue(), "18081");
    checks.push("background polling retains installation draft");
    const before = mutations.length;
    await page
      .getByRole("button", { name: "儲存並套用", exact: true })
      .last()
      .click();
    await page
      .getByText("請先勾選同意安裝系統相依套件，或取消安裝。", { exact: true })
      .waitFor();
    assert.equal(mutations.length, before);
    checks.push("dependencies require explicit consent before mutation");
    await page.getByLabel("我同意一併安裝／啟用以下系統相依套件").check();
    await page
      .getByRole("button", { name: "儲存並套用", exact: true })
      .last()
      .click();
    await page.waitForFunction(
      () => !document.querySelector(".modal-backdrop.active:not(#auth-modal)"),
    );
    assert.equal(mutations.at(-2).ep, "extensions/install");
    assert.equal(mutations.at(-1).ep, "apps/install");
    checks.push("offline dependencies install first, then application");
    await page.evaluate(() => NAS.navigate("apps"));
    await page.waitForFunction(
      () => document.querySelectorAll(".app-main").length === 1,
    );
    await page.getByLabel("靜態網站示範 的操作").click();
    await page.getByRole("button", { name: "停止", exact: true }).click();
    await page
      .getByRole("button", { name: "取消", exact: true })
      .last()
      .click();
    assert.equal(mutations.at(-1).ep, "apps/install");
    checks.push("cancel stop never changes service");
    entries = [];
    await page.evaluate(() => NAS.navigate("apps"));
    await page.waitForFunction(
      () => document.querySelectorAll(".app-main").length === 0,
    );
    assert.equal(await page.locator(".app-store-tile").count(), 1);
    checks.push("empty launcher always retains store icon");
    assert.deepEqual(errors, []);
    checks.push("no uncaught JavaScript errors");
    fs.writeFileSync(
      path.join(out, "browser-fixtures.json"),
      JSON.stringify(
        { type: "mock API browser acceptance; not VM acceptance", checks },
        null,
        2,
      ),
    );
    console.log("PASS", checks.length, "checks");
  } finally {
    await browser.close();
    server.close();
  }
}
main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
