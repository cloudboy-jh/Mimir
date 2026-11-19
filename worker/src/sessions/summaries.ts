import type { Bindings } from "../env";
import { readArchives } from "../exchanges/archives";
import { buildTurnContents, finalAssistantText, type Envelope } from "../exchanges/transcript";
import { captureSummaryValues, type CaptureSummary } from "./capture-status";
import { SUBTREE_CTE } from "./session-tree";

const EXCHANGE_LIMIT = 12;
const ARCHIVE_LIMIT = 4;
const SUMMARY_BATCH = 10;

type Section = "goal" | "actions" | "result" | "verification" | "unresolved";
export type SummaryEvidence = { section: Section; index: number; exchange_id: string };
export type SummaryContent = {
  goal: string | null;
  actions: string[];
  result: string | null;
  verification: string[];
  unresolved: string[];
  partial: boolean;
  evidence: SummaryEvidence[];
};
export type SessionSummary = SummaryContent & {
  status: "ready" | "pending" | "unavailable";
  updated_at: string | null;
};

export type SummaryInput = {
  intent: string | null;
  goalExchangeID: string | null;
  active: boolean;
  capture: CaptureSummary;
  errors: Array<{ signature: string; count: number; exchange_id: string | null }>;
  commits: Array<{ commit_sha: string; subject: string | null; capture_status: string; patch_files: number; pushed: number }>;
  /** Most recent saved primary archives, newest first. */
  archives: Array<{ exchange_id: string; session_id: string; envelope: Envelope | null }>;
  sampled: boolean;
};

function excerpt(value: string | null | undefined, limit = 240): string {
  const text = (value ?? "").replace(/\s+/g, " ").trim();
  return text.length > limit ? `${text.slice(0, limit).trimEnd()}…` : text;
}

function toolTarget(input: string | null) {
  if (!input) return "";
  try {
    const args = JSON.parse(input) as Record<string, unknown>;
    const value = args?.command ?? args?.cmd ?? args?.path ?? args?.file_path;
    return typeof value === "string" ? excerpt(value, 140) : "";
  } catch {
    return "";
  }
}

