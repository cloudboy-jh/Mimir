import { exportJWK, generateKeyPair, SignJWT } from "jose";
import { describe, expect, it, vi } from "vitest";
import {
  addMachineToken,
  createExecutionContext,
  dashboardRequest,
  env,
  finalizeAcceptedExchange,
  request,
  tokenHash,
  waitOnExecutionContext,
  worker,
} from "./support";

describe("Search integration", () => {
  it("filters search matches by requested types", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, boundary, intent) VALUES ('typed-session', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', 'header', 'zebra intent only')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at, request_excerpt, response_excerpt) VALUES ('typed-exchange', 'typed-session', '2026-01-01T00:00:00Z', 'chat', 1, 'log/typed.json', 'saved', '2026-01-01T00:00:01Z', 'plain request', 'plain response')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO session_files(session_id, file) VALUES ('typed-session', 'src/zebra.ts')",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const search = (body: unknown) =>
      request("/search", {
        method: "POST",
        headers,
        body: JSON.stringify(body),
      });
    const files = (await (
      await search({ query: "zebra", types: ["files"] })
    ).json()) as { matches: unknown[] };
    expect(files.matches).toHaveLength(1);
    const excerpts = (await (
      await search({ query: "zebra", types: ["excerpts"] })
    ).json()) as { matches: unknown[] };
    expect(excerpts.matches).toHaveLength(0);
    const intent = (await (
      await search({ query: "zebra", types: ["intent"] })
    ).json()) as { matches: unknown[] };
    expect(intent.matches).toHaveLength(1);
    const invalid = await search({ query: "zebra", types: ["bogus"] });
    expect(invalid.status).toBe(400);
  });

  it("searches queries beyond D1's LIKE pattern limit", async () => {
    const query = "a".repeat(49);
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, boundary, intent) VALUES ('long-query-session', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', 'header', ?)",
    )
      .bind(`prefix ${query} suffix`)
      .run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at) VALUES ('long-query-exchange', 'long-query-session', '2026-01-01T00:00:00Z', 'chat', 1, 'log/long-query.json', 'saved', '2026-01-01T00:00:01Z')",
    ).run();
    const response = await request("/search", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
      },
      body: JSON.stringify({ query, types: ["intent"] }),
    });
    expect(response.status).toBe(200);
    expect(
      (
        (await response.json()) as { matches: Array<{ session_id: string }> }
      ).matches.map((match) => match.session_id),
    ).toContain("long-query-session");
  });
});
