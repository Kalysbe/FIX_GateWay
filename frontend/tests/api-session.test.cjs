const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");
const ts = require("typescript");

// Run the browser API module against controlled storage, timers and responses.
const source = readFileSync(resolve(__dirname, "../src/api.ts"), "utf8")
  .replace("import.meta.env.VITE_API_URL", '""');
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

function setup() {
  const values = new Map();
  const storage = {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
  let respond;
  let timer;
  const redirects = [];
  const context = {
    exports: {}, sessionStorage: storage, localStorage: storage,
    Headers, Blob, TextDecoder, atob,
    fetch: () => new Promise((done) => { respond = done; }),
    window: {
      location: { pathname: "/orders", assign: (path) => redirects.push(path) },
      setTimeout: (callback) => { timer = callback; return 1; },
      clearTimeout: () => {},
    },
  };
  vm.runInNewContext(compiled, context);
  return {
    api: context.exports, redirects,
    unauthorized: () => respond(new Response('{"error":"unauthorized"}', { status: 401 })),
    expire: () => timer(),
  };
}

for (const operation of ["api", "apiDownload", "consumeAdminEvents"]) {
  for (const replaced of [false, true]) {
    test(`${operation}: 401 ${replaced ? "preserves the new session" : "expires the current session"}`, async () => {
      const env = setup();
      env.api.setToken("old-token");
      const pending = operation === "consumeAdminEvents"
        ? env.api.consumeAdminEvents(new AbortController().signal, () => {}, () => {})
        : env.api[operation]("/api/v1/admin/orders");
      if (replaced) env.api.setToken("new-token");
      env.unauthorized();
      await assert.rejects(pending);
      assert.equal(env.api.getToken(), replaced ? "new-token" : "");
      assert.deepEqual(env.redirects, replaced ? [] : ["/login"]);
    });
  }
}

test("old expiry timer preserves a replacement session", () => {
  const env = setup();
  const payload = Buffer.from(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 60 })).toString("base64url");
  env.api.setToken(`header.${payload}.signature`);
  env.api.watchSessionExpiry();
  env.api.setToken("new-token");
  env.expire();
  assert.equal(env.api.getToken(), "new-token");
  assert.deepEqual(env.redirects, []);
});
