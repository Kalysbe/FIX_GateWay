import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The gateway the dev server and `vite preview` forward API calls to; the local
// stand (deploy/docker-compose.local.yml) publishes it on 28080.
const gateway = process.env.GATEWAY_URL ?? "http://localhost:8080";
const proxy = {
  "/api": { target: gateway, ws: true },
  "/health": gateway,
  "/ready": gateway,
  "/broker-swagger": gateway,
};

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  build: {
    rollupOptions: {
      output: {
        // Libraries change less often than the pages; separate chunks stay cached across releases.
        manualChunks(id) {
          if (!id.includes("node_modules")) return;
          if (/[\\/]node_modules[\\/](react|react-dom|react-router|react-router-dom|scheduler)[\\/]/.test(id)) return "react";
          if (/[\\/]node_modules[\\/](radix-ui|@radix-ui|sonner|@floating-ui)[\\/]/.test(id)) return "ui";
          if (/[\\/]node_modules[\\/]@tanstack[\\/]/.test(id)) return "tanstack";
          if (/[\\/]node_modules[\\/](react-hook-form|@hookform|zod)[\\/]/.test(id)) return "form-libs";
        },
      },
    },
  },
  server: { port: 5173, proxy },
  preview: { proxy },
});
