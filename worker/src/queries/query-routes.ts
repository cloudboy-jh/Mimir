import { Hono, type Context } from "hono";
import type { AppEnv } from "../env";
import { loadGitArtifactPatch } from "../sessions/git-artifacts";
import { QueryError } from "../shared/query-error";
import { listCommits, patchLimit } from "./commits";
import { daySnapshot } from "./days";
import { listDevices, renameDevice, revokeDevice } from "./devices";
import { queryContext } from "./query-context";
import { searchSessions } from "./search";
import { getReceipt, getSession, listSessions, setSessionTitle } from "./sessions";
import { stats } from "./stats";
import { listTurns } from "./turns";

async function jsonBody(c: Context<AppEnv>) {
  try {
    return await c.req.json<unknown>();
  } catch {
    throw new QueryError(400, "invalid JSON body");
  }
}

// queryRouter is the read API shared by agents (`/api`, machine token) and the
// dashboard (`/dashboard/api`, Cloudflare Access). Handlers never write except
// for the explicit manual title edit.
export function queryRouter() {
  const router = new Hono<AppEnv>();
  router.get("/sessions", async (c) => c.json(await listSessions(queryContext(c), c.req.query())));
  router.get("/sessions/:id", async (c) => c.json(await getSession(queryContext(c), c.req.param("id"))));
  router.get("/sessions/:id/receipt", async (c) => c.json(await getReceipt(queryContext(c), c.req.param("id"))));
  router.get("/sessions/:id/turns", async (c) => c.json(await listTurns(queryContext(c), c.req.param("id"), c.req.query())));
  router.patch("/sessions/:id/title", async (c) => c.json(await setSessionTitle(queryContext(c), c.req.param("id"), await jsonBody(c))));
  router.get("/sessions/:id/commits/:sha/patch", async (c) => {
    const patch = await loadGitArtifactPatch(c.env.DB, c.env.LOGS, c.req.param("id"), c.req.param("sha"), patchLimit(c.req.query("max_bytes"), null));
    return new Response(patch.body, {
      headers: {
        "content-type": "text/plain; charset=utf-8",
        "cache-control": "private, no-store",
        "x-mimir-patch-sha256": patch.artifact.patch_sha256,
        "x-mimir-patch-bytes": String(patch.artifact.patch_bytes),
        "x-mimir-patch-truncated": String(patch.truncated),
      },
    });
  });
  router.get("/search", async (c) => c.json(await searchSessions(queryContext(c), c.req.query())));
  router.get("/stats", async (c) => c.json(await stats(queryContext(c), c.req.query())));
  router.get("/days/:date", async (c) => c.json(await daySnapshot(queryContext(c), c.req.param("date"), c.req.query("tz") || "UTC")));
  router.get("/devices", async (c) => c.json(await listDevices(queryContext(c))));
  router.get("/commits", async (c) => c.json(await listCommits(queryContext(c), c.req.query())));
  return router;
}

// Device rename and revocation are user actions, so only the Access-protected
// dashboard prefix serves them; machine tokens cannot revoke devices.
export function deviceEditRouter() {
  const router = new Hono<AppEnv>();
  router.patch("/devices/:id", async (c) => c.json(await renameDevice(queryContext(c), c.req.param("id"), await jsonBody(c))));
  router.post("/devices/:id/revoke", async (c) => c.json(await revokeDevice(queryContext(c), c.req.param("id"))));
  return router;
}
