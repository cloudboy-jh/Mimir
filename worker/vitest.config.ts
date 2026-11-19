import { fileURLToPath } from "node:url";
import { cloudflareTest } from "@cloudflare/vitest-pool-workers";
import { defineConfig } from "vitest/config";

const webSource = fileURLToPath(new URL("./web/src", import.meta.url));
export default defineConfig({
  resolve: { alias: { "@": webSource } },
  plugins: [
    cloudflareTest({
      wrangler: { configPath: "./wrangler.jsonc" },
      miniflare: { bindings: { OPENROUTER_API_KEY: "test-openrouter-key" } },
    }),
  ],
});
