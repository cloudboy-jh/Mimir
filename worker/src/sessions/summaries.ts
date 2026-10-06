import { readBoundedText } from "../exchanges/response-codec";
import type { CaptureSummary } from "./capture-status";
import { SESSION_SUBTREE_CTE } from "./session-queries";
import { SESSION_SUMMARY_VERSION } from "./summary-cache";

const EXCERPT_LIMIT = 12;
const ARCHIVE_LIMIT = 4;
const ARCHIVE_BYTES = 512 * 1024;

type Section = "goal" | "actions" | "result" | "verification" | "unresolved";
export type ReconstructedSummary = {
  goal: string | null;
  actions: string[];
  result: string | null;
  verification: string[];
  unresolved: string[];
  partial: boolean;
  source: "reconstructed";
  evidence: Array<{ section: Section; index: number; href: string; label: string }>;
};
type ExchangeEvidence = { id: string; session_id: string; response_excerpt: string; request_excerpt: string; r2_key: string; saved_at: string | null; evidence_count: number };
type ToolObservation = { exchange_id: string; side: "request" | "response"; name: string; input: unknown; status: string | null; output: unknown };
export type SummaryEvidence = {
  capture: CaptureSummary;
  children: Array<Record<string, unknown>>;
  errors: Array<{ signature: string; count: number; latest_exchange_id: string | null }>;
  gitArtifacts: Array<{ commit_sha: string; subject: string | null; capture_status: string; patch_files: number; saved_at?: string | null; patch_sha256?: string }>;
  outcomeEvents: Array<Record<string, unknown>>;
  exchanges?: ExchangeEvidence[];
  observations?: ToolObservation[];
  sampled?: boolean;
  unavailableArchives?: number;
};

function excerpt(value: unknown, limit = 240): string {
  if (typeof value !== "string") return "";
  const text = value.replace(/\s+/g, " ").trim();
  return text.length > limit ? `${text.slice(0, limit).trimEnd()}…` : text;
}
function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}
function outputExcerpt(value: unknown): string {
  if (typeof value === "string") return excerpt(value);
  if (value == null) return "";
  return excerpt(JSON.stringify(value));
}

