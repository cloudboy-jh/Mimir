import { describe, expect, it } from "vitest";
import { summarizeSession, type SummaryInput } from "./summaries";

const base: SummaryInput = {
  intent: "Fix request rendering",
  goalExchangeID: "first",
  active: false,
  capture: { status: "saved", saved_exchanges: 2, pending_exchanges: 0, failed_exchanges: 0, last_saved_at: "2026-10-01T00:00:00.000Z" },
  errors: [],
  commits: [],
  archives: [],
  sampled: false,
};

function archive(id: string, response: unknown) {
  return { exchange_id: id, session_id: "session", envelope: { exchange_id: id, request: { messages: [] }, response: { format: "json", body: response } } };
}

describe("session summaries", () => {
  it("cites tool actions, observed output and the final response by exchange", () => {
    const latest = archive("latest", {
      message: { role: "assistant", content: "Rendering is fixed." },
    });
    const working = archive("working", {
      message: { role: "assistant", content: [{ type: "tool_use", id: "call-1", name: "bash", input: { command: "npm test" } }] },
      tool_results: [{ tool_call_id: "call-1", name: "bash", content: "3 tests passed" }],
    });
    const summary = summarizeSession({ ...base, archives: [latest, working] });
    expect(summary.goal).toBe("Fix request rendering");
    expect(summary.actions).toEqual(["Ran bash: npm test."]);
    expect(summary.verification).toEqual(["Observed bash output: 3 tests passed"]);
    expect(summary.result).toBe("Rendering is fixed.");
    expect(summary.evidence).toEqual([
      { section: "goal", index: 0, exchange_id: "first" },
      { section: "actions", index: 0, exchange_id: "working" },
      { section: "verification", index: 0, exchange_id: "working" },
      { section: "result", index: 0, exchange_id: "latest" },
    ]);
    expect(summary.partial).toBe(false);
  });

  it("reports commits and unresolved errors without claiming verification", () => {
    const summary = summarizeSession({
      ...base,
      commits: [{ commit_sha: "a".repeat(40), subject: "Render request", capture_status: "saved", patch_files: 2, pushed: 1 }],
      errors: [{ signature: "TypeError: missing value", count: 1, exchange_id: "error" }],
    });
    expect(summary.actions).toEqual(["Committed aaaaaaaa: Render request (2 files, pushed)."]);
    expect(summary.result).toBe("1 commit recorded, 1 pushed.");
    expect(summary.unresolved).toContain("No verification output was observed.");
    expect(summary.evidence).toContainEqual({ section: "unresolved", index: 0, exchange_id: "error" });
  });

  it("marks active, incomplete, sampled or unreadable evidence partial", () => {
    for (const input of [
      { active: true },
      { capture: { ...base.capture, status: "partial" as const, failed_exchanges: 1 } },
      { sampled: true },
      { archives: [{ exchange_id: "gone", session_id: "session", envelope: null }] },
    ])
      expect(summarizeSession({ ...base, ...input }).partial).toBe(true);
  });

  it("bounds excerpts", () => {
    const summary = summarizeSession({ ...base, intent: "x".repeat(800), archives: [archive("long", { message: { role: "assistant", content: "y".repeat(2000) } })] });
    expect(summary.goal!.length).toBeLessThanOrEqual(361);
    expect(summary.result!.length).toBeLessThanOrEqual(361);
  });
});