// summarizeSession reconstructs a factual summary from recorded evidence only:
// commits, observed tool activity, the final assistant response, errors and
// capture state. It never infers that a check passed.
export function summarizeSession(input: SummaryInput): SummaryContent {
  const summary: SummaryContent = {
    goal: excerpt(input.intent, 360) || null,
    actions: [],
    result: null,
    verification: [],
    unresolved: [],
    partial: input.active || input.capture.status !== "saved" || input.sampled || input.archives.some((archive) => !archive.envelope),
    evidence: [],
  };
  if (summary.goal && input.goalExchangeID) summary.evidence.push({ section: "goal", index: 0, exchange_id: input.goalExchangeID });
  const add = (section: "actions" | "verification" | "unresolved", text: string, exchangeID: string | null) => {
    if (summary[section].includes(text)) return;
    const index = summary[section].push(text) - 1;
    if (exchangeID) summary.evidence.push({ section, index, exchange_id: exchangeID });
  };

  for (const commit of input.commits.slice(0, 5)) {
    const subject = commit.subject ? `: ${excerpt(commit.subject, 160)}` : "";
    const patch = commit.capture_status === "saved" ? ` (${commit.patch_files} ${commit.patch_files === 1 ? "file" : "files"}${commit.pushed ? ", pushed" : ""})` : ` (patch ${commit.capture_status})`;
    add("actions", `Committed ${commit.commit_sha.slice(0, 8)}${subject}${patch}.`, null);
  }

  const chronological = [...input.archives].reverse();
  const turns = buildTurnContents(chronological.map((archive) => ({ exchange_id: archive.exchange_id, session_id: archive.session_id, primary: true, envelope: archive.envelope })));
  let observed = 0;
  for (const archive of input.archives) {
    for (const tool of turns.get(archive.exchange_id)?.tools ?? []) {
      if (observed++ >= 8) break;
      const target = toolTarget(tool.input);
      add("actions", `Ran ${tool.name}${target ? `: ${target}` : ""}${tool.error ? " (failed)" : ""}.`, archive.exchange_id);
      const output = excerpt(tool.output);
      if (output && (target || /(?:^|[._-])(?:bash|shell|exec|terminal|test|command)(?:$|[._-])/i.test(tool.name)))
        add("verification", `Observed ${tool.name} output: ${output}`, archive.exchange_id);
    }
  }

  for (const archive of input.archives) {
    const text = archive.envelope ? finalAssistantText(archive.envelope) : null;
    if (!text) continue;
    summary.result = excerpt(text, 360);
    summary.evidence.push({ section: "result", index: 0, exchange_id: archive.exchange_id });
    break;
  }
  if (!summary.result && input.commits.length) {
    const pushed = input.commits.filter((commit) => commit.pushed).length;
    summary.result = `${input.commits.length} ${input.commits.length === 1 ? "commit" : "commits"} recorded${pushed ? `, ${pushed} pushed` : ""}.`;
  }

  for (const error of input.errors.slice(0, 4))
    add("unresolved", `Recorded error: ${excerpt(error.signature, 180)} (${error.count} ${error.count === 1 ? "exchange" : "exchanges"}; resolution not established).`, error.exchange_id);
  if (!summary.verification.length) add("unresolved", "No verification output was observed.", null);
  if (input.capture.status !== "saved")
    add("unresolved", `Capture is ${input.capture.status}: ${input.capture.saved_exchanges} saved, ${input.capture.pending_exchanges} pending, ${input.capture.failed_exchanges} failed exchanges.`, null);
  if (input.active) add("unresolved", "A supporting session is still active.", null);
  if (input.sampled) add("unresolved", "Only the most recent exchanges were inspected.", null);
  const unavailable = input.archives.filter((archive) => !archive.envelope).length;
  if (unavailable) add("unresolved", `${unavailable} saved ${unavailable === 1 ? "archive was" : "archives were"} unavailable.`, null);
  return summary;
}

/** Projects stored summary columns into the API shape; null before the first build. */
export function storedSummary(row: { summary_status: string; summary_json: string | null; summary_updated_at: string | null }): SessionSummary | null {
  const status = row.summary_status === "ready" || row.summary_status === "unavailable" ? row.summary_status : "pending";
  if (row.summary_json) {
    try {
      return { status, updated_at: row.summary_updated_at, ...(JSON.parse(row.summary_json) as SummaryContent) };
    } catch {
      return null;
    }
  }
  if (status !== "unavailable") return null;
  return { status, updated_at: row.summary_updated_at, goal: null, actions: [], result: null, verification: [], unresolved: [], partial: false, evidence: [] };
}

