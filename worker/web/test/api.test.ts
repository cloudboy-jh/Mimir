import { describe, expect, it } from "vitest";
import { currentOutcomeEvidence, getSessionGitArtifactPatch, outcomeCommitEvidence, type OutcomeEvent } from "../src/lib/api";

function event(id: string, outcome: OutcomeEvent["outcome"], evidence: unknown): OutcomeEvent {
  return {
    id,
    outcome,
    source: "agent",
    reason: null,
    evidence_json: evidence === null ? null : JSON.stringify(evidence),
    created_at: `2026-08-04T00:00:0${id}Z`,
  };
}

describe("currentOutcomeEvidence", () => {
  const git = {
    commit: "a".repeat(40),
    repository_url: "https://github.com/owner/repo",
    patch: "diff --git a/a.ts b/a.ts\n+added\n",
  };

  it("retains commit and diff evidence when a same-outcome update only adds a note", () => {
    expect(currentOutcomeEvidence([
      event("2", "landed", { note: "release published" }),
      event("1", "landed", git),
    ], "landed")).toEqual({ ...git, note: "release published" });
  });

  it("does not reuse a commit from a different outcome", () => {
    expect(currentOutcomeEvidence([
      event("2", "discarded", { note: "reverted" }),
      event("1", "landed", git),
    ], "discarded")).toEqual({ note: "reverted" });
  });

  it("treats an explicit URL as replacement evidence", () => {
    expect(currentOutcomeEvidence([
      event("2", "landed", { url: "https://example.com/pr/1" }),
      event("1", "landed", git),
    ], "landed")).toEqual({ url: "https://example.com/pr/1" });
  });

  it("respects a user clearing commit evidence", () => {
    const cleared = event("2", "landed", null);
    cleared.source = "user";
    expect(currentOutcomeEvidence([cleared, event("1", "landed", git)], "landed")).toBeNull();
  });
});

describe("outcomeCommitEvidence", () => {
  it("retains every distinct commit across revisited outcome events", () => {
    const first = event("1", "landed", { commit: "a".repeat(40) });
    const revisit = event("2", "landed", { commit: "b".repeat(40) });
    const duplicate = event("3", "landed", { commit: "A".repeat(40) });
    expect(
      outcomeCommitEvidence([duplicate, revisit, first]).map(
        (entry) => entry.evidence.commit,
      ),
    ).toEqual(["A".repeat(40), "b".repeat(40)]);
  });
});

describe("getSessionGitArtifactPatch", () => {
  it("retrieves the patch for the exact session and commit", async () => {
    const originalFetch = globalThis.fetch;
    let requested: string | URL | Request = "";
    globalThis.fetch = (async (input: string | URL | Request) => {
      requested = input;
      return new Response("diff --git a/a.ts b/a.ts\n");
    }) as typeof fetch;
    try {
      const commit = "a".repeat(40);
      await expect(getSessionGitArtifactPatch("session/one", commit)).resolves.toContain("diff --git");
      expect(String(requested)).toBe(`/dashboard/api/sessions/session%2Fone/git-artifacts/${commit}/patch`);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });
});
