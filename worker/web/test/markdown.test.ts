import { describe, expect, it } from "vitest";

import type { SessionDetail, SessionExchange } from "../src/lib/api";
import { sessionMarkdown } from "../src/lib/markdown";

function detail(summary: string | null, intent = "Fallback intent"): SessionDetail {
  return {
    session: {
      id: "ses_example",
      display_title: "Repair dashboard export",
      title: "Repair dashboard export",
      intent,
      summary_text: summary,
      repo: "mimir",
      source_ref: "feature/session-notes",
      harness: "oh-my-pi",
      models: [{ name: "openai/gpt-5", request_count: 2, first_seen_at: null, last_seen_at: null }],
      model_primary: "openai/gpt-5",
      started_at: "2026-08-27T10:00:00Z",
      ended_at: "2026-08-27T10:12:00Z",
      request_count: 2,
      tokens_in: 1200,
      tokens_out: 300,
      outcome: "landed",
      outcome_reason: "Implemented and verified the session-note flow.",
      outcome_src: "agent",
    },
    capture: { status: "saved", saved_exchanges: 2, failed_exchanges: 0, pending_exchanges: 0 },
    supporting_sessions: [],
    outcome_events: [],
    files: ["worker/web/src/lib/session-notes.ts"],
    errors: [{ signature: "Permission denied\nfor vault", count: 1, first_seen_at: null, last_seen_at: "2026-08-27T10:05:00Z", latest_exchange_id: "ex_1" }],
    git_artifacts: [{
      commit_sha: "1234567890abcdef",
      parent_commit_sha: null,
      committed_at: "2026-08-27T10:11:00Z",
      subject: "Add session notes",
      repository_url: null,
      ref: "feature/session-notes",
      provenance: "agent",
      patch_r2_key: "patch/example",
      patch_sha256: "abc",
      patch_bytes: 100,
      patch_files: 2,
      patch_additions: 30,
      patch_deletions: 4,
      capture_status: "saved",
      accepted_at: "2026-08-27T10:11:00Z",
      saved_at: "2026-08-27T10:11:01Z",
      failed_at: null,
      failure_code: null,
      created_at: "2026-08-27T10:11:00Z",
    }],
  } as unknown as SessionDetail;
}

const exchanges: SessionExchange[] = [{
  id: "ex_1",
  session_id: "ses_example",
  ts: "2026-08-27T10:01:00Z",
  model: "openai/gpt-5",
  provider: "openrouter",
  finish_reason: "stop",
  latency_ms: 500,
  harness: "oh-my-pi",
  input_tokens: 100,
  output_tokens: 25,
  request_excerpt: "Inspect the settings page.",
  capture_status: "saved",
  capture_reason: null,
  failure_code: null,
}];

describe("sessionMarkdown", () => {
  it("leads with readable summary and outcome before supporting evidence", () => {
    const markdown = sessionMarkdown(detail("Added a direct Obsidian session-note workflow."), exchanges, "https://mimir.example/dashboard/sessions/ses_example");

    expect(markdown).toContain("## Summary\n\nAdded a direct Obsidian session-note workflow.");
    expect(markdown.indexOf("## Summary")).toBeLessThan(markdown.indexOf("## Work outcome"));
    expect(markdown.indexOf("## Work outcome")).toBeLessThan(markdown.indexOf("## Changes"));
    expect(markdown.indexOf("## Changes")).toBeLessThan(markdown.indexOf("## Request evidence"));
    expect(markdown).toContain("`1234567890ab` Add session notes (2 files, +30, -4)");
    expect(markdown).toContain("Permission denied for vault");
    expect(markdown).toContain("[View this session in Mimir](https://mimir.example/dashboard/sessions/ses_example)");
  });

  it("includes Git evidence recorded with the work outcome", () => {
    const value = detail("Captured the landed change.");
    value.git_artifacts = [];
    value.outcome_events = [{
      id: "outcome_1",
      outcome: "landed",
      source: "agent",
      reason: "Implemented the integration.",
      evidence_json: JSON.stringify({ commit: "abcdef1234567890", note: "Add Obsidian notes", patch_files: 4, patch_additions: 11, patch_deletions: 2 }),
      created_at: "2026-08-27T10:11:00Z",
    }];

    expect(sessionMarkdown(value, [])).toContain("`abcdef123456` Add Obsidian notes (4 files, +11, -2)");
  });

  it("includes commits from earlier visits without duplicating artifacts", () => {
    const value = detail("Captured work across visits.");
    value.outcome_events = [
      {
        id: "outcome_2",
        outcome: "landed",
        source: "agent",
        reason: "Second visit.",
        evidence_json: JSON.stringify({ commit: "b".repeat(40) }),
        created_at: "2026-08-29T10:11:00Z",
      },
      {
        id: "outcome_1",
        outcome: "landed",
        source: "agent",
        reason: "First visit.",
        evidence_json: JSON.stringify({ commit: "a".repeat(40) }),
        created_at: "2026-08-27T10:11:00Z",
      },
    ];

    const markdown = sessionMarkdown(value, []);
    expect(markdown).toContain(`\`${"a".repeat(12)}\` First visit.`);
    expect(markdown).toContain(`\`${"b".repeat(12)}\` Second visit.`);
  });

  it("falls back to intent when no summary is available", () => {
    expect(sessionMarkdown(detail(null, "Connect Mimir to Obsidian."), [])).toContain("## Summary\n\nConnect Mimir to Obsidian.");
  });
});
