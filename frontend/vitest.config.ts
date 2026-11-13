import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vitest/config";

// Unit tests: components and helpers rendered to static markup in Node.
// Browser scenarios live in e2e/ and run with Playwright.
export default defineConfig({
  esbuild: { jsx: "automatic" },
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  test: {
    globals: true,
    environment: "node",
    include: ["tests/**/*.test.{cjs,ts,tsx}"],
  },
});
