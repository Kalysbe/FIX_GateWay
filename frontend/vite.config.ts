import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

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
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://localhost:8080", ws: true },
      "/health": "http://localhost:8080",
      "/ready": "http://localhost:8080",
      "/broker-swagger": "http://localhost:8080",
    },
  },
});
