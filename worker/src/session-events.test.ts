import { describe, expect, it } from "vitest";
import { parseSessionEvent } from "./session-events";

const base = { version: 1, session_id: "session-1", ts: "2026-07-22T12:00:00.000Z" };

describe("parseSessionEvent", () => {
  it("accepts turn, heartbeat, and end events", () => {
    expect(parseSessionEvent({ ...base, kind: "turn", turn: { model: "openai/test" } })).toMatchObject({ kind: "turn", turn: { model: "openai/test" } });
    expect(parseSessionEvent({ ...base, kind: "heartbeat" })).toMatchObject({ kind: "heartbeat", harness: null });
    expect(parseSessionEvent({ ...base, kind: "end" })).toMatchObject({ kind: "end", reason: "explicit" });
  });

  it("rejects malformed envelopes", () => {
    expect(parseSessionEvent(null)).toEqual({ error: "event must be an object" });
    expect(parseSessionEvent([1])).toEqual({ error: "event must be an object" });
    expect(parseSessionEvent({ ...base, version: 2, kind: "turn" })).toEqual({ error: "unsupported event version" });
    expect(parseSessionEvent({ ...base, kind: "note" })).toEqual({ error: "invalid event kind" });
    expect(parseSessionEvent({ ...base, kind: "turn", session_id: "bad id!" })).toEqual({ error: "invalid session_id" });
    expect(parseSessionEvent({ ...base, kind: "turn", ts: "not-a-date" })).toEqual({ error: "invalid ts" });
  });

  it("validates turn fields and caps the excerpt", () => {
    expect(parseSessionEvent({ ...base, kind: "turn", turn: { usage: { input_tokens: -1, output_tokens: 0 } } })).toEqual({ error: "invalid turn usage" });
    expect(parseSessionEvent({ ...base, kind: "turn", turn: { request_kind: "fancy" } })).toEqual({ error: "invalid turn request_kind" });
    expect(parseSessionEvent({ ...base, kind: "turn", turn: { latency_ms: -5 } })).toEqual({ error: "invalid turn latency_ms" });
    const event = parseSessionEvent({ ...base, kind: "turn", turn: { exchange_id: "ex-1", provider: "OpenAI", usage: { input_tokens: 5.7, output_tokens: 3 }, latency_ms: 12.9, excerpt: "x".repeat(900) } });
    expect(event).toMatchObject({ turn: { exchange_id: "ex-1", provider: "OpenAI", usage: { input_tokens: 5, output_tokens: 3 }, latency_ms: 12 } });
    expect((event as { turn: { excerpt: string } }).turn.excerpt).toHaveLength(500);
  });

  it("normalizes timestamps and defaults the end reason", () => {
    const event = parseSessionEvent({ version: 1, kind: "end", session_id: "s", ts: "2026-07-22T12:00:00Z", reason: undefined });
    expect(event).toMatchObject({ ts: "2026-07-22T12:00:00.000Z", reason: "explicit" });
    expect(parseSessionEvent({ ...base, kind: "end", reason: "user closed" })).toMatchObject({ reason: "user closed" });
  });
});
