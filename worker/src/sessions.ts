import type { Context } from "hono";
import { readSaveConfig } from "./config";
import type { AppEnv } from "./types";

export const SESSION_COLUMNS = "id, started_at, ended_at, state, last_active_at, inactive_at, harness, boundary, work_outcome AS outcome, outcome_src, outcome_updated_at, outcome_reason, repo, source_ref, model_primary, request_count, tokens_in, tokens_out, intent";

export type WorkOutcome = "landed" | "discarded" | "abandoned" | "unresolved";
export type OutcomeSource = "agent" | "user" | "git";

type OutcomeInput = {
  outcome?: string;
  source?: string;
  reason?: unknown;
  evidence?: unknown;
};

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

export async function updateOutcome(c: Context<AppEnv>, input: OutcomeInput, defaultSource: OutcomeSource) {
  const outcome = canonicalOutcome(input.outcome);
  if (!outcome) return c.json({ error: "invalid outcome" }, 400);
  const source = canonicalSource(input.source, defaultSource);
  if (!source) return c.json({ error: "invalid outcome source" }, 400);
  if (input.reason !== undefined && (typeof input.reason !== "string" || input.reason.length > 2_000)) return c.json({ error: "invalid outcome reason" }, 400);
  let evidenceJson: string | null = null;
  if (input.evidence !== undefined) {
    try {
      evidenceJson = JSON.stringify(input.evidence);
    } catch {
      return c.json({ error: "invalid outcome evidence" }, 400);
    }
    if (evidenceJson.length > 32_000) return c.json({ error: "outcome evidence too large" }, 400);
  }
  const id = c.req.param("id");
  if (!await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first()) return c.json({ error: "session not found" }, 404);
  const now = new Date().toISOString();
  const reason = typeof input.reason === "string" ? input.reason : null;
  await c.env.DB.batch([
    c.env.DB.prepare("UPDATE sessions SET work_outcome = ?, outcome = ?, outcome_src = ?, outcome_updated_at = ?, outcome_reason = ? WHERE id = ?").bind(outcome, legacyOutcome(outcome), source, now, reason, id),
    c.env.DB.prepare("INSERT INTO session_outcome_events(id, session_id, outcome, source, reason, evidence_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)").bind(ulid(), id, outcome, source, reason, evidenceJson, now),
  ]);
  return c.json({ id, outcome, outcome_src: source, outcome_updated_at: now, outcome_reason: reason, evidence: input.evidence ?? null });
}

export function canonicalOutcome(outcome: string | undefined): WorkOutcome | null {
  if (outcome === "promoted") return "landed";
  if (outcome === "unknown") return "unresolved";
  return outcome === "landed" || outcome === "discarded" || outcome === "abandoned" || outcome === "unresolved" ? outcome : null;
}

function canonicalSource(source: string | undefined, fallback: OutcomeSource): OutcomeSource | null {
  if (source === undefined) return fallback;
  if (source === "explicit") return "user";
  return source === "agent" || source === "user" || source === "git" ? source : null;
}

function legacyOutcome(outcome: WorkOutcome) {
  if (outcome === "landed") return "promoted";
  if (outcome === "unresolved") return "unknown";
  return outcome;
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
