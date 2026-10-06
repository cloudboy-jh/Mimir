import { describe, expect, it } from "vitest";
import { ensureSessionSummary } from "../src/sessions/summaries";
import { captureTreeSummary } from "../src/sessions/capture-status";
import { loadSessionRecord, loadSupportingSessions } from "../src/sessions/session-queries";
import { env } from "./support";

async function reconstruct() {
  const observedAt = new Date().toISOString();
  const session = (await loadSessionRecord(env.DB, "summary-root"))!;
  return ensureSessionSummary(env.DB, env.LOGS, session, {
    capture: await captureTreeSummary(env.DB, "summary-root"),
    children: await loadSupportingSessions(env.DB, "summary-root"),
    errors: [], gitArtifacts: [], outcomeEvents: [],
  }, observedAt);
}

describe("summary evidence freshness", () => {
  it("refreshes after late child saves, capture failures, and recorded outcome changes without newer activity", async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, last_active_at, state, boundary, intent, summary_text, summary_status, summary_source, summary_updated_at)
      VALUES ('summary-root', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z', 'inactive', 'header', 'Investigate rendering', 'Old optimistic result', 'ready', 'generated', '2099-01-01T00:00:00.000Z');
      INSERT INTO sessions(id, parent_session_id, started_at, last_active_at, state, boundary)
      VALUES ('summary-child', 'summary-root', '2026-01-01T00:00:00.000Z', '2026-01-01T00:00:00.000Z', 'inactive', 'header');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, request_kind)
      VALUES ('summary-check', 'summary-child', '2026-01-01T00:00:00.000Z', 'harness', 0, 'summary-check.json', 'accepted', 'primary');
    `.replace(/\n/g, " "));
    const pending = await reconstruct();
    expect(pending.summary.partial).toBe(true);
    expect(pending.summary.verification).toEqual([]);
    expect(pending.session.summary_text).not.toContain("Old optimistic result");

    await env.LOGS.put("summary-check.json", JSON.stringify({
      request: {}, tool_activity: [{ name: "bash", input: { command: "npm test" }, status: "succeeded", output: "3 tests passed" }],
    }));
    await env.DB.prepare("UPDATE exchanges SET capture_status = 'saved', saved_at = ? WHERE id = ?").bind("2026-02-01T00:00:00.000Z", "summary-check").run();
    const saved = await reconstruct();
    expect(saved.summary.partial).toBe(false);
    expect(saved.summary.verification[0]).toContain("3 tests passed");
    expect(saved.summary.result).toContain("unresolved");
    expect(saved.session.summary_source).not.toBe(pending.session.summary_source);
    const unchanged = await reconstruct();
    expect(unchanged.session.summary_updated_at).toBe(saved.session.summary_updated_at);

    await env.DB.exec("UPDATE sessions SET work_outcome = 'discarded', outcome_reason = 'Rejected after review' WHERE id = 'summary-root'");
    const discarded = await reconstruct();
    expect(discarded.summary.result).toContain("discarded");
    expect(discarded.summary.result).toContain("Rejected after review");
    expect(discarded.session.summary_source).not.toBe(saved.session.summary_source);

    await env.DB.exec("UPDATE exchanges SET capture_status = 'failed', saved_at = NULL WHERE id = 'summary-check'");
    const failed = await reconstruct();
    expect(failed.summary.partial).toBe(true);
    expect(failed.summary.verification).toEqual([]);
    expect(failed.summary.result).toContain("discarded");
    expect(failed.session.summary_source).not.toBe(discarded.session.summary_source);
  });

  it("makes missing saved archives inspectable and excludes auxiliary responses from action reconstruction", async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, state, boundary) VALUES ('summary-root', '2026-01-01T00:00:00.000Z', 'inactive', 'header');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at, request_kind, response_excerpt)
      VALUES ('missing-primary', 'summary-root', '2026-01-01T00:00:00.000Z', 'harness', 0, 'missing.json', 'saved', '2026-02-01T00:00:00.000Z', 'primary', 'Primary recorded response');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at, request_kind, response_excerpt)
      VALUES ('auxiliary', 'summary-root', '2026-01-01T00:00:00.000Z', 'harness', 0, 'auxiliary.json', 'saved', '2026-02-01T00:00:00.000Z', 'summary', 'Unsupported claim that work shipped');
    `.replace(/\n/g, " "));
    const result = await reconstruct();
    expect(result.summary.partial).toBe(true);
    expect(result.summary.actions[0]).toContain("Primary recorded response");
    expect(result.summary.actions.join(" ")).not.toContain("Unsupported claim");
    expect(result.summary.unresolved.some(value => value.includes("not be inspected"))).toBe(true);
  });

  it("pairs Anthropic results with their calls without promoting output into a landed outcome", async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, state, boundary) VALUES ('summary-root', '2026-01-01T00:00:00.000Z', 'inactive', 'header');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at, request_kind)
      VALUES ('anthropic-check', 'summary-root', '2026-01-01T00:00:00.000Z', '/v1/messages', 0, 'anthropic-check.json', 'saved', '2026-02-01T00:00:00.000Z', 'primary');
    `.replace(/\n/g, " "));
    await env.LOGS.put("anthropic-check.json", JSON.stringify({ request: { messages: [
      { role: "assistant", content: [{ type: "tool_use", id: "check-1", name: "bash", input: { command: "make verify" } }] },
      { role: "user", content: [{ type: "tool_result", tool_use_id: "check-1", is_error: true, content: [{ type: "text", text: "Compilation failed at src/main.ts:8" }] }] },
    ] } }));
    const result = await reconstruct();
    expect(result.summary.actions.join(" ")).toContain("make verify");
    expect(result.summary.verification.join(" ")).toContain("Compilation failed at src/main.ts:8");
    expect(result.summary.result).toContain("unresolved");
  });
});
