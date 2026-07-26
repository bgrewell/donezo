import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      // donezod backend. Cookies pass through untouched (same-origin from
      // the browser's point of view), so no changeOrigin.
      "/api": {
        target: process.env.DONEZO_API_TARGET ?? "http://localhost:8787",
        changeOrigin: false,
      },
    },
    watch: {
      // The design system is a file: dependency; watch its source too so
      // token/component edits there hot-reload donezo.
      ignored: ["!**/node_modules/@grewelltech/console/**"],
    },
  },
});
