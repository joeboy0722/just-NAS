// Pure UI calculations run without a browser or NAS connection.
const assert = require("assert/strict");
const fs = require("fs");
const path = require("path");
const vm = require("vm");
const sandbox = { NAS: {}, window: {} };
vm.runInNewContext(
  fs.readFileSync(path.join(__dirname, "../web/js/maintenance.js"), "utf8"),
  sandbox,
);
for (const [input, expected] of [
  ["10486", 10486],
  ["100", 100],
  ["0", 0],
  ["1G", 1073741824],
  ["1.5M", 1572864],
  ["2t", 2199023255552],
]) {
  assert.equal(
    sandbox.NAS.quotaBytes(input),
    expected,
    "Quota conversion: " + input,
  );
}
console.log("PASS 6 quota unit regressions (including bare byte values)");
