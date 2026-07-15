import type { Context } from "hono";
import { readSaveConfig } from "./config";
import type { AppEnv } from "./types";

export const SESSION_COLUMNS = "id, started_at, ended_at, state, last_active_at, inactive_at, harness, boundary, outcome, outcome_src, repo, source_ref, model_primary, request_count, tokens_in, tokens_out, intent";

export async function resolveSession(db: D1Database, declared: string | null, repo: string | null, harness: string | null, sourceRef: string | null, model: string, now: string) {
  if (declared) {
    await db.prepare("INSERT OR IGNORE INTO sessions(id, started_at, last_active_at, harness, boundary, repo, source_ref, model_primary) VALUES (?, ?, ?, ?, 'header', ?, ?, ?)").bind(declared, now, now, harness, repo, sourceRef, model).run();
    return { id: declared };
  }
  const config = await readSaveConfig(db);
  const cutoff = new Date(Date.parse(now) - config.gapMinutes * 60_000).toISOString();
  const prior = await db.prepare("SELECT id FROM sessions WHERE boundary = 'heuristic' AND state = 'active' AND repo IS ? AND harness IS ? AND last_active_at >= ? ORDER BY last_active_at DESC LIMIT 1").bind(repo, harness, cutoff).first<{ id: string }>();
  if (prior) return prior;
  const id = ulid();
  await db.prepare("INSERT OR IGNORE INTO sessions(id, started_at, last_active_at, harness, boundary, repo, model_primary) VALUES (?, ?, ?, ?, 'heuristic', ?, ?)").bind(id, now, now, harness, repo, model).run();
  const active = await db.prepare("SELECT id FROM sessions WHERE boundary = 'heuristic' AND state = 'active' AND repo IS ? AND harness IS ? ORDER BY last_active_at DESC LIMIT 1").bind(repo, harness).first<{ id: string }>();
  if (!active) throw new Error("could not resolve heuristic session");
  return active;
}

export async function expireSessions(db: D1Database, gapMinutes?: number, now = new Date().toISOString()) {
  const gap = gapMinutes ?? (await readSaveConfig(db)).gapMinutes;
  const cutoff = new Date(Date.parse(now) - gap * 60_000).toISOString();
  await db.prepare("UPDATE sessions SET state = 'inactive', inactive_at = COALESCE(inactive_at, ?), ended_at = COALESCE(ended_at, last_active_at) WHERE state = 'active' AND last_active_at < ?").bind(now, cutoff).run();
}

export async function updateOutcome(c: Context<AppEnv>, outcome: string | undefined, source: "explicit" | "git") {
  const outcomes = new Set(["promoted", "discarded", "abandoned", "unknown"]);
  if (!outcome || !outcomes.has(outcome)) return c.json({ error: "invalid outcome" }, 400);
  const result = await c.env.DB.prepare("UPDATE sessions SET outcome = ?, outcome_src = ? WHERE id = ?").bind(outcome, source, c.req.param("id")).run();
  if (!result.meta.changes) return c.json({ error: "session not found" }, 404);
  return c.json({ id: c.req.param("id"), outcome, outcome_src: source });
}

export function ulid() {
  const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";
  let time = Date.now();
  let prefix = "";
  for (let i = 0; i < 10; i++) {
    prefix = alphabet[time % 32] + prefix;
    time = Math.floor(time / 32);
  }
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return prefix + Array.from(bytes, (byte) => alphabet[byte % 32]).join("");
}
