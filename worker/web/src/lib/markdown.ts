import type { SessionDetail, SessionExchange } from "@/lib/api";
import { compactNumber, duration, shortDate } from "@/lib/format";

const EXPORT_LIMIT = 500;

function heading(text: string, level = 2): string {
  return `${"#".repeat(level)} ${text}`;
}

function bullet(label: string, value: string): string {
  return `- **${label}:** ${value}`;
}

// sessionMarkdown renders a transient export from live dashboard data. It
// contains metadata and excerpts only; raw redacted payloads stay in R2.
export function sessionMarkdown(detail: SessionDetail, exchanges: SessionExchange[]): string {
  const session = detail.session;
  const lines: string[] = [];
  lines.push(`# ${session.intent || "Untitled session"}`, "");
  lines.push(heading("Session"));
  lines.push(
    bullet("ID", `\`${session.id}\``),
    bullet("Repository", session.repo || "None"),
    bullet("Reference", session.source_ref || "None"),
    bullet("App", session.harness || "Unknown"),
    bullet("Model", session.model_primary || "Unknown"),
    bullet("Started", shortDate(session.started_at)),
    bullet("Duration", duration(session.started_at, session.ended_at)),
    bullet("Requests", String(session.request_count)),
    bullet("Tokens", `${compactNumber(session.tokens_in)} in · ${compactNumber(session.tokens_out)} out`),
    "",
  );
  lines.push(heading("Capture"));
  lines.push(bullet("Status", detail.capture.status), bullet("Saved", String(detail.capture.saved_exchanges)), bullet("Failed", String(detail.capture.failed_exchanges)), bullet("Pending", String(detail.capture.pending_exchanges)), "");
  lines.push(heading("Work outcome"));
  lines.push(bullet("Outcome", session.outcome));
  if (session.outcome_reason) lines.push(bullet("Reason", session.outcome_reason));
  if (session.outcome_src) lines.push(bullet("Source", session.outcome_src));
  lines.push("");
  if (detail.outcome_events.length) {
    lines.push(heading("Outcome history", 3));
    for (const event of detail.outcome_events) lines.push(`- ${event.outcome} · ${event.source} · ${shortDate(event.created_at)}${event.reason ? ` — ${event.reason}` : ""}`);
    lines.push("");
  }
  lines.push(heading("Files"));
  if (detail.files.length) for (const file of detail.files) lines.push(`- \`${file}\``);
  else lines.push("No files detected.");
  lines.push("");
  lines.push(heading("Errors"));
  if (detail.errors.length) for (const error of detail.errors) lines.push(`- ${error.signature} (${error.count}×${error.last_seen_at ? `, last seen ${shortDate(error.last_seen_at)}` : ""})`);
  else lines.push("No errors detected.");
  lines.push("");
  lines.push(heading("Request timeline"));
  if (exchanges.length >= EXPORT_LIMIT) lines.push(`_Truncated at ${EXPORT_LIMIT} exchanges._`, "");
  if (!exchanges.length) lines.push("No request evidence was captured.");
  for (const exchange of exchanges) {
    lines.push(`### ${shortDate(exchange.ts)} · ${exchange.model}`);
    lines.push(`- **Provider:** ${exchange.provider || "Unknown"}`);
    if (exchange.finish_reason) lines.push(`- **Finish:** ${exchange.finish_reason}`);
    lines.push(`- **Tokens:** ${exchange.input_tokens} in · ${exchange.output_tokens} out`);
    if (exchange.request_excerpt) lines.push("", `> ${exchange.request_excerpt.replaceAll("\n", " ")}`);
    lines.push("");
  }
  lines.push("---", "_Exported from Mimir. Contains excerpts and metadata, not raw request payloads._");
  return lines.join("\n");
}

export function markdownExportLimit(): number {
  return EXPORT_LIMIT;
}
