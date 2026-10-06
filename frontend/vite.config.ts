import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://localhost:8080", ws: true },
      "/health": "http://localhost:8080",
      "/ready": "http://localhost:8080",
      "/swagger": "http://localhost:8080",
      "/broker-swagger": "http://localhost:8080",
    },
  },
});
