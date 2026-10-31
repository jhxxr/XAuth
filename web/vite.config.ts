import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

// The Go binary embeds this directory (internal/webui).
const outDir = fileURLToPath(new URL("../internal/webui/dist", import.meta.url));

// emptyOutDir removes the placeholder that keeps the directory in git.
const keepPlaceholder: Plugin = {
  name: "xauth-keep-placeholder",
  closeBundle() {
    writeFileSync(`${outDir}/.gitkeep`, "");
  },
};

// `npm run dev` serves the UI on :5173 and forwards everything else to a
// local `XAUTH_DEV=true xauth` on :8080.
const backend = "http://127.0.0.1:8080";
const proxied = ["/api", "/login/start", "/authorize", "/oauth", "/userinfo", "/keys", "/end_session", "/revoke", "/.well-known"];

export default defineConfig({
  plugins: [react(), tailwindcss(), keepPlaceholder],
  build: { outDir, emptyOutDir: true, sourcemap: false },
  server: {
    port: 5173,
    proxy: Object.fromEntries(proxied.map((p) => [p, { target: backend, changeOrigin: false }])),
  },
});
