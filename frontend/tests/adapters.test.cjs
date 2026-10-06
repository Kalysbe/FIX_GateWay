const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");
const ts = require("typescript");

const source = readFileSync(resolve(__dirname, "../src/lib/adapters.ts"), "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const context = { exports: {} };
vm.runInNewContext(compiled, context);
const { isHiddenAdapter, normalizeAdapter, isSimulator } = context.exports;

test("operational cards follow enabled flags across simulator, primary and standby adapters", () => {
  for (const adapter of [
    { id: "simulator-1", type: "simulator", enabled: true },
    { id: "quik-fix-1b", name: "Secondary", enabled: true },
    { id: "quik-fix-1", enabled: true },
  ]) {
    assert.equal(isHiddenAdapter(adapter), false);
    assert.equal(isHiddenAdapter({ ...adapter, enabled: false }), true);
  }
});

test("disabled flags normalize from both API property styles", () => {
  assert.equal(normalizeAdapter({ enabled: false }).enabled, false);
  assert.equal(normalizeAdapter({ Enabled: false }).enabled, false);
  assert.equal(normalizeAdapter({}).enabled, true);
});

test("simulator detection supports the test environment route label", () => {
  assert.equal(isSimulator({ type: "simulator" }), true);
  assert.equal(isSimulator({ id: "simulator-2" }), true);
  assert.equal(isSimulator({ type: "arqa", id: "quik-fix-1" }), false);
});
