/// <reference types="vitest/config" />
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

// 127.0.0.1, not localhost: Node resolves localhost to ::1 first on macOS.
const api = process.env.PZMAN_API ?? "http://127.0.0.1:8080";

// In dev, /mods/<token>/ must render the public entry (in prod Go serves it).
const publicModsPage = (): Plugin => ({
  name: "pzman-public-mods-page",
  configureServer(server) {
    server.middlewares.use((req, _res, next) => {
      if (req.url && /^\/mods\/[^/]+\/?(\?.*)?$/.test(req.url)) {
        req.url = "/mods.html";
      }
      next();
    });
  },
});

export default defineConfig({
  plugins: [react(), tailwindcss(), publicModsPage()],
  // Top-level input applies in dev and build alike: two entries, two bundles, so
  // unauthenticated visitors of the public mod page never receive the admin bundle.
  input: {
    app: path.resolve(import.meta.dirname, "index.html"),
    mods: path.resolve(import.meta.dirname, "mods.html"),
  },
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "src") },
    dedupe: ["react", "react-dom"],
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: api, changeOrigin: false },
      "^/mods/[^/]+/(data\\.json|download/.*)$": { target: api, changeOrigin: false },
      "/livez": api,
      "/readyz": api,
    },
  },
  build: {
    outDir: "../internal/webui/dist",
    emptyOutDir: true,
    rolldownOptions: {
      output: {
        minify: { compress: { dropConsole: true, dropDebugger: true } },
      },
    },
  },
  test: {
    environment: "jsdom",
  },
});
