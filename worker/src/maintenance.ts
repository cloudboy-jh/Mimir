import type { Bindings } from "./env";
import { backfillSearchIndex } from "./search/search-index";
import { expireSessions } from "./sessions/lifecycle";
import { buildPendingSummaries } from "./sessions/summaries";

// runMaintenance is the scheduled (cron) handler body. Read paths never
// write; expiry, summary building and search backfill happen here in bounded
// steps. Each step runs even when an earlier one fails.
export async function runMaintenance(env: Bindings) {
  const steps: Array<[string, () => Promise<unknown>]> = [
    ["expire sessions", () => expireSessions(env.DB)],
    ["build summaries", () => buildPendingSummaries(env)],
    ["backfill search", () => backfillSearchIndex(env)],
  ];
  for (const [step, run] of steps) {
    try {
      await run();
    } catch (error) {
      console.error(JSON.stringify({ message: "maintenance step failed", step, error: error instanceof Error ? error.message : String(error) }));
    }
  }
}
