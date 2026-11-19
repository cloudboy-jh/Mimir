import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

// `bun run dev` from the repository root starts `wrangler dev` on 8787; Vite
// proxies the dashboard API and Access handshake to it.
const worker = { target: "http://127.0.0.1:8787", changeOrigin: true };

export default defineConfig({
  root: fileURLToPath(new URL(".", import.meta.url)),
  plugins: [vue(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  base: "/",
  server: {
    port: 5173,
    strictPort: true,
    proxy: { "/dashboard/auth": worker, "/dashboard/api": worker },
  },
  build: { outDir: "dist", emptyOutDir: true },
});
