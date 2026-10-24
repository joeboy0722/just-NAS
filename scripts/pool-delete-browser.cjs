const { chromium } = require("playwright");
const fs = require("fs");
const assert = require("assert/strict");
(async () => {
  const fixture = JSON.parse(
    fs.readFileSync("output/pool-delete-test/fixture.json", "utf8"),
  );
  const c = JSON.parse(
    fs.readFileSync("output/vm-test/test-credentials.json", "utf8"),
  );
  const browser = await chromium.launch({
    executablePath: process.env.NAS_CHROMIUM_PATH,
  });
  const page = await browser.newPage({
    viewport: { width: 1366, height: 900 },
  });
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  try {
    await page.goto("http://127.0.0.1:8770");
    await page.locator("#auth-password").fill(c.admin);
    await page.locator("#auth-submit").click();
    await page.waitForFunction(
      () => !document.querySelector("#auth-modal").classList.contains("active"),
    );
    await page.evaluate(() => NAS.navigate("storage"));
    await page.waitForTimeout(1000);
    async function openDeletion() {
      const row = page
        .locator("#tbody-pools tr")
        .filter({
          has: page.locator("td:first-child", {
            hasText: new RegExp("^" + fixture.pool + "$"),
          }),
        });
      await row.locator("summary").click();
      await row.getByRole("button", { name: "銷毀", exact: true }).click();
      await page.locator(".modal-backdrop.active").waitFor();
    }
    await openDeletion();
    const message = await page.locator(".modal-backdrop.active").innerText();
    assert(message.includes("2 個分享"));
    assert(message.includes("6 個排程"));
    await page.screenshot({
      path: "output/pool-delete-test/deletion-preview.png",
      fullPage: true,
    });
    await page.locator(".modal-backdrop.active [data-cancel]").click();
    assert(
      (await page
        .locator("#tbody-pools tr")
        .filter({ hasText: fixture.pool })
        .count()) === 1,
    );
    console.log("PASS Browser shows cascade impact; cancel preserves pool");
    await openDeletion();
    await page.locator('[id^="field-confirm-"]').fill(fixture.pool);
    await page.locator(".modal-backdrop.active button.btn-primary").click();
    await page
      .getByText("儲存池與關聯設定已同步刪除", { exact: true })
      .waitFor({ timeout: 110000 });
    await page.waitForTimeout(1200);
    assert.equal(
      await page
        .locator("#tbody-pools tr")
        .filter({ hasText: fixture.pool })
        .count(),
      0,
    );
    assert.equal(errors.length, 0, errors.join(";"));
    await page.screenshot({
      path: "output/pool-delete-test/deletion-complete.png",
      fullPage: true,
    });
    console.log(
      "PASS Browser confirms and executes cascade deletion without JavaScript errors",
    );
  } finally {
    await browser.close();
  }
})().catch((e) => {
  console.error(e);
  process.exitCode = 1;
});