// buildSessionSummary stores the summary of an inactive session's subtree.
// Active sessions are skipped; their summary is built when they end or expire.
export async function buildSessionSummary(env: Pick<Bindings, "DB" | "LOGS">, sessionID: string) {
  const db = env.DB;
  const [session, active, capture, errors, commits, exchanges, goal] = await db.batch<Record<string, unknown>>([
    db.prepare("SELECT intent, state FROM sessions WHERE id = ?").bind(sessionID),
    db.prepare(`${SUBTREE_CTE} SELECT 1 FROM sessions WHERE id IN (SELECT id FROM subtree) AND id <> ?2 AND state = 'active' LIMIT 1`).bind(sessionID, sessionID),
    db.prepare(`${SUBTREE_CTE} SELECT COALESCE(SUM(capture_status = 'saved'), 0) AS saved, COALESCE(SUM(capture_status = 'failed'), 0) AS failed, COALESCE(SUM(capture_status = 'accepted'), 0) AS pending, MAX(CASE WHEN capture_status = 'saved' THEN saved_at END) AS last_saved_at FROM exchanges WHERE session_id IN (SELECT id FROM subtree)`).bind(sessionID),
    db.prepare(`${SUBTREE_CTE} SELECT ee.signature, COUNT(DISTINCT ee.exchange_id) AS count, (SELECT latest.exchange_id FROM exchange_errors latest JOIN exchanges le ON le.id = latest.exchange_id WHERE latest.signature = ee.signature AND latest.session_id IN (SELECT id FROM subtree) ORDER BY le.ts DESC LIMIT 1) AS exchange_id FROM exchange_errors ee JOIN exchanges e ON e.id = ee.exchange_id WHERE ee.session_id IN (SELECT id FROM subtree) AND e.capture_status = 'saved' GROUP BY ee.signature ORDER BY count DESC, ee.signature`).bind(sessionID),
    db.prepare(`${SUBTREE_CTE} SELECT commit_sha, subject, capture_status, patch_files, pushed FROM session_git_artifacts WHERE session_id IN (SELECT id FROM subtree) ORDER BY COALESCE(committed_at, created_at) DESC, commit_sha`).bind(sessionID),
    db.prepare(`${SUBTREE_CTE} SELECT id, session_id, r2_key, COUNT(*) OVER () AS total FROM exchanges WHERE session_id IN (SELECT id FROM subtree) AND capture_status = 'saved' AND request_kind = 'primary' ORDER BY ts DESC, id DESC LIMIT ${EXCHANGE_LIMIT}`).bind(sessionID),
    db.prepare(`${SUBTREE_CTE} SELECT id FROM exchanges WHERE session_id IN (SELECT id FROM subtree) AND capture_status = 'saved' AND request_kind = 'primary' AND intent_candidate IS NOT NULL ORDER BY ts, id LIMIT 1`).bind(sessionID),
  ]);
  const row = session!.results[0] as { intent: string | null; state: string } | undefined;
  if (!row || row.state !== "inactive") return;
  const counts = capture!.results[0] as { saved: number; failed: number; pending: number; last_saved_at: string | null };
  const recent = exchanges!.results as Array<{ id: string; session_id: string; r2_key: string; total: number }>;
  const artifacts = commits!.results as SummaryInput["commits"];
  const updatedAt = new Date().toISOString();
  if (!counts.saved && !artifacts.length) {
    await db.prepare("UPDATE sessions SET summary_status = 'unavailable', summary_json = NULL, summary_updated_at = ? WHERE id = ?").bind(updatedAt, sessionID).run();
    return;
  }
  const inspected = recent.slice(0, ARCHIVE_LIMIT);
  const envelopes = await readArchives(env.LOGS, inspected.map((exchange) => exchange.r2_key));
  const summary = summarizeSession({
    intent: row.intent,
    goalExchangeID: (goal!.results[0] as { id: string } | undefined)?.id ?? null,
    active: active!.results.length > 0,
    capture: captureSummaryValues(counts.saved, counts.failed, counts.pending, counts.last_saved_at),
    errors: errors!.results as SummaryInput["errors"],
    commits: artifacts,
    archives: inspected.map((exchange, index) => ({ exchange_id: exchange.id, session_id: exchange.session_id, envelope: envelopes[index] ?? null })),
    sampled: (recent[0]?.total ?? 0) > ARCHIVE_LIMIT || artifacts.length > 5 || errors!.results.length > 4,
  });
  await db
    .prepare("UPDATE sessions SET summary_status = 'ready', summary_json = ?, summary_updated_at = ? WHERE id = ?")
    .bind(JSON.stringify(summary), updatedAt, sessionID)
    .run();
}

/** Builds summaries for a bounded batch of inactive root sessions still pending. */
export async function buildPendingSummaries(env: Pick<Bindings, "DB" | "LOGS">, limit = SUMMARY_BATCH) {
  const pending = await env.DB.prepare(
    "SELECT id FROM sessions WHERE parent_session_id IS NULL AND state = 'inactive' AND summary_status = 'pending' ORDER BY last_active_at DESC LIMIT ?",
  )
    .bind(limit)
    .all<{ id: string }>();
  for (const { id } of pending.results) await buildSessionSummary(env, id);
  return pending.results.length;
}
