type SummaryInput = {
  intent: unknown;
  outcome: unknown;
  outcome_reason: unknown;
  request_count: unknown;
  file_count: number;
  error_count: number;
};

function sentence(value: unknown, limit = 360): string {
  if (typeof value !== "string") return "";
  const normalized = value.replace(/\s+/g, " ").trim().slice(0, limit);
  if (!normalized) return "";
  return /[.!?]$/.test(normalized) ? normalized : `${normalized}.`;
}

export function buildSessionSummary(input: SummaryInput): string | null {
  const parts: string[] = [];
  const intent = sentence(input.intent);
  if (intent) parts.push(`The session worked on: ${intent}`);
  const reason = sentence(input.outcome_reason);
  if (reason) parts.push(reason);
  else if (input.outcome === "landed") parts.push("The recorded work landed.");
  else if (input.outcome === "discarded") parts.push("The recorded work was discarded.");
  else if (input.outcome === "abandoned") parts.push("The session ended without a retained result.");
  else parts.push("No final work outcome was recorded.");
  const requests = Number(input.request_count) || 0;
  const facts = [`${requests} ${requests === 1 ? "request" : "requests"}`];
  if (input.file_count) facts.push(`${input.file_count} changed ${input.file_count === 1 ? "file" : "files"}`);
  if (input.error_count) facts.push(`${input.error_count} recurring ${input.error_count === 1 ? "error" : "errors"}`);
  if (facts.length) parts.push(`Mimir recorded ${facts.join(", ")}.`);
  return parts.join(" ") || null;
}

export async function ensureSessionSummary(db: D1Database, session: Record<string, unknown>, fileCount: number, errorCount: number) {
  const summaryAt = typeof session.summary_updated_at === "string" ? Date.parse(session.summary_updated_at) : 0;
  const evidenceAt = Math.max(...[session.outcome_updated_at, session.last_active_at, session.ended_at].map((value) => typeof value === "string" ? Date.parse(value) || 0 : 0));
  if (session.state !== "inactive" || (session.summary_status === "ready" && summaryAt >= evidenceAt)) return session;
  const summary = buildSessionSummary({ intent: session.intent, outcome: session.outcome, outcome_reason: session.outcome_reason, request_count: session.request_count, file_count: fileCount, error_count: errorCount });
  const now = new Date().toISOString();
  await db.prepare("UPDATE sessions SET summary_text = ?, summary_status = ?, summary_source = 'generated', summary_updated_at = ? WHERE id = ?")
    .bind(summary, summary ? "ready" : "unavailable", now, session.id).run();
  return { ...session, summary_text: summary, summary_status: summary ? "ready" : "unavailable", summary_source: "generated", summary_updated_at: now };
}
