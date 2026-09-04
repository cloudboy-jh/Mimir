import { currentOutcomeEvidence, outcomeCommitEvidence, type SessionDetail, type SessionExchange } from "@/lib/api";
import { compactNumber, duration, shortDate } from "@/lib/format";
import { displayTitle } from "@/lib/sessions";

const EXPORT_LIMIT = 500;

function heading(text: string, level = 2): string {
  return `${"#".repeat(level)} ${text}`;
}

function oneLine(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function bullet(label: string, value: string): string {
  return `- **${label}:** ${oneLine(value)}`;
}

function inlineCode(value: string): string {
  const longestRun = Math.max(0, ...Array.from(value.matchAll(/`+/g), (match) => match[0].length));
  const fence = "`".repeat(longestRun + 1);
  return `${fence}${value}${fence}`;
}

function changeSummary(files: number, additions: number, deletions: number): string {
  const parts = [`${files} ${files === 1 ? "file" : "files"}`];
  if (additions) parts.push(`+${additions}`);
  if (deletions) parts.push(`-${deletions}`);
  return parts.join(", ");
}

// sessionMarkdown renders a transient export from live dashboard data. It
// contains metadata and excerpts only; raw redacted payloads stay in R2.
export function sessionMarkdown(detail: SessionDetail, exchanges: SessionExchange[], sourceURL?: string): string {
  const session = detail.session;
  const models = session.models?.length ? session.models.map((model) => model.name) : session.model_primary ? [session.model_primary] : [];
  const lines: string[] = [];
  lines.push(`# ${oneLine(displayTitle(session))}`, "");
  lines.push(heading("Summary"), "");
  lines.push(session.summary_text?.trim() || session.intent?.trim() || "No session summary is available.", "");
  lines.push(heading("Work outcome"));
  lines.push(bullet("Outcome", session.outcome));
  if (session.outcome_reason) lines.push(bullet("Reason", session.outcome_reason));
  if (session.outcome_src) lines.push(bullet("Source", session.outcome_src));
  lines.push("");
  lines.push(heading("Session"));
  lines.push(
    bullet("ID", inlineCode(session.id)),
    bullet("Repository", session.repo || "None"),
    bullet("Reference", session.source_ref || "None"),
    bullet("App", session.harness || "Unknown"),
    bullet(models.length === 1 ? "Model" : "Models", models.length ? models.join(", ") : "Unknown"),
    bullet("Started", shortDate(session.started_at)),
    bullet("Duration", duration(session.started_at, session.ended_at)),
    bullet("Requests", String(session.request_count)),
    bullet("Tokens", `${compactNumber(session.tokens_in)} in · ${compactNumber(session.tokens_out)} out${(session.cache_read_tokens ?? 0) > 0 ? ` · ${compactNumber(session.cache_read_tokens ?? 0)} cache read` : ""}${(session.cache_write_tokens ?? 0) > 0 ? ` · ${compactNumber(session.cache_write_tokens ?? 0)} cache write` : ""}`),
    "",
  );
  lines.push(heading("Capture"));
  lines.push(bullet("Status", detail.capture.status), bullet("Saved", String(detail.capture.saved_exchanges)), bullet("Failed", String(detail.capture.failed_exchanges)), bullet("Pending", String(detail.capture.pending_exchanges)), "");
  if (detail.outcome_events.length) {
    lines.push(heading("Outcome history", 3));
    for (const event of detail.outcome_events) lines.push(`- ${oneLine(event.outcome)} · ${oneLine(event.source)} · ${shortDate(event.created_at)}${event.reason ? `: ${oneLine(event.reason)}` : ""}`);
    lines.push("");
  }
  lines.push(heading("Changes"));
  const recordedCommits = new Set<string>();
  for (const artifact of detail.git_artifacts) {
    const subject = artifact.subject ? ` ${oneLine(artifact.subject)}` : "";
    lines.push(`- ${inlineCode(artifact.commit_sha.slice(0, 12))}${subject} (${changeSummary(artifact.patch_files, artifact.patch_additions, artifact.patch_deletions)})`);
    recordedCommits.add(artifact.commit_sha.toLowerCase());
  }
  for (const entry of outcomeCommitEvidence(detail.outcome_events)) {
    const commit = entry.evidence.commit!;
    if (recordedCommits.has(commit.toLowerCase())) continue;
    const stats = changeSummary(entry.evidence.patch_files ?? 0, entry.evidence.patch_additions ?? 0, entry.evidence.patch_deletions ?? 0);
    lines.push(`- ${inlineCode(commit.slice(0, 12))}${entry.evidence.note ? ` ${oneLine(entry.evidence.note)}` : entry.event.reason ? ` ${oneLine(entry.event.reason)}` : ""} (${stats})`);
    recordedCommits.add(commit.toLowerCase());
  }
  const outcomeEvidence = currentOutcomeEvidence(detail.outcome_events, session.outcome);
  if (outcomeEvidence?.patch && !detail.git_artifacts.length) {
    lines.push(`- ${oneLine(outcomeEvidence.patch)}`);
  }
  if (!recordedCommits.size && !outcomeEvidence?.patch) lines.push("No Git changes were captured.");
  lines.push("");
  lines.push(heading("Files"));
  if (detail.files.length) for (const file of detail.files) lines.push(`- ${inlineCode(file)}`);
  else lines.push("No files detected.");
  lines.push("");
  lines.push(heading("Errors"));
  if (detail.errors.length) for (const error of detail.errors) lines.push(`- ${oneLine(error.signature)} (${error.count}×${error.last_seen_at ? `, last seen ${shortDate(error.last_seen_at)}` : ""})`);
  else lines.push("No errors detected.");
  lines.push("");
  lines.push(heading("Request evidence"));
  if (exchanges.length >= EXPORT_LIMIT) lines.push(`_Truncated at ${EXPORT_LIMIT} exchanges._`, "");
  if (!exchanges.length) lines.push("No request evidence was captured.");
  for (const exchange of exchanges) {
    lines.push(`### ${shortDate(exchange.ts)} · ${oneLine(exchange.model)}`);
    lines.push(`- **Provider:** ${oneLine(exchange.provider || "Unknown")}`);
    if (exchange.finish_reason) lines.push(`- **Finish:** ${oneLine(exchange.finish_reason)}`);
    lines.push(`- **Tokens:** ${exchange.input_tokens} in · ${exchange.output_tokens} out${(exchange.cache_read_tokens ?? 0) > 0 ? ` · ${exchange.cache_read_tokens} cache read` : ""}${(exchange.cache_write_tokens ?? 0) > 0 ? ` · ${exchange.cache_write_tokens} cache write` : ""}`);
    if (exchange.request_excerpt) lines.push("", `> ${oneLine(exchange.request_excerpt)}`);
    lines.push("");
  }
  if (sourceURL) lines.push(`[View this session in Mimir](${sourceURL})`, "");
  lines.push("---", "_Exported from Mimir. Contains excerpts and metadata, not raw request payloads._");
  return lines.join("\n");
}

export function markdownExportLimit(): number {
  return EXPORT_LIMIT;
}
