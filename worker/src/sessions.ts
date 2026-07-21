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

type NormalizedOutcome = {
  outcome: WorkOutcome;
  source: OutcomeSource;
  reason: string | null;
  evidence: unknown;
  evidenceJson: string | null;
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
  const normalized = normalizeOutcome(input, defaultSource);
  if ("error" in normalized) return c.json({ error: normalized.error }, 400);
  const id = c.req.param("id");
  if (!id) return c.json({ error: "session id is required" }, 400);
  if (!await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first()) return c.json({ error: "session not found" }, 404);
  const now = new Date().toISOString();
  await c.env.DB.batch(outcomeStatements(c.env.DB, id, normalized, now));
  return c.json(outcomeResult(id, normalized, now));
}

export async function endSession(c: Context<AppEnv>, defaultSource: OutcomeSource) {
  let input: OutcomeInput = {};
  if (c.req.header("content-type")?.includes("application/json")) {
    try {
      const parsed = await c.req.json<unknown>();
      if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return c.json({ error: "JSON body must be an object" }, 400);
      input = parsed as OutcomeInput;
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
  }
  if (input.outcome === undefined && (input.reason !== undefined || input.evidence !== undefined)) {
    return c.json({ error: "outcome is required when reason or evidence is provided" }, 400);
  }
  const normalized = input.outcome === undefined ? null : normalizeOutcome({ ...input, source: defaultSource }, defaultSource);
  if (normalized && "error" in normalized) return c.json({ error: normalized.error }, 400);
  const id = c.req.param("id");
  if (!id) return c.json({ error: "session id is required" }, 400);
  if (!await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first()) return c.json({ error: "session not found" }, 404);
  const now = new Date().toISOString();
  const endStatement = c.env.DB.prepare("UPDATE sessions SET state = 'inactive', ended_at = CASE WHEN inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at THEN ? ELSE ended_at END, inactive_at = CASE WHEN inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at THEN ? ELSE inactive_at END WHERE id = ?").bind(now, now, id);
  await endStatement.run();
  if (normalized && !("error" in normalized)) {
    const generation = await c.env.DB.prepare("SELECT inactive_at AS value FROM sessions WHERE id = ?").bind(id).first<{ value: string }>();
    const eventID = await endOutcomeEventID(id, generation?.value ?? "", normalized);
    await c.env.DB.batch([
      c.env.DB.prepare("INSERT OR IGNORE INTO session_outcome_events(id, session_id, outcome, source, reason, evidence_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)").bind(eventID, id, normalized.outcome, normalized.source, normalized.reason, normalized.evidenceJson, now),
      c.env.DB.prepare("UPDATE sessions SET work_outcome = ?, outcome = ?, outcome_src = ?, outcome_updated_at = (SELECT created_at FROM session_outcome_events WHERE id = ?), outcome_reason = ? WHERE id = ?").bind(normalized.outcome, legacyOutcome(normalized.outcome), normalized.source, eventID, normalized.reason, id),
    ]);
  }
  const session = await c.env.DB.prepare("SELECT id, state, ended_at, inactive_at, work_outcome AS outcome, outcome_src, outcome_updated_at, outcome_reason FROM sessions WHERE id = ?").bind(id).first();
  return c.json({ session, evidence: normalized && !("error" in normalized) ? normalized.evidence ?? null : null });
}

async function endOutcomeEventID(id: string, generation: string, outcome: NormalizedOutcome) {
  const value = JSON.stringify([id, generation, outcome.outcome, outcome.source, outcome.reason, outcome.evidenceJson]);
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return "end_" + Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function normalizeOutcome(input: OutcomeInput, defaultSource: OutcomeSource): NormalizedOutcome | { error: string } {
  const outcome = canonicalOutcome(input.outcome);
  if (!outcome) return { error: "invalid outcome" };
  const source = canonicalSource(input.source, defaultSource);
  if (!source) return { error: "invalid outcome source" };
  if (input.reason !== undefined && (typeof input.reason !== "string" || input.reason.length > 2_000)) return { error: "invalid outcome reason" };
  let evidenceJson: string | null = null;
  if (input.evidence !== undefined) {
    try {
      evidenceJson = JSON.stringify(input.evidence);
    } catch {
      return { error: "invalid outcome evidence" };
    }
    if (evidenceJson.length > 32_000) return { error: "outcome evidence too large" };
  }
  return { outcome, source, reason: typeof input.reason === "string" ? input.reason : null, evidence: input.evidence, evidenceJson };
}

function outcomeStatements(db: D1Database, id: string, outcome: NormalizedOutcome, now: string) {
  return [
    db.prepare("UPDATE sessions SET work_outcome = ?, outcome = ?, outcome_src = ?, outcome_updated_at = ?, outcome_reason = ? WHERE id = ?").bind(outcome.outcome, legacyOutcome(outcome.outcome), outcome.source, now, outcome.reason, id),
    db.prepare("INSERT INTO session_outcome_events(id, session_id, outcome, source, reason, evidence_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)").bind(ulid(), id, outcome.outcome, outcome.source, outcome.reason, outcome.evidenceJson, now),
  ];
}

function outcomeResult(id: string, outcome: NormalizedOutcome, now: string) {
  return { id, outcome: outcome.outcome, outcome_src: outcome.source, outcome_updated_at: now, outcome_reason: outcome.reason, evidence: outcome.evidence ?? null };
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
