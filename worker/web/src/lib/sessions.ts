import type { Session, SessionDetail, SessionSummary } from "@/lib/api";

type SessionTitle = Pick<Session, "display_title" | "title" | "intent">;

export function displayTitle(session: SessionTitle): string {
  for (const value of [session.display_title, session.title, session.intent]) {
    if (value?.trim()) return value.trim();
  }
  return "Untitled session";
}

// Fixed sentences from buildSessionSummary (worker/src/sessions/summaries.ts)
// that only restate missing evidence. Keep them in sync with that builder.
const DEFAULT_UNRESOLVED_RESULT = "Recorded outcome: unresolved. No outcome reason was recorded.";
const ABSENCE_NOTES = [
  "No resolved work outcome has been recorded.",
  "No observed verification output is available in the inspected evidence.",
];

// The goal repeats the session intent shown in the header, so a summary is
// worth showing only when it records actions, verification, a specific open
// item, or a result other than the default unresolved sentence.
export function summaryHasEvidence(summary: SessionSummary | undefined): summary is SessionSummary {
  if (!summary) return false;
  return summary.actions.length > 0
    || summary.verification.length > 0
    || summary.unresolved.some((item) => !ABSENCE_NOTES.includes(item))
    || Boolean(summary.result && summary.result !== DEFAULT_UNRESOLVED_RESULT);
}

// An unresolved outcome with no reason and no history is the default state;
// the page offers a compact "Set outcome" control instead of a full section.
export function outcomeRecorded(detail: SessionDetail): boolean {
  return detail.session.outcome !== "unresolved" || Boolean(detail.session.outcome_reason?.trim()) || detail.outcome_events.length > 0;
}
