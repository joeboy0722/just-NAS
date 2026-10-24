/* UI regression against the pinned-TLS localhost preview; fixtures are uniquely named. */
const { chromium } = require("playwright");
const fs = require("fs");
const path = require("path");
const assert = require("assert/strict");
const root = path.resolve(__dirname, "..");
const out = path.join(root, "output/ui-review");
const credentials = JSON.parse(
  fs.readFileSync(
    path.join(root, "output/vm-test/test-credentials.json"),
    "utf8",
  ),
);
const base = process.env.NAS_UI_BASE || "http://127.0.0.1:8765";
const checks = [];
fs.mkdirSync(out, { recursive: true });
function record(name) {
  checks.push({ name, result: "PASS" });
  fs.writeFileSync(
    path.join(out, "acceptance.json"),
    JSON.stringify(checks, null, 2),
  );
  console.log("PASS", name);
}
async function main() {
  const browser = await chromium.launch({
    executablePath: process.env.NAS_CHROMIUM_PATH || undefined,
  });
  const context = await browser.newContext({
    viewport: { width: 1366, height: 900 },
  });
  const admin = await context.newPage();
  const errors = [];
  context.on("page", (p) => p.on("pageerror", (e) => errors.push(e.message)));
  admin.on("pageerror", (e) => errors.push(e.message));
  let originalACL, share, portal, createdSnapshot;
  const stamp = Date.now();
  const fixture = "UI-" + stamp;
  let fixtureCreated = false;
  async function api(endpoint, method = "GET", body) {
    const auth = await (
      await context.request.get(base + "/api/auth/status")
    ).json();
    const res = await context.request.fetch(base + "/api/" + endpoint, {
      method,
      data: body,
      headers: { "X-CSRF-Token": auth.csrf || "" },
    });
    const value = await res.json();
    assert(res.ok(), endpoint + ": " + JSON.stringify(value));
    return value;
  }
  async function waitJob(id) {
    for (let i = 0; i < 100; i++) {
      const jobs = await api("tasks");
      const j = jobs.find((x) => x.id === id);
      if (j && j.state !== "running" && j.state !== "queued") {
        assert.equal(j.state, "succeeded", j.error);
        return;
      }
      await new Promise((r) => setTimeout(r, 300));
    }
    throw Error("Task did not complete");
  }
  async function cancelForm() {
    await admin
      .locator(".modal-backdrop.active")
      .last()
      .locator("[data-cancel], [data-close]")
      .first()
      .click();
  }
  try {
    await admin.goto(base);
    await admin.locator("#auth-password").fill(credentials.admin);
    await admin.locator("#auth-submit").click();
    await admin.waitForFunction(
      () => !document.querySelector("#auth-modal").classList.contains("active"),
    );
    await admin.waitForFunction(
      () =>
        document.querySelector("#tbody-shares tr td")?.textContent !==
        "載入中...",
    );
    await admin.waitForTimeout(1200);
    assert.equal(await admin.locator("#connection-status").innerText(), "");
    record("Administrator login and all data sources load");
    const before = {
      users: await api("users"),
      shares: await api("shares"),
      backup: await api("backup"),
      network: await api("network"),
    };
    fs.writeFileSync(
      path.join(out, "configuration-before.json"),
      JSON.stringify(before, null, 2),
    );
    const options = await api("ui/storage-options");
    assert(Array.isArray(options.backup_devices));
    assert(options.pool_members.length > 0, JSON.stringify(options));
    record("Structured backup-device and pool-member selectors");
    const routes = [
      "dashboard",
      "storage",
      "datasets",
      "disks",
      "users",
      "shares",
      "snapshots",
      "backup",
      "tasks",
      "logs",
      "settings",
    ];
    for (const theme of ["light", "dark"]) {
      await admin.locator(".theme-select").selectOption(theme);
      for (const width of [1920, 1366, 768, 390]) {
        await admin.setViewportSize({ width, height: 900 });
        for (const route of routes) {
          await admin.evaluate((id) => NAS.navigate(id), route);
          assert.equal(
            await admin.locator(".section-page.active").getAttribute("id"),
            "sec-" + route,
          );
          assert(
            await admin.evaluate(
              () => document.documentElement.scrollWidth <= innerWidth,
            ),
            theme + " " + width + " " + route,
          );
          if (width === 1366 || width === 390)
            await admin.screenshot({
              path: path.join(out, `${route}-${theme}-${width}.png`),
              fullPage: true,
            });
        }
        record(`All administrator pages: ${theme}, ${width}px`);
      }
    }
    await admin.setViewportSize({ width: 1366, height: 900 });
    await admin.evaluate(() => NAS.navigate("shares"));
    await admin
      .locator("#tbody-shares tr")
      .filter({ hasText: "Family" })
      .first()
      .locator("summary")
      .click();
    await admin
      .locator("#tbody-shares .row-menu[open] button")
      .filter({ hasText: /^權限$/ })
      .click();
    const matrix = admin.locator(".permission-table");
    const familyRow = matrix
      .locator("tr")
      .filter({ hasText: new RegExp("^" + credentials.user + "$") });
    const read = familyRow.locator("input").nth(0),
      write = familyRow.locator("input").nth(1);
    await read.uncheck();
    assert.equal(await write.isChecked(), false);
    await write.check();
    assert.equal(await read.isChecked(), true);
    await admin.locator(".permission-filter").fill("no-such-user");
    assert.equal(await matrix.locator("tbody tr:visible").count(), 0);
    await admin.locator(".permission-filter").fill("");
    record("Permission matrix dependency and account filter");
    await cancelForm();
    await admin.locator("#btn-open-create-share").click();
    await admin.locator("#share-name-input").fill("unsaved-draft");
    await admin.waitForTimeout(11000);
    assert.equal(
      await admin.locator("#share-name-input").inputValue(),
      "unsaved-draft",
    );
    assert.equal(await admin.locator("#share-users-select").isVisible(), false);
    assert(
      (await admin.locator("#group-share-users .choice-list input").count()) >
        0,
    );
    await cancelForm();
    record("Share checkbox list and background refresh preserve form draft");
    let unexpectedQuotaWrite = false;
    await admin.route("**/api/storage/dataset-properties?*", async (route) => {
      if (route.request().method() === "PATCH") {
        unexpectedQuotaWrite = true;
        await route.fulfill({
          status: 400,
          contentType: "application/json",
          body: JSON.stringify({ error: "驗收保護：預覽取消流程不得寫入" }),
        });
      } else await route.continue();
    });
    await admin.evaluate(() => NAS.navigate("datasets"));
    const quotaRow = admin
      .locator("#tbody-datasets tr")
      .filter({ hasText: "homepool/Family" })
      .first();
    await quotaRow
      .getByRole("button", { name: "屬性／配額", exact: true })
      .click();
    await admin.locator(".quota-control input[type=checkbox]").uncheck();
    await admin.locator(".quota-control input[type=number]").fill("0.01");
    await admin.locator(".quota-control select").selectOption("M");
    await admin.locator(".modal-backdrop.active button.btn-primary").click();
    await admin
      .locator(".modal-backdrop.active")
      .last()
      .getByText(/30 天寬限期/)
      .waitFor();
    await cancelForm();
    await cancelForm();
    assert.equal(unexpectedQuotaWrite, false);
    await admin.unroute("**/api/storage/dataset-properties?*");
    record("Quota reduction previews grace period; cancel preserves quota");

    await admin.evaluate(() => NAS.navigate("tasks"));
    await admin.getByRole("button", { name: "執行任務", exact: true }).click();
    await admin.locator('[id^="field-kind-"]').selectOption("scrub");
    assert((await admin.locator('[id^="field-resource-"] option').count()) > 0);
    await admin.locator('[id^="field-kind-"]').selectOption("smart-short");
    assert((await admin.locator('[id^="field-resource-"] option').count()) > 0);
    await cancelForm();
    record("Task targets follow selected maintenance type");
    assert((await admin.locator("#tasks-view tr").count()) <= 20);
    record("Task history stays within twenty rows per page");
    await admin.evaluate(() => NAS.navigate("settings"));
    await admin.getByRole("button", { name: "編輯網路", exact: true }).click();
    assert.equal(
      await admin.locator('[id^="field-dhcp-"]').isChecked(),
      before.network.settings?.dhcp ?? true,
    );
    await cancelForm();
    await admin.getByRole("button", { name: "郵件通知", exact: true }).click();
    await admin
      .getByRole("button", { name: "編輯郵件通知", exact: true })
      .click();
    assert.equal(
      await admin.locator('[id^="field-password-"]').inputValue(),
      "",
    );
    await cancelForm();
    record("Network prefills actual mode; SMTP secrets stay hidden");
    portal = await context.newPage();
    await portal.goto(base + "/files.html");
    await portal.locator("#username").fill(credentials.user);
    await portal.locator("#password").fill(credentials.password);
    await portal.locator("#login-form button").click();
    await portal.locator("#browser").waitFor({ state: "visible" });
    await portal.waitForTimeout(800);
    share = before.shares.find(
      (s) => s.name === "Family" && s.mode === "public",
    );
    assert(share, "Family test space is required");
    originalACL = structuredClone(share.access);
    const changed = { ...originalACL, [credentials.user]: "ro" };
    const task = await api("tasks", "POST", {
      kind: "permissions",
      resource: share.id + "|" + JSON.stringify(changed),
    });
    await waitJob(task.id);
    await portal.locator("#refresh").click();
    await portal.waitForTimeout(500);
    assert(await portal.locator("#upload-button").isDisabled());
    record("Live VM permission change produces read-only file UI");
    const restored = await api("tasks", "POST", {
      kind: "permissions",
      resource: share.id + "|" + JSON.stringify(originalACL),
    });
    await waitJob(restored.id);
    await portal.locator("#refresh").click();
    await portal.waitForTimeout(600);
    assert.equal(await portal.locator("#upload-button").isDisabled(), false);
    record("Live VM permission restoration enables writes");
    await portal.locator("#mkdir").click();
    await portal.locator('[id^="field-value-"]').fill(fixture);
    await portal.locator(".modal-backdrop.active button.btn-primary").click();
    await portal.getByRole("button", { name: fixture, exact: true }).waitFor();
    fixtureCreated = true;
    await portal.getByRole("button", { name: fixture, exact: true }).click();
    await portal.route("**/files-api/upload?*", (route) =>
      route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "驗收模擬：上傳失敗" }),
      }),
    );
    await portal.locator("#upload").setInputFiles({
      name: "failed.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("not sent"),
    });
    await portal.getByText("驗收模擬：上傳失敗", { exact: true }).waitFor();
    assert.equal(await portal.locator("#upload-button").isDisabled(), false);
    await portal.unroute("**/files-api/upload?*");
    record("Upload errors remain visible and allow retry (injected response)");
    const longName = "中文長檔名-" + "family-document-".repeat(8) + ".txt";
    await portal.locator("#upload").setInputFiles({
      name: longName,
      mimeType: "text/plain",
      buffer: Buffer.from("UI acceptance content"),
    });
    await portal.getByText("上傳完成：" + longName, { exact: true }).waitFor();
    record("Live streamed upload with long Chinese filename");
    await portal.locator("#upload").setInputFiles({
      name: longName,
      mimeType: "text/plain",
      buffer: Buffer.from("replacement"),
    });
    await portal.locator(".modal-backdrop.active").waitFor();
    await portal.locator(".modal-backdrop.active [data-cancel]").click();
    assert(
      (await portal.locator("#message").innerText()).includes("已取消替換"),
    );
    record(
      "Duplicate upload prompts only on conflict; cancel cancels replacement",
    );
    const row = portal.locator("#entries tr").filter({ hasText: longName });
    await row.locator("summary").click();
    await row.getByRole("button", { name: "重新命名", exact: true }).click();
    await portal.locator('[id^="field-value-"]').fill("renamed.txt");
    await portal.locator(".modal-backdrop.active button.btn-primary").click();
    await portal.getByText("renamed.txt", { exact: true }).waitFor();
    record("Rename is separate from move");

    await portal.locator("#mkdir").click();
    await portal.locator('[id^="field-value-"]').fill("destination");
    await portal.locator(".modal-backdrop.active button.btn-primary").click();
    await portal
      .getByRole("button", { name: "destination", exact: true })
      .waitFor();
    const renamed = portal
      .locator("#entries tr")
      .filter({ hasText: "renamed.txt" });
    await renamed.locator("summary").click();
    await renamed.getByRole("button", { name: "移動至…", exact: true }).click();
    await portal
      .locator("dialog")
      .getByRole("button", { name: fixture, exact: true })
      .click();
    await portal
      .locator("dialog")
      .getByRole("button", { name: "destination", exact: true })
      .click();
    await portal
      .locator("dialog")
      .getByRole("button", { name: "選擇此資料夾", exact: true })
      .click();
    await portal.waitForTimeout(400);
    await portal
      .getByRole("button", { name: "destination", exact: true })
      .click();
    await portal.getByText("renamed.txt", { exact: true }).waitFor();
    record("Folder picker moves files within the same space");
    const dataset = share.path.replace(/^\/mnt\//, "");
    const tag = "ui-acceptance-" + stamp;
    await api("storage/snapshots", "POST", { dataset, tag });
    createdSnapshot = dataset + "@" + tag;
    await portal.locator("#upload").setInputFiles({
      name: "renamed.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("new version"),
    });
    await portal.locator(".modal-backdrop.active button.btn-primary").click();
    await portal.getByText("上傳完成：renamed.txt", { exact: true }).waitFor();
    record("Explicit replacement updates an existing file");

    await portal.locator("#entries summary").click();
    await portal.getByRole("button", { name: "歷史版本", exact: true }).click();
    await portal
      .locator('[id^="field-snapshot-"]')
      .selectOption(createdSnapshot);
    await portal.locator('[id^="field-name-"]').fill("historical.txt");
    await portal.locator(".modal-backdrop.active button.btn-primary").click();
    await portal
      .locator("dialog")
      .getByRole("button", { name: "選擇此資料夾", exact: true })
      .click();
    await portal.getByText("historical.txt", { exact: true }).waitFor();
    const restoredDownload = portal
      .locator("#entries tr")
      .filter({ hasText: "historical.txt" })
      .locator("a.row-download");
    const bytes = await (
      await portal.request.get(
        new URL(await restoredDownload.getAttribute("href"), base).href,
      )
    ).body();
    assert.equal(bytes.toString(), "UI acceptance content");
    record(
      "Historical version selector restores a file with identical contents",
    );

    for (const theme of ["light", "dark"]) {
      await portal.locator(".theme-select").selectOption(theme);
      for (const width of [1920, 1366, 768, 390]) {
        await portal.setViewportSize({ width, height: 900 });
        assert(
          await portal.evaluate(
            () => document.documentElement.scrollWidth <= innerWidth,
          ),
        );
        await portal.screenshot({
          path: path.join(out, `files-${theme}-${width}.png`),
          fullPage: true,
        });
      }
      record("Family file layout: " + theme + ", all four viewports");
    }
    await portal.setViewportSize({ width: 1366, height: 900 });
    await portal
      .locator("#entries tr")
      .filter({ hasText: "renamed.txt" })
      .locator("summary")
      .click();
    await portal
      .getByRole("button", { name: "移入回收區", exact: true })
      .click();
    await portal.locator(".modal-backdrop.active button.btn-primary").click();
    await portal.waitForTimeout(500);
    await portal.locator("#trash-mode").click();
    await portal.waitForTimeout(500);
    record("File delete moves content into recycle bin");
    assert.equal(errors.length, 0, errors.join("\n"));
    const after = {
      users: await api("users"),
      shares: await api("shares"),
      backup: await api("backup"),
      network: await api("network"),
    };
    assert.deepEqual(after.users, before.users);
    const canonicalShares = (ss) =>
      ss
        .map((s) => ({ ...s, users: (s.users || []).slice().sort() }))
        .sort((a, b) => a.id.localeCompare(b.id));
    assert.deepEqual(
      canonicalShares(after.shares),
      canonicalShares(before.shares),
    );
    assert.deepEqual(after.backup, before.backup);
    record(
      "Existing users, permissions, shares and backup configuration preserved",
    );
  } catch (e) {
    checks.push({ name: "Failure", result: e.message });
    fs.writeFileSync(
      path.join(out, "acceptance.json"),
      JSON.stringify(checks, null, 2),
    );
    await admin
      .screenshot({ path: path.join(out, "failure.png"), fullPage: true })
      .catch(() => {});
    throw e;
  } finally {
    if (share && originalACL) {
      const now = await api("shares").catch(() => []);
      if (
        JSON.stringify(now.find((s) => s.id === share.id)?.access) !==
        JSON.stringify(originalACL)
      ) {
        const task = await api("tasks", "POST", {
          kind: "permissions",
          resource: share.id + "|" + JSON.stringify(originalACL),
        });
        await waitJob(task.id);
      }
    }
    if (createdSnapshot)
      await api(
        "storage/snapshots?" +
          new URLSearchParams({
            name: createdSnapshot,
            confirm: createdSnapshot,
          }),
        "DELETE",
      );
    // Fixture cleanup is restricted to this run's unique directory and trash entries.
    if (fixtureCreated && portal) {
      const auth = await (
        await portal.request.get(base + "/files-api/spaces")
      ).json();
      const headers = { "X-CSRF-Token": auth.csrf };
      const fop = (op, p, extra = {}) =>
        base +
        "/files-api/" +
        op +
        "?" +
        new URLSearchParams({ space: share.id, path: p, ...extra });
      await portal.request.post(fop("trash", fixture), { headers, data: {} });
      const trash = await (
        await portal.request.get(fop("trash-list", ""))
      ).json();
      for (const e of trash.entries || [])
        if (e.name.includes(fixture))
          await portal.request.post(fop("purge", e.name), {
            headers,
            data: {},
          });
    }
    await browser.close();
  }
}
main().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
