/// <reference types="vitest/config" />
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss(), wails("./bindings")],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "src"),
      "@bindings": path.resolve(import.meta.dirname, "bindings/github.com/magne4000/easy-pz-docker"),
    },
  },
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: { chunkSizeWarningLimit: 1024 },
  test: {
    environment: "jsdom",
  },
});