export function buildSessionSummary(session: Record<string, unknown>, input: SummaryEvidence): ReconstructedSummary {
  const summary: ReconstructedSummary = {
    goal: excerpt(session.intent, 360) || null,
    actions: [], result: null, verification: [], unresolved: [],
    partial: session.state === "active" || input.children.some(child => child.state === "active") || input.capture.status !== "saved" || Boolean(input.sampled) || Boolean(input.unavailableArchives),
    source: "reconstructed", evidence: [],
  };
  const add = (section: "actions" | "verification" | "unresolved", text: string, href: string, label: string) => {
    if (summary[section].includes(text)) return;
    const index = summary[section].push(text) - 1;
    summary.evidence.push({ section, index, href, label });
  };
  const outcome = typeof session.outcome === "string" ? session.outcome : "unresolved";
  const reason = excerpt(session.outcome_reason, 360);
  const resultLabels: Record<string, string> = {
    landed: "Recorded outcome: landed", discarded: "Recorded outcome: discarded", abandoned: "Recorded outcome: abandoned", unresolved: "Recorded outcome: unresolved",
  };
  summary.result = `${resultLabels[outcome] ?? resultLabels.unresolved}${reason ? `. ${reason}` : ". No outcome reason was recorded."}`;
  if (!session.parent_session_id) {
    const event = input.outcomeEvents.find(event => event.outcome === outcome);
    if (event && typeof event.evidence_json === "string") {
      try {
        const evidence = record(JSON.parse(event.evidence_json));
        const note = excerpt(evidence?.note, 240);
        if (note) summary.result += ` Recorded outcome evidence note: ${note}`;
      } catch { /* Malformed outcome evidence remains available in the outcome record. */ }
    }
  }
  summary.evidence.push({ section: "result", index: 0, href: "#outcome-heading", label: "Recorded outcome" });
  if (summary.goal) summary.evidence.push({ section: "goal", index: 0, href: "#session-activity", label: "Session activity" });

  for (const artifact of input.gitArtifacts.slice(0, 5)) {
    add("actions", `Commit artifact ${artifact.commit_sha.slice(0, 8)}${artifact.subject ? `: ${excerpt(artifact.subject, 160)}` : ""}. Patch capture ${artifact.capture_status}${artifact.capture_status === "saved" ? ` (${artifact.patch_files} ${artifact.patch_files === 1 ? "file" : "files"} in the captured patch)` : ""}.`, "#changes-heading", "Commit evidence");
  }
  for (const observation of (input.observations ?? []).slice(0, 8)) {
    const args = record(observation.input);
    const command = excerpt(args?.command ?? args?.cmd, 140);
    const target = excerpt(args?.path ?? args?.file_path, 100);
    const description = `${observation.name}${command ? `: ${command}` : target ? `: ${target}` : ""}`;
    const href = `/requests/${encodeURIComponent(observation.exchange_id)}?session=${encodeURIComponent(String(session.id ?? ""))}&view=conversation&side=${observation.side}#request-evidence-panel`;
    add("actions", `Recorded tool ${description}${observation.status ? ` (${observation.status})` : ""}.`, href, "Tool evidence");
    const output = outputExcerpt(observation.output);
    if (output && (command || /(?:^|[._-])(?:bash|shell|exec|terminal|test|command)(?:$|[._-])/i.test(observation.name))) {
      // Tool output is an observation, not proof that an invoked check passed.
      add("verification", `Observed ${observation.name} output: ${output}`, href, "Observed output");
    }
  }
  if (!summary.actions.length) {
    const latest = input.exchanges?.find(exchange => excerpt(exchange.response_excerpt));
    if (latest) add("actions", `Saved assistant response excerpt: ${excerpt(latest.response_excerpt)}`, `/requests/${encodeURIComponent(latest.id)}?session=${encodeURIComponent(String(session.id ?? ""))}&view=conversation&side=response`, "Saved response");
  }
  for (const error of input.errors.slice(0, 4)) {
    add("unresolved", `Recorded error signature: ${excerpt(error.signature, 180)} (${error.count} saved ${error.count === 1 ? "exchange" : "exchanges"}; resolution not established).`, error.latest_exchange_id ? `/requests/${encodeURIComponent(error.latest_exchange_id)}` : "#session-activity", "Error evidence");
  }
  if (outcome === "unresolved") add("unresolved", "No resolved work outcome has been recorded.", "#outcome-heading", "Outcome record");
  if (!summary.verification.length) add("unresolved", "No observed verification output is available in the inspected evidence.", "#session-activity", "Inspect activity");
  if (input.capture.status !== "saved") add("unresolved", `Capture is ${input.capture.status}: ${input.capture.saved_exchanges} saved, ${input.capture.pending_exchanges} pending, ${input.capture.failed_exchanges} failed exchanges. Unsaved evidence is not summarized.`, "#session-activity", "Capture evidence");
  if (session.state === "active" || input.children.some(child => child.state === "active")) add("unresolved", "This session or a supporting session is still active; the reconstruction is provisional.", "#session-activity", "Session activity");
  if (input.sampled) add("unresolved", "Only bounded recent saved excerpts and tool results were inspected; earlier evidence may add context.", "#session-activity", "Full activity");
  if (input.unavailableArchives) add("unresolved", `${input.unavailableArchives} selected saved ${input.unavailableArchives === 1 ? "archive could" : "archives could"} not be inspected.`, "#session-activity", "Saved archives");
  return summary;
}

export function summaryText(summary: ReconstructedSummary): string {
  return [
    summary.goal ? `Goal: ${summary.goal}` : null,
    summary.actions.length ? `Actions: ${summary.actions.join(" ")}` : "Actions: no action evidence is available.",
    summary.result,
    summary.verification.length ? `Observed output: ${summary.verification.join(" ")}` : "Verification: unknown.",
    summary.unresolved.length ? `Open evidence: ${summary.unresolved.join(" ")}` : null,
    summary.partial ? "Provisional reconstruction." : null,
  ].filter(Boolean).join(" ");
}

