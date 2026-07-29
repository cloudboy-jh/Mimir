import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

export default defineConfig({
  root: fileURLToPath(new URL(".", import.meta.url)),
  plugins: [vue(), tailwindcss()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  base: "/",
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/dashboard/auth": { target: "http://127.0.0.1:8787", changeOrigin: true },
      "/dashboard/api": { target: "http://127.0.0.1:8787", changeOrigin: true },
      "/dashboard/log-objects": { target: "http://127.0.0.1:8787", changeOrigin: true },
    },
  },
  build: { outDir: "dist", emptyOutDir: true },
});
