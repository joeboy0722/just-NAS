/* Real VM assets, certificate-pinned loopback proxy; run only after snapshot backup. */
const { chromium } = require("playwright"),
  fs = require("fs"),
  path = require("path"),
  assert = require("assert/strict");
const root = path.resolve(__dirname, ".."),
  out = path.join(root, "output/launcher-review"),
  base = "http://127.0.0.1:8765";
async function main() {
  const browser = await chromium.launch({
    executablePath: "C:/Program Files/Google/Chrome/Application/chrome.exe",
  });
  const resume = process.argv.includes("--resume");
  const checks = resume
    ? JSON.parse(fs.readFileSync(path.join(out, "live-browser.json"), "utf8"))
        .checks
    : [];
  const pass = (t) => {
    checks.push(t);
    fs.writeFileSync(
      path.join(out, "live-browser.json"),
      JSON.stringify({ checks }, null, 2),
    );
    console.log("PASS", t);
  };
  try {
    const context = await browser.newContext({
        viewport: { width: 1366, height: 900 },
      }),
      page = await context.newPage(),
      errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.goto(base + "/#apps");
    await page
      .locator("#auth-password")
      .fill(
        JSON.parse(
          fs.readFileSync(
            path.join(root, "output/vm-test/test-credentials.json"),
            "utf8",
          ),
        ).admin,
      );
    await page.locator("#auth-submit").click();
    await page.locator("#auth-modal").waitFor({ state: "hidden" });
    const status = await (
        await context.request.get(base + "/api/auth/status")
      ).json(),
      headers = { "X-CSRF-Token": status.csrf };
    async function api(p, m = "GET", data) {
      const r = await context.request.fetch(base + "/api/" + p, {
        method: m,
        headers,
        data,
        timeout: 180000,
      });
      assert(r.ok(), p + ": " + (await r.text()).slice(0, 1000));
      return r.json();
    }
    async function wait(id) {
      for (let n = 0; n < 240; n++) {
        const j = (await api("tasks")).find((v) => v.id === id);
        if (!["running", "queued"].includes(j.state)) {
          assert.equal(j.state, "succeeded", j.error);
          return;
        }
        await page.waitForTimeout(1000);
      }
      throw Error("task timeout");
    }
    async function upload(file, kind = "apps") {
      const r = await context.request.post(base + "/api/" + kind + "/import", {
        headers: { ...headers, "Content-Type": "application/octet-stream" },
        data: fs.readFileSync(path.join(root, file)),
        timeout: 240000,
      });
      assert(r.ok(), (await r.text()).slice(0, 1000));
      return r.json();
    }
    async function installed() {
      for (let i = 0; i < 180; i++) {
        const a = (await api("apps/launcher")).apps.find(
          (a) => a.id === "static-site",
        );
        if (a?.status === "active") return a;
        await page.waitForTimeout(1000);
      }
      throw Error("Application did not become active");
    }
    let a;
    if (!resume) {
      const initial = await api("apps");
      assert.equal(initial.state.dataset, "homepool/.just-nas-extensions");
      assert.equal(Object.keys(initial.apps.apps).length, 0);
      await page
        .getByRole("button", { name: "應用商店", exact: true })
        .waitFor();
      pass("Empty real VM launcher retains store icon");
      await upload(
        "output/store-packages/container-engine-1.0.0.jnpkg",
        "extensions",
      );
      await upload("output/store-packages/static-site-1.0.0.jnpkg");
      await page.evaluate(() => NAS.navigate("app-store/downloads"));
      await page.getByRole("button", { name: "檢視與安裝" }).click();
      await page
        .getByRole("heading", { name: "安裝／更新 靜態網站示範" })
        .waitFor();
      await page
        .getByLabel("NAS 區網位址", { exact: true })
        .selectOption("192.168.56.101");
      await page.getByLabel("網頁連接埠", { exact: true }).fill("18080");
      await page.screenshot({
        path: path.join(out, "live-dependency-consent.png"),
        fullPage: true,
      });
      const oldTasks = (await api("tasks")).length;
      await page
        .getByRole("button", { name: "儲存並套用", exact: true })
        .last()
        .click();
      await page
        .getByText("請先勾選同意安裝系統相依套件，或取消安裝。", {
          exact: true,
        })
        .waitFor();
      assert.equal((await api("tasks")).length, oldTasks);
      assert(!(await api("extensions")).state.installed["container-engine"]);
      pass("Unchecked dependency consent causes no installation or task");
      await page.getByLabel("我同意一併安裝／啟用以下系統相依套件").check();
      await page
        .getByRole("button", { name: "儲存並套用", exact: true })
        .last()
        .click();
      await page.waitForFunction(
        () =>
          !document.querySelector(".modal-backdrop.active:not(#auth-modal)"),
        null,
        { timeout: 240000 },
      );
      a = await installed();
      assert.equal(
        (await api("extensions")).state.installed["container-engine"].status,
        "active",
      );
      pass(
        "Actual offline dependency engine installs before application and becomes healthy",
      );
      await page.evaluate(() => NAS.navigate("apps"));
      await page
        .getByRole("button", { name: "靜態網站示範，執行中", exact: true })
        .waitFor();
      const popup = page.waitForEvent("popup");
      await page
        .getByRole("button", { name: "靜態網站示範，執行中", exact: true })
        .click();
      const site = await popup;
      await site.waitForLoadState();
      assert.equal((await context.request.get(a.url)).status(), 200);
      await site.close();
      await page.waitForTimeout(5500);
      assert.equal(
        await page.locator('.app-badge[data-state="active"]').count(),
        1,
      );
      pass(
        "Real application opens and closing its page retains running marker",
      );
    }
    await page.evaluate(() => NAS.navigate("apps"));
    await page
      .getByRole("button", { name: "靜態網站示範，執行中", exact: true })
      .waitFor();
    await page.getByLabel("靜態網站示範 的操作").click();
    await page.getByRole("button", { name: "停止", exact: true }).click();
    await page
      .getByRole("button", { name: "確認", exact: true })
      .last()
      .click();
    await page
      .getByRole("button", { name: "靜態網站示範，已停止", exact: true })
      .waitFor({ timeout: 30000 });
    await page
      .getByRole("button", { name: "靜態網站示範，已停止", exact: true })
      .click();
    await page
      .getByRole("button", { name: "啟動", exact: true })
      .last()
      .click();
    await installed();
    pass("Stop, stopped details and start follow actual container state");
    const icon = await upload(
      "output/launcher-fixture/static-site-icon-1.1.0.jnpkg",
    );
    await wait(
      (
        await api("apps/install", "POST", {
          token: icon.token,
          port: 18080,
          bind_ip: "192.168.56.101",
          read_only: true,
        })
      ).task_id,
    );
    a = await installed();
    assert.equal(a.version, "1.1.0");
    const iconResponse = await context.request.get(base + a.icon);
    assert.equal(iconResponse.status(), 200);
    assert.equal(iconResponse.headers()["content-type"], "image/png");
    await page.evaluate(() => NAS.navigate("apps"));
    await page.waitForFunction(
      () => document.querySelector(".app-tile img")?.naturalWidth === 128,
    );
    pass(
      "Signed icon package updates existing app and serves bounded authenticated PNG",
    );
    for (const theme of ["light", "dark"])
      for (const width of [1920, 1366, 768, 390]) {
        await page.setViewportSize({ width, height: 900 });
        await page.evaluate(
          (t) => (document.documentElement.dataset.theme = t),
          theme,
        );
        for (const route of ["apps", "app-store/explore"]) {
          await page.evaluate((r) => NAS.navigate(r), route);
          await page.waitForTimeout(350);
          assert.equal(
            await page.evaluate(
              () => document.documentElement.scrollWidth > innerWidth + 1,
            ),
            false,
          );
          await page.screenshot({
            path: path.join(
              out,
              `live-${route.startsWith("app-store") ? "store" : "launcher"}-${theme}-${width}.png`,
            ),
            fullPage: true,
          });
        }
      }
    pass(
      "VM embedded launcher and store: both themes at 1920, 1366, 768 and 390 without overflow",
    );
    await wait(
      (
        await api("apps/action", "POST", {
          id: "static-site",
          action: "remove",
          delete_data: false,
        })
      ).task_id,
    );
    assert.equal((await api("apps/launcher")).apps.length, 0);
    assert(
      (await api("apps")).retained_data.some((x) =>
        x.endsWith("/apps/static-site"),
      ),
    );
    await page.evaluate(() => NAS.navigate("apps"));
    await page.waitForFunction(
      () => document.querySelectorAll(".app-main").length === 0,
    );
    pass(
      "Uninstall removes icon while retained application dataset remains visible in store",
    );
    const again = await upload(
      "output/launcher-fixture/static-site-icon-1.1.0.jnpkg",
    );
    await wait(
      (
        await api("apps/install", "POST", {
          token: again.token,
          port: 18080,
          bind_ip: "192.168.56.101",
          read_only: true,
          reuse_data: true,
        })
      ).task_id,
    );
    await installed();
    pass("Explicit retained-data reuse reinstalls application successfully");
    assert.deepEqual(errors, []);
    pass("No uncaught JavaScript errors using VM assets");
  } finally {
    await browser.close();
  }
}
main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
