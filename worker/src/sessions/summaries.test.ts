import { describe, expect, it } from "vitest";
import { buildSessionSummary, summaryText, type SummaryEvidence } from "./summaries";

const saved: SummaryEvidence = {
  capture: { status: "saved", saved_exchanges: 1, pending_exchanges: 0, failed_exchanges: 0, last_saved_at: "2026-10-01T00:00:00.000Z" },
  children: [], errors: [], gitArtifacts: [], outcomeEvents: [],
};
const session = { intent: "Fix request rendering", outcome: "unresolved", state: "inactive" };

describe("factual session reconstruction", () => {
  it("does not promote command invocation, read paths, or saved patches into a successful result", () => {
    const summary = buildSessionSummary(session, {
      ...saved,
      observations: [
        { exchange_id: "read", side: "response", name: "read", input: { path: "src/request.ts" }, status: "succeeded", output: "export const request = 1" },
        { exchange_id: "check", side: "response", name: "bash", input: { command: "npm test" }, status: "succeeded", output: null },
      ],
      gitArtifacts: [{ commit_sha: "a".repeat(40), subject: "Render request", capture_status: "saved", patch_files: 2 }],
      errors: [{ signature: "TypeError: missing value", count: 1, latest_exchange_id: "error" }],
    });
    expect(summary.result).toContain("unresolved");
    expect(summary.verification).toEqual([]);
    expect(summary.unresolved.some(value => value.includes("resolution not established"))).toBe(true);
  });

  it("keeps discarded work discarded despite observed successful checks and commit capture", () => {
    const summary = buildSessionSummary({ ...session, outcome: "discarded", outcome_reason: "Replaced by another approach" }, {
      ...saved,
      observations: [{ exchange_id: "check", side: "response", name: "bash", input: { command: "npm test" }, status: "succeeded", output: "3 tests passed" }],
      gitArtifacts: [{ commit_sha: "b".repeat(40), subject: null, capture_status: "saved", patch_files: 1 }],
    });
    expect(summary.result).toContain("discarded");
    expect(summary.result).toContain("Replaced by another approach");
    expect(summary.verification[0]).toContain("3 tests passed");
    expect(summaryText(summary)).toContain("npm test");
  });

  it.each([
    { state: "active", children: [], capture: saved.capture },
    { state: "inactive", children: [{ state: "active" }], capture: saved.capture },
    { state: "inactive", children: [], capture: { ...saved.capture, status: "partial" as const, failed_exchanges: 1 } },
  ])("marks live and incomplete capture evidence provisional", ({ state, children, capture }) => {
    const summary = buildSessionSummary({ ...session, state }, { ...saved, children, capture });
    expect(summary.partial).toBe(true);
  });

  it("bounds excerpts and reports unavailable or sampled evidence rather than filling the gaps", () => {
    const summary = buildSessionSummary({ ...session, intent: "x".repeat(800) }, {
      ...saved, sampled: true, unavailableArchives: 1,
      observations: [{ exchange_id: "bounded", side: "response", name: "bash", input: { command: "npm test" }, status: "failed", output: "failure ".repeat(1000) }],
    });
    expect(summary.goal!.length).toBeLessThanOrEqual(361);
    expect(summary.verification[0].length).toBeLessThan(300);
    expect(summary.partial).toBe(true);
    expect(summary.unresolved.some(value => value.includes("not be inspected"))).toBe(true);
    expect(summary.unresolved.some(value => value.includes("bounded"))).toBe(true);
  });
});
