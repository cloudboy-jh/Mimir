import { describe, expect, it } from "vitest";
import { buildSessionSummary } from "./session-summary";

describe("session summary", () => {
  it("creates a concise narrative from recorded facts", () => {
    expect(buildSessionSummary({ intent: "Fix request rendering", outcome: "landed", outcome_reason: "Structured output shipped", request_count: 7, file_count: 3, error_count: 1 }))
      .toBe("The session worked on: Fix request rendering. Structured output shipped. Mimir recorded 7 requests, 3 changed files, 1 recurring error.");
  });
});
