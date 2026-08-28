import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

export default defineConfig(({ mode }) => {
  const demo = mode === "demo";
  const fixtures = demo || (mode === "development" && process.env.VITE_MIMIR_DATA_SOURCE === "fixtures");
  return {
    root: fileURLToPath(new URL(".", import.meta.url)),
    plugins: [vue(), tailwindcss()],
    resolve: {
      alias: [
        {
          find: "@/lib/fixture-provider",
          replacement: fileURLToPath(
            new URL(fixtures ? "./src/lib/dev-fixtures.ts" : "./src/lib/fixture-provider.ts", import.meta.url),
          ),
        },
        { find: "@", replacement: fileURLToPath(new URL("./src", import.meta.url)) },
      ],
    },
    base: "/",
    define: { __MIMIR_FIXTURES__: JSON.stringify(fixtures) },
    server: {
      port: 5173,
      strictPort: true,
      proxy: {
        "/dashboard/auth": { target: "http://127.0.0.1:8787", changeOrigin: true },
        "/dashboard/api": { target: "http://127.0.0.1:8787", changeOrigin: true, ws: true },
        "/dashboard/log-objects": { target: "http://127.0.0.1:8787", changeOrigin: true },
      },
    },
    build: {
      outDir: demo ? fileURLToPath(new URL("../../internal/demoassets/static", import.meta.url)) : "dist",
      emptyOutDir: true,
    },
  };
});