function archiveObservations(envelope: unknown, exchangeID: string): ToolObservation[] {
  const root = record(envelope);
  if (!root) return [];
  const observations: ToolObservation[] = [];
  if (Array.isArray(root.tool_activity)) {
    for (const activity of root.tool_activity) {
      const tool = record(activity);
      if (!tool || typeof tool.name !== "string") continue;
      observations.push({ exchange_id: exchangeID, side: "response", name: tool.name, input: tool.input, status: tool.status === "succeeded" || tool.status === "failed" ? tool.status : null, output: tool.output });
    }
  }
  // Proxy archives can contain tool results in the next request, without a
  // success status. Preserve that distinction instead of guessing exit codes.
  const request = record(root.request);
  if (Array.isArray(request?.messages)) {
    const calls = new Map<string, { name: string; input: unknown }>();
    for (const message of request.messages) {
      const assistant = record(message);
      if (Array.isArray(assistant?.tool_calls)) {
        for (const value of assistant.tool_calls) {
          const call = record(value);
          const fn = record(call?.function);
          if (typeof call?.id !== "string" || typeof fn?.name !== "string") continue;
          let input: unknown = null;
          if (typeof fn.arguments === "string") {
            try { input = JSON.parse(fn.arguments); } catch { /* Malformed inputs remain visible in the archive. */ }
          }
          calls.set(call.id, { name: fn.name, input });
        }
      }
      if (Array.isArray(assistant?.content)) {
        for (const value of assistant.content) {
          const call = record(value);
          if (call?.type === "tool_use" && typeof call.id === "string" && typeof call.name === "string") {
            calls.set(call.id, { name: call.name, input: call.input });
          }
        }
      }
    }
    for (const message of request.messages) {
      const tool = record(message);
      if (tool?.role === "tool" && typeof tool.content === "string") {
        const call = typeof tool.tool_call_id === "string" ? calls.get(tool.tool_call_id) : undefined;
        observations.push({ exchange_id: exchangeID, side: "request", name: call?.name ?? (typeof tool.name === "string" ? tool.name : "tool"), input: call?.input ?? null, status: null, output: tool.content });
      }
      if (Array.isArray(tool?.content)) {
        for (const value of tool.content) {
          const result = record(value);
          if (result?.type !== "tool_result" || typeof result.tool_use_id !== "string") continue;
          const call = calls.get(result.tool_use_id);
          observations.push({ exchange_id: exchangeID, side: "request", name: call?.name ?? "tool", input: call?.input ?? null, status: result.is_error === true ? "failed" : null, output: result.content });
        }
      }
    }
  }
  return observations;
}

export async function ensureSessionSummary(db: D1Database, bucket: R2Bucket, session: Record<string, unknown>, input: SummaryEvidence, observedAt: string) {
  const rows = await db.prepare(
    `${SESSION_SUBTREE_CTE} SELECT id, session_id, substr(response_excerpt, 1, 1000) AS response_excerpt, substr(request_excerpt, 1, 1000) AS request_excerpt, r2_key, saved_at, COUNT(*) OVER() AS evidence_count FROM exchanges WHERE session_id IN (SELECT id FROM subtree) AND capture_status = 'saved' AND request_kind = 'primary' ORDER BY saved_at DESC, id DESC LIMIT ?`,
  ).bind(session.id, EXCERPT_LIMIT).all<ExchangeEvidence>();
  const exchanges = rows.results;
  const observations: ToolObservation[] = [];
  let unavailableArchives = 0;
  for (const exchange of exchanges.slice(0, ARCHIVE_LIMIT)) {
    try {
      const object = await bucket.get(exchange.r2_key);
      if (!object) { unavailableArchives++; continue; }
      if (object.size > ARCHIVE_BYTES) { await object.body.cancel(); unavailableArchives++; continue; }
      const envelope: unknown = JSON.parse(await readBoundedText(object.body, ARCHIVE_BYTES));
      observations.push(...archiveObservations(envelope, exchange.id));
    } catch {
      unavailableArchives++;
    }
  }
  const evidence: SummaryEvidence = { ...input, exchanges, observations, unavailableArchives, sampled: (exchanges[0]?.evidence_count ?? 0) > ARCHIVE_LIMIT || observations.length > 8 || input.gitArtifacts.length > 5 || input.errors.length > 4 };
  const summary = buildSessionSummary(session, evidence);
  const text = summaryText(summary);
  // Hash the actual reconstruction and metadata, not just activity time. Late
  // saves, capture failures, child outcomes, and algorithm changes invalidate it.
  const sourceData = JSON.stringify({ version: SESSION_SUMMARY_VERSION, summary, capture: input.capture, children: input.children.map(child => [child.id, child.state, child.last_active_at, child.outcome, child.outcome_updated_at, child.outcome_reason, child.intent, child.request_count]), events: input.outcomeEvents, artifacts: input.gitArtifacts, exchanges });
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(sourceData)));
  const source = `${SESSION_SUMMARY_VERSION}:${Array.from(digest, byte => byte.toString(16).padStart(2, "0")).join("")}`;
  const status = summary.partial ? "pending" : "ready";
  if (session.summary_source === source && session.summary_text === text && session.summary_status === status) return { session, summary };
  // Evidence saved during collection must remain newer than this reconstruction.
  await db.prepare("UPDATE sessions SET summary_text = ?, summary_status = ?, summary_source = ?, summary_updated_at = ? WHERE id = ?").bind(text, status, source, observedAt, session.id).run();
  return { session: { ...session, summary_text: text, summary_status: status, summary_source: source, summary_updated_at: observedAt }, summary };
}
