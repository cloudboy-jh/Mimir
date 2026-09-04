import type { Context } from "hono";
import type { AppEnv, Bindings } from "../env";
import { ulid } from "../shared/ulid";
import { reportSessionEvent, SESSION_ID } from "./events";
import { rootSessionID } from "./session-queries";
export type WorkOutcome = "landed" | "discarded" | "abandoned" | "unresolved";
export type OutcomeSource = "agent" | "user" | "git" | "auto";

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
export async function updateOutcome(
  c: Context<AppEnv>,
  input: OutcomeInput,
  defaultSource: OutcomeSource,
) {
  const id = c.req.param("id");
  if (!id) return c.json({ error: "session id is required" }, 400);
  if (
    !(await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?")
      .bind(id)
      .first())
  )
    return c.json({ error: "session not found" }, 404);
  const gitEvidenceError = landedGitEvidenceError(input);
  if (gitEvidenceError) return c.json({ error: gitEvidenceError }, 400);
  const outcomeSessionID = await rootSessionID(c.env.DB, id);
  const validation = normalizeOutcome(
    withoutOutcomePatch(input),
    defaultSource,
  );
  if ("error" in validation) return c.json({ error: validation.error }, 400);
  const prepared = await persistOutcomePatch(
    c.env.LOGS,
    outcomeSessionID,
    input,
  );
  if ("error" in prepared) return c.json({ error: prepared.error }, 400);
  const normalized = normalizeOutcome(prepared, defaultSource);
  if ("error" in normalized) return c.json({ error: normalized.error }, 400);
  const now = new Date().toISOString();
  await c.env.DB.batch(
    outcomeStatements(c.env.DB, outcomeSessionID, normalized, now),
  );
  return c.json(outcomeResult(outcomeSessionID, normalized, now));
}

export async function bulkUpdateOutcomes(
  c: Context<AppEnv>,
  input: { session_ids?: unknown; outcome?: string; reason?: unknown },
) {
  if (
    !Array.isArray(input.session_ids) ||
    input.session_ids.length === 0 ||
    input.session_ids.length > 100 ||
    input.session_ids.some((id) => typeof id !== "string" || !SESSION_ID.test(id))
  )
    return c.json(
      { error: "session_ids must contain between 1 and 100 valid session ids" },
      400,
    );
  const sessionIDs = [...new Set(input.session_ids as string[])];
  const normalized = normalizeOutcome(
    {
      outcome: input.outcome,
      reason: input.reason,
      source: "user",
    },
    "user",
  );
  if ("error" in normalized) return c.json({ error: normalized.error }, 400);
  const placeholders = sessionIDs.map(() => "?").join(", ");
  const rows = await c.env.DB.prepare(
    `SELECT id, parent_session_id FROM sessions WHERE id IN (${placeholders})`,
  )
    .bind(...sessionIDs)
    .all<{ id: string; parent_session_id: string | null }>();
  if (
    rows.results.length !== sessionIDs.length ||
    rows.results.some((row) => row.parent_session_id !== null)
  )
    return c.json(
      { error: "all session_ids must identify root sessions" },
      400,
    );
  const now = new Date().toISOString();
  await c.env.DB.batch(
    sessionIDs.flatMap((id) =>
      outcomeStatements(c.env.DB, id, normalized, now),
    ),
  );
  return c.json({
    updated: sessionIDs.map((id) => outcomeResult(id, normalized, now)),
  });
}

const AUTO_OUTCOME_STALE_MS = 48 * 60 * 60 * 1_000;
const AUTO_OUTCOME_CANDIDATE_LIMIT = 100;

type AutoOutcomeCandidate = {
  session_id: string;
  commit_sha: string;
  parent_commit_sha: string | null;
  committed_at: string | null;
  subject: string | null;
  repository_url: string | null;
  ref: string | null;
  provenance: string;
  patch_r2_key: string;
  patch_sha256: string;
  patch_bytes: number;
  patch_files: number;
  patch_additions: number;
  patch_deletions: number;
};

