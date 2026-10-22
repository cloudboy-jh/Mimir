import { describe, expect, it } from "vitest";

import type { SessionDetail, SessionSummary } from "../src/lib/api";
import { outcomeRecorded, summaryHasEvidence } from "../src/lib/sessions";

const boilerplate: SessionSummary = {
  goal: "Fix the dashboard",
  actions: [],
  result: "Recorded outcome: unresolved. No outcome reason was recorded.",
  verification: [],
  unresolved: ["No resolved work outcome has been recorded.", "No observed verification output is available in the inspected evidence."],
  partial: false,
  source: "reconstructed",
  evidence: [],
};

describe("summaryHasEvidence", () => {
  it("rejects missing summaries and goal-only boilerplate", () => {
    expect(summaryHasEvidence(undefined)).toBe(false);
    expect(summaryHasEvidence(boilerplate)).toBe(false);
  });

  it.each([
    { actions: ["Recorded tool bash: npm test."] },
    { verification: ["Observed bash output: 3 passed"] },
    { unresolved: [...boilerplate.unresolved, "This session or a supporting session is still active; the reconstruction is provisional."] },
    { result: "Recorded outcome: landed. Shipped." },
    { result: "Recorded outcome: unresolved. Waiting on review." },
  ])("accepts summaries with recorded evidence %#", (change) => {
    expect(summaryHasEvidence({ ...boilerplate, ...change })).toBe(true);
  });
});


describe("outcomeRecorded", () => {
  const detail = (session: Partial<SessionDetail["session"]>, events = 0) => ({
    session: { outcome: "unresolved", outcome_reason: null, ...session },
    outcome_events: Array.from({ length: events }, (_, index) => ({ id: `out_${index}` })),
  }) as unknown as SessionDetail;

  it("treats an unresolved outcome without reason or history as unrecorded", () => {
    expect(outcomeRecorded(detail({}))).toBe(false);
    expect(outcomeRecorded(detail({ outcome_reason: "  " }))).toBe(false);
  });

  it("treats a decided outcome, a reason, or outcome history as recorded", () => {
    expect(outcomeRecorded(detail({ outcome: "landed" }))).toBe(true);
    expect(outcomeRecorded(detail({ outcome_reason: "Waiting on review." }))).toBe(true);
    expect(outcomeRecorded(detail({}, 1))).toBe(true);
  });
});