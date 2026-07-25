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
    watch: {
      // The design system is a file: dependency; watch its source too so
      // token/component edits there hot-reload donezo.
      ignored: ["!**/node_modules/@grewelltech/aether/**"],
    },
  },
});