export async function autoResolveStaleOutcomes(
  env: Pick<Bindings, "DB" | "LOGS">,
  now = new Date().toISOString(),
) {
  const cutoff = new Date(
    Date.parse(now) - AUTO_OUTCOME_STALE_MS,
  ).toISOString();
  const result = await env.DB.prepare(
    `WITH RECURSIVE session_tree(root_id, id) AS (
      SELECT id, id FROM sessions WHERE parent_session_id IS NULL
      UNION ALL
      SELECT session_tree.root_id, sessions.id
      FROM sessions JOIN session_tree ON sessions.parent_session_id = session_tree.id
    ),
    root_activity(root_id, activity_at) AS (
      SELECT session_tree.root_id, MAX(COALESCE(activity.last_active_at, activity.started_at))
      FROM session_tree JOIN sessions activity ON activity.id = session_tree.id
      GROUP BY session_tree.root_id
    )
    SELECT root.id AS session_id, artifact.commit_sha, artifact.parent_commit_sha,
      artifact.committed_at, artifact.subject, artifact.repository_url, artifact.ref,
      artifact.provenance, artifact.patch_r2_key, artifact.patch_sha256,
      artifact.patch_bytes, artifact.patch_files, artifact.patch_additions,
      artifact.patch_deletions
    FROM sessions root
    JOIN root_activity ON root_activity.root_id = root.id
    JOIN session_tree ON session_tree.root_id = root.id
    JOIN session_git_artifacts artifact ON artifact.session_id = session_tree.id
    WHERE root.parent_session_id IS NULL
      AND root.work_outcome = 'unresolved'
      AND (root.outcome_src IS NULL OR root.outcome_src <> 'user')
      AND root_activity.activity_at <= ?
      AND artifact.capture_status = 'saved'
      AND length(artifact.commit_sha) = 40
      AND artifact.commit_sha NOT GLOB '*[^0-9a-f]*'
    ORDER BY root.id, COALESCE(artifact.committed_at, artifact.created_at) DESC
    LIMIT ?`,
  )
    .bind(cutoff, AUTO_OUTCOME_CANDIDATE_LIMIT)
    .all<AutoOutcomeCandidate>();
  const availability = await Promise.all(
    result.results.map(async (candidate) => ({
      candidate,
      available: (await env.LOGS.head(candidate.patch_r2_key)) !== null,
    })),
  );
  const selected = new Map<string, AutoOutcomeCandidate>();
  for (const item of availability) {
    if (item.available && !selected.has(item.candidate.session_id))
      selected.set(item.candidate.session_id, item.candidate);
  }
  if (selected.size === 0) return { count: 0, session_ids: [] as string[] };
  const statements: D1PreparedStatement[] = [];
  const sessionIDs: string[] = [];
  for (const candidate of selected.values()) {
    const reason =
      "Automatically marked landed after 48 hours of inactivity with saved commit and patch evidence";
    const evidenceJson = JSON.stringify({
      commit: candidate.commit_sha,
      parent_commit: candidate.parent_commit_sha,
      committed_at: candidate.committed_at,
      subject: candidate.subject,
      repository_url: candidate.repository_url,
      ref: candidate.ref,
      provenance: candidate.provenance,
      patch_r2_key: candidate.patch_r2_key,
      patch_bytes: candidate.patch_bytes,
      patch_files: candidate.patch_files,
      patch_additions: candidate.patch_additions,
      patch_deletions: candidate.patch_deletions,
      automation: {
        stale_hours: 48,
        evidence_threshold: 2,
        signals: ["saved_git_artifact", "retrievable_patch"],
      },
    });
    const eventID = `auto_${ulid()}`;
    statements.push(
      env.DB.prepare(
        "INSERT OR IGNORE INTO session_outcome_events(id, session_id, outcome, source, reason, evidence_json, created_at) SELECT ?, id, 'landed', 'auto', ?, ?, ? FROM sessions WHERE id = ? AND work_outcome = 'unresolved' AND (outcome_src IS NULL OR outcome_src <> 'user')",
      ).bind(
        eventID,
        reason,
        evidenceJson,
        now,
        candidate.session_id,
      ),
      env.DB.prepare(
        "UPDATE sessions SET work_outcome = 'landed', outcome = 'promoted', outcome_src = 'auto', outcome_updated_at = ?, outcome_reason = ?, summary_text = NULL, summary_status = 'pending', summary_source = NULL, summary_updated_at = NULL WHERE id = ? AND work_outcome = 'unresolved' AND (outcome_src IS NULL OR outcome_src <> 'user') AND EXISTS (SELECT 1 FROM session_outcome_events WHERE id = ?)",
      ).bind(now, reason, candidate.session_id, eventID),
    );
    sessionIDs.push(candidate.session_id);
  }
  const writes = await env.DB.batch(statements);
  const updated = sessionIDs.filter(
    (_id, index) => (writes[index * 2 + 1]?.meta.changes ?? 0) > 0,
  );
  return { count: updated.length, session_ids: updated };
}

