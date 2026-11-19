import app from "./app";
import type { Bindings } from "./env";
import { runMaintenance } from "./maintenance";

export default {
  fetch: app.fetch,
  scheduled(_controller, env, ctx) {
    ctx.waitUntil(runMaintenance(env));
  },
} satisfies ExportedHandler<Bindings>;