export async function endSession(
  c: Context<AppEnv>,
  defaultSource: OutcomeSource,
) {
  let input: OutcomeInput = {};
  if (c.req.header("content-type")?.includes("application/json")) {
    try {
      const parsed = await c.req.json<unknown>();
      if (
        typeof parsed !== "object" ||
        parsed === null ||
        Array.isArray(parsed)
      )
        return c.json({ error: "JSON body must be an object" }, 400);
      input = parsed as OutcomeInput;
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
  }
  if (
    input.outcome === undefined &&
    (input.reason !== undefined || input.evidence !== undefined)
  ) {
    return c.json(
      { error: "outcome is required when reason or evidence is provided" },
      400,
    );
  }
  const id = c.req.param("id");
  if (!id) return c.json({ error: "session id is required" }, 400);
  if (input.outcome !== undefined) {
    const gitEvidenceError = landedGitEvidenceError(input);
    if (gitEvidenceError) return c.json({ error: gitEvidenceError }, 400);
    const validation = normalizeOutcome(
      withoutOutcomePatch({ ...input, source: defaultSource }),
      defaultSource,
    );
    if ("error" in validation) return c.json({ error: validation.error }, 400);
    const patchError = outcomePatchError(input);
    if (patchError) return c.json({ error: patchError }, 400);
  }
  const now = new Date().toISOString();
  // Sessions known only to the session object (harness-plugin reporting with
  // no captured exchanges yet) have no D1 row until finalize. Explicit end
  // must still work: forward the end so the object finalizes and upserts the
  // row, then continue the normal flow. Sessions unknown to both D1 and the
  // object keep the 404 contract.
  if (
    !(await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?")
      .bind(id)
      .first())
  ) {
    if (!(await finalizeObjectOnlySession(c, id, now)))
      return c.json({ error: "session not found" }, 404);
  }
  const outcomeSessionID = await rootSessionID(c.env.DB, id);
  const prepared =
    input.outcome === undefined
      ? input
      : await persistOutcomePatch(c.env.LOGS, outcomeSessionID, {
          ...input,
          source: defaultSource,
        });
  if ("error" in prepared) return c.json({ error: prepared.error }, 400);
  const normalized =
    prepared.outcome === undefined
      ? null
      : normalizeOutcome(prepared, defaultSource);
  if (normalized && "error" in normalized)
    return c.json({ error: normalized.error }, 400);
  const endStatement = c.env.DB.prepare(
    "UPDATE sessions SET state = 'inactive', ended_at = CASE WHEN inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at THEN ? ELSE ended_at END, inactive_at = CASE WHEN inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at THEN ? ELSE inactive_at END WHERE id = ?",
  ).bind(now, now, id);
  await endStatement.run();
  if (normalized && !("error" in normalized)) {
    if (normalized.evidenceJson === null) {
      const current = await c.env.DB.prepare(
        "SELECT work_outcome AS outcome FROM sessions WHERE id = ?",
      )
        .bind(outcomeSessionID)
        .first<{ outcome: string }>();
      if (current?.outcome === normalized.outcome) {
        const prior = await c.env.DB.prepare(
          "SELECT evidence_json FROM session_outcome_events WHERE session_id = ? AND outcome = ? AND evidence_json IS NOT NULL ORDER BY created_at DESC, rowid DESC LIMIT 1",
        )
          .bind(outcomeSessionID, normalized.outcome)
          .first<{ evidence_json: string }>();
        if (prior) {
          normalized.evidenceJson = prior.evidence_json;
          normalized.evidence = JSON.parse(prior.evidence_json);
        }
      }
    }
    const generation = await c.env.DB.prepare(
      "SELECT inactive_at AS value FROM sessions WHERE id = ?",
    )
      .bind(id)
      .first<{ value: string }>();
    const eventID = await endOutcomeEventID(
      outcomeSessionID,
      generation?.value ?? "",
      normalized,
    );
    await c.env.DB.batch([
      c.env.DB.prepare(
        "INSERT OR IGNORE INTO session_outcome_events(id, session_id, outcome, source, reason, evidence_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
      ).bind(
        eventID,
        outcomeSessionID,
        normalized.outcome,
        normalized.source,
        normalized.reason,
        normalized.evidenceJson,
        now,
      ),
      c.env.DB.prepare(
        "UPDATE sessions SET work_outcome = ?, outcome = ?, outcome_src = ?, outcome_updated_at = (SELECT created_at FROM session_outcome_events WHERE id = ?), outcome_reason = ?, summary_text = NULL, summary_status = 'pending', summary_source = NULL, summary_updated_at = NULL WHERE id = ?",
      ).bind(
        normalized.outcome,
        legacyOutcome(normalized.outcome),
        normalized.source,
        eventID,
        normalized.reason,
        outcomeSessionID,
      ),
    ]);
  }
  const session = await c.env.DB.prepare(
    "SELECT id, state, ended_at, inactive_at, work_outcome AS outcome, outcome_src, outcome_updated_at, outcome_reason FROM sessions WHERE id = ?",
  )
    .bind(id)
    .first();
  await reportSessionEvent(c.env, {
    version: 1,
    kind: "end",
    session_id: id,
    installation_id: c.get("installationID"),
    harness: null,
    ts: now,
    reason: "explicit",
  });
  return c.json({
    session,
    evidence:
      normalized && !("error" in normalized)
        ? (normalized.evidence ?? null)
        : null,
  });
}

const MAX_PATCH_BYTES = 5 * 1024 * 1024;
const COMMIT_SHA = /^[0-9a-f]{40}$/i;

function landedGitEvidenceError(input: OutcomeInput): string {
  if (canonicalOutcome(input.outcome) !== "landed") return "";
  const evidence = input.evidence;
  if (
    !evidence ||
    typeof evidence !== "object" ||
    Array.isArray(evidence) ||
    !("provenance" in evidence) ||
    typeof evidence.provenance !== "string"
  )
    return "";
  if (
    !("commit" in evidence) ||
    typeof evidence.commit !== "string" ||
    !COMMIT_SHA.test(evidence.commit)
  )
    return "landed Git outcomes require a full commit SHA";
  if (
    !("patch" in evidence) ||
    typeof evidence.patch !== "string" ||
    !evidence.patch.trim()
  )
    return "landed Git outcomes require a retrievable patch";
  return "";
}

function outcomePatchError(input: OutcomeInput): string {
  if (
    !input.evidence ||
    typeof input.evidence !== "object" ||
    Array.isArray(input.evidence)
  )
    return "";
  const patch = (input.evidence as Record<string, unknown>).patch;
  return typeof patch === "string" &&
    new TextEncoder().encode(patch).byteLength > MAX_PATCH_BYTES
    ? "outcome patch exceeds 5 MiB"
    : "";
}

function withoutOutcomePatch(input: OutcomeInput): OutcomeInput {
  if (
    !input.evidence ||
    typeof input.evidence !== "object" ||
    Array.isArray(input.evidence)
  )
    return input;
  const { patch: _patch, ...evidence } = input.evidence as Record<
    string,
    unknown
  >;
  return { ...input, evidence };
}

async function persistOutcomePatch(
  bucket: R2Bucket,
  sessionID: string,
  input: OutcomeInput,
): Promise<OutcomeInput | { error: string }> {
  if (
    !input.evidence ||
    typeof input.evidence !== "object" ||
    Array.isArray(input.evidence)
  )
    return input;
  const evidence = input.evidence as Record<string, unknown>;
  if (typeof evidence.patch !== "string" || evidence.patch.length === 0)
    return input;
  const bytes = new TextEncoder().encode(evidence.patch);
  if (bytes.byteLength > MAX_PATCH_BYTES)
    return { error: "outcome patch exceeds 5 MiB" };
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  const hash = Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
  const key = `sessions/${sessionID}/diffs/${hash}.patch`;
  let files = 0,
    additions = 0,
    deletions = 0;
  for (const line of evidence.patch.split("\n")) {
    if (line.startsWith("diff --git ")) files++;
    else if (line.startsWith("+") && !line.startsWith("+++")) additions++;
    else if (line.startsWith("-") && !line.startsWith("---")) deletions++;
  }
  const { patch: _patch, ...rest } = evidence;
  const prepared = {
    ...input,
    evidence: {
      ...rest,
      patch_r2_key: key,
      patch_bytes: bytes.byteLength,
      patch_files: files,
      patch_additions: additions,
      patch_deletions: deletions,
    },
  };
  if (JSON.stringify(prepared.evidence).length > 32_000)
    return { error: "outcome evidence too large" };
  await bucket.put(key, bytes, {
    httpMetadata: { contentType: "text/plain; charset=utf-8" },
    customMetadata: { session_id: sessionID, sha256: hash },
  });
  return prepared;
}
async function finalizeObjectOnlySession(
  c: Context<AppEnv>,
  id: string,
  now: string,
): Promise<boolean> {
  try {
    const stub = c.env.SESSIONS.get(c.env.SESSIONS.idFromName(id));
    const state = await stub.fetch("https://session-object/state");
    if (!state.ok) return false;
    const body = await state.json<{ session_id?: string }>();
    if (body.session_id !== id) return false;
    await reportSessionEvent(c.env, {
      version: 1,
      kind: "end",
      session_id: id,
      installation_id: c.get("installationID"),
      harness: null,
      ts: now,
      reason: "explicit",
    });
    return (
      (await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?")
        .bind(id)
        .first()) !== null
    );
  } catch {
    return false;
  }
}

async function endOutcomeEventID(
  id: string,
  generation: string,
  outcome: NormalizedOutcome,
) {
  const value = JSON.stringify([
    id,
    generation,
    outcome.outcome,
    outcome.source,
    outcome.reason,
    outcome.evidenceJson,
  ]);
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(value),
  );
  return (
    "end_" +
    Array.from(new Uint8Array(digest), (byte) =>
      byte.toString(16).padStart(2, "0"),
    ).join("")
  );
}

function normalizeOutcome(
  input: OutcomeInput,
  defaultSource: OutcomeSource,
): NormalizedOutcome | { error: string } {
  const outcome = canonicalOutcome(input.outcome);
  if (!outcome) return { error: "invalid outcome" };
  const source = canonicalSource(input.source, defaultSource);
  if (!source) return { error: "invalid outcome source" };
  if (
    input.reason !== undefined &&
    (typeof input.reason !== "string" || input.reason.length > 2_000)
  )
    return { error: "invalid outcome reason" };
  let evidenceJson: string | null = null;
  if (input.evidence !== undefined) {
    try {
      evidenceJson = JSON.stringify(input.evidence);
    } catch {
      return { error: "invalid outcome evidence" };
    }
    if (evidenceJson.length > 32_000)
      return { error: "outcome evidence too large" };
  }
  return {
    outcome,
    source,
    reason: typeof input.reason === "string" ? input.reason : null,
    evidence: input.evidence,
    evidenceJson,
  };
}

function outcomeStatements(
  db: D1Database,
  id: string,
  outcome: NormalizedOutcome,
  now: string,
) {
  return [
    db
      .prepare(
        "UPDATE sessions SET work_outcome = ?, outcome = ?, outcome_src = ?, outcome_updated_at = ?, outcome_reason = ?, summary_text = NULL, summary_status = 'pending', summary_source = NULL, summary_updated_at = NULL WHERE id = ?",
      )
      .bind(
        outcome.outcome,
        legacyOutcome(outcome.outcome),
        outcome.source,
        now,
        outcome.reason,
        id,
      ),
    db
      .prepare(
        "INSERT INTO session_outcome_events(id, session_id, outcome, source, reason, evidence_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
      )
      .bind(
        ulid(),
        id,
        outcome.outcome,
        outcome.source,
        outcome.reason,
        outcome.evidenceJson,
        now,
      ),
  ];
}

function outcomeResult(id: string, outcome: NormalizedOutcome, now: string) {
  return {
    id,
    outcome: outcome.outcome,
    outcome_src: outcome.source,
    outcome_updated_at: now,
    outcome_reason: outcome.reason,
    evidence: outcome.evidence ?? null,
  };
}

export function canonicalOutcome(
  outcome: string | undefined,
): WorkOutcome | null {
  if (outcome === "promoted") return "landed";
  if (outcome === "unknown") return "unresolved";
  return outcome === "landed" ||
    outcome === "discarded" ||
    outcome === "abandoned" ||
    outcome === "unresolved"
    ? outcome
    : null;
}

function canonicalSource(
  source: string | undefined,
  fallback: OutcomeSource,
): OutcomeSource | null {
  if (source === undefined) return fallback;
  if (source === "explicit") return "user";
  return source === "agent" ||
    source === "user" ||
    source === "git" ||
    source === "auto"
    ? source
    : null;
}

function legacyOutcome(outcome: WorkOutcome) {
  if (outcome === "landed") return "promoted";
  if (outcome === "unresolved") return "unknown";
  return outcome;
}
