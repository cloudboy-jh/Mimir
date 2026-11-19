import { describe, expect, it } from "vitest";
import { addMachineToken, api, dashboardRequest, env, request } from "./support";

type Json = Record<string, any>;

async function seed() {
  await addMachineToken("install-a", "machine-a");
  await env.DB.exec(`
    UPDATE machines SET name = 'Desk', platform = 'linux', arch = 'amd64' WHERE installation_id = 'install-a';
    INSERT INTO sessions(id, installation_id, started_at, last_active_at, state, boundary, harness, repo, model_primary, intent, title, title_source) VALUES ('root-a', 'install-a', '2026-09-01T09:00:00.000Z', '2026-09-01T09:30:00.000Z', 'inactive', 'header', 'claude-code', 'mimir', 'anthropic/claude', 'Fix the login redirect', NULL, NULL);
    INSERT INTO sessions(id, installation_id, parent_session_id, started_at, last_active_at, state, boundary, harness, repo, model_primary) VALUES ('child-a', 'install-a', 'root-a', '2026-09-01T09:10:00.000Z', '2026-09-01T11:00:00.000Z', 'inactive', 'header', 'claude-code', 'mimir', 'openai/gpt-5');
    INSERT INTO sessions(id, started_at, last_active_at, state, boundary, harness, repo, title, title_source) VALUES ('root-b', '2026-09-01T10:00:00.000Z', '2026-09-01T10:30:00.000Z', 'active', 'header', 'codex', 'other', 'Write docs', 'manual');
    INSERT INTO sessions(id, started_at, last_active_at, state, boundary, harness) VALUES ('root-c', '2026-08-30T10:00:00.000Z', '2026-08-30T10:00:00.000Z', 'inactive', 'header', 'pi');
    UPDATE sessions SET request_count = 2, tokens_in = 100, tokens_out = 10 WHERE id = 'root-a';
    UPDATE sessions SET request_count = 1, tokens_in = 50, tokens_out = 5 WHERE id = 'child-a';
    UPDATE sessions SET request_count = 1, tokens_in = 7, tokens_out = 1 WHERE id = 'root-b';
    INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, saved_at, request_kind, input_tokens, output_tokens, harness) VALUES ('ex-a1', 'root-a', '2026-09-01T09:00:00.000Z', 'chat', 'anthropic/claude', 1, 'log/ex-a1.json', 'saved', '2026-09-01T09:00:00.000Z', '2026-09-01T09:00:01.000Z', 'primary', 60, 6, 'claude-code');
    INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, saved_at, request_kind, input_tokens, output_tokens, harness) VALUES ('ex-a2', 'root-a', '2026-09-01T09:30:00.000Z', 'chat', 'anthropic/claude', 1, 'log/ex-a2.json', 'saved', '2026-09-01T09:30:00.000Z', '2026-09-01T09:30:01.000Z', 'primary', 40, 4, 'claude-code');
    INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, saved_at, request_kind, input_tokens, output_tokens, harness) VALUES ('ex-child', 'child-a', '2026-09-01T11:00:00.000Z', 'chat', 'openai/gpt-5', 1, 'log/ex-child.json', 'saved', '2026-09-01T11:00:00.000Z', '2026-09-01T11:00:01.000Z', 'primary', 50, 5, 'claude-code');
    INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, failed_at, request_kind, harness) VALUES ('ex-b-failed', 'root-b', '2026-09-01T10:10:00.000Z', 'chat', 'openai/gpt-5', 1, '', 'failed', '2026-09-01T10:10:00.000Z', '2026-09-01T10:10:00.000Z', 'primary', 'codex');
    INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, saved_at, request_kind, input_tokens, output_tokens, harness) VALUES ('ex-b', 'root-b', '2026-09-01T10:20:00.000Z', 'chat', 'openai/gpt-5', 1, 'log/ex-b.json', 'saved', '2026-09-01T10:20:00.000Z', '2026-09-01T10:20:01.000Z', 'primary', 7, 1, 'codex');
    INSERT INTO exchange_files(exchange_id, session_id, file) VALUES ('ex-a1', 'root-a', 'src/login.ts');
    INSERT INTO exchange_files(exchange_id, session_id, file) VALUES ('ex-child', 'child-a', 'src/login.ts');
    INSERT INTO session_files(session_id, file) VALUES ('root-a', 'src/login.ts');
    INSERT INTO session_files(session_id, file) VALUES ('child-a', 'src/login.ts');
    INSERT INTO exchange_errors(exchange_id, session_id, signature) VALUES ('ex-a2', 'root-a', 'TypeError: boom');
    INSERT INTO session_errors(session_id, signature) VALUES ('root-a', 'TypeError: boom');
    INSERT INTO session_git_artifacts(session_id, commit_sha, committed_at, subject, repository_url, repository_key, ref, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, saved_at, created_at, pushed, pushed_at) VALUES ('root-a', '${"a".repeat(40)}', '2026-09-01T09:20:00.000Z', 'Fix redirect', 'https://github.com/example/mimir', 'https://github.com/example/mimir', 'main', 'git', 'sessions/root-a/git/x.patch', '${"0".repeat(64)}', 10, 1, 2, 1, 'saved', '2026-09-01T09:20:00.000Z', '2026-09-01T09:20:00.000Z', '2026-09-01T09:20:00.000Z', 1, '2026-09-01T09:25:00.000Z');
    INSERT INTO session_git_artifacts(session_id, commit_sha, committed_at, subject, repository_url, repository_key, ref, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, saved_at, created_at) VALUES ('root-b', '${"a".repeat(40)}', '2026-09-01T09:20:00.000Z', 'Fix redirect', 'https://github.com/example/mimir', 'https://github.com/example/mimir', 'main', 'git', 'sessions/root-b/git/x.patch', '${"0".repeat(64)}', 10, 1, 2, 1, 'saved', '2026-09-01T10:21:00.000Z', '2026-09-01T10:21:00.000Z', '2026-09-01T10:21:00.000Z');
    INSERT INTO session_git_artifacts(session_id, commit_sha, committed_at, subject, repository_key, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, saved_at, created_at) VALUES ('root-b', '${"b".repeat(40)}', '2026-09-01T10:25:00.000Z', 'Docs', 'session:root-b', 'git', 'sessions/root-b/git/y.patch', '${"0".repeat(64)}', 10, 1, 1, 0, 'saved', '2026-09-01T10:25:00.000Z', '2026-09-01T10:25:00.000Z', '2026-09-01T10:25:00.000Z');
    INSERT INTO exchange_search(exchange_id, body) VALUES ('ex-a1', 'The login redirect loops forever because the cookie path is wrong');
    INSERT INTO exchange_search(exchange_id, body) VALUES ('ex-child', 'Patched the cookie path in src/login.ts');
    INSERT INTO exchange_search(exchange_id, body) VALUES ('ex-b', 'Documented the cookie settings');
  `);
  const envelope = (id: string, messages: unknown[], response: unknown) =>
    env.LOGS.put(`log/${id}.json`, JSON.stringify({ exchange_id: id, request: { messages }, response: { format: "json", body: response } }));
  const user = { role: "user", content: "Fix the login redirect" };
  const call = { role: "assistant", content: [{ type: "toolCall", id: "call-1", name: "read", arguments: { path: "src/login.ts" } }] };
  await envelope("ex-a1", [user], call);
  await envelope("ex-a2", [user, call, { role: "toolResult", toolCallId: "call-1", content: "export const login = 1" }], { role: "assistant", content: "Fixed the redirect." });
  await envelope("ex-child", [{ role: "user", content: "Patch cookie path" }], { role: "assistant", content: "Patched." });
}

describe("Query API", () => {
  it("lists root sessions with subtree projections, filters and pagination on both prefixes", async () => {
    await seed();
    const all = await api<{ sessions: Json[]; next_cursor: string | null }>("/api/sessions");
    expect(all.status).toBe(200);
    expect(all.body.sessions.map((session) => session.id)).toEqual(["root-a", "root-b", "root-c"]);
    expect(all.body.next_cursor).toBeNull();
    expect(all.body.sessions[0]).toEqual({
      id: "root-a",
      parent_session_id: null,
      started_at: "2026-09-01T09:00:00.000Z",
      ended_at: null,
      activity_at: "2026-09-01T11:00:00.000Z",
      state: "inactive",
      harness: "claude-code",
      repo: "mimir",
      source_ref: null,
      model_primary: "anthropic/claude",
      models: [
        { name: "anthropic/claude", request_count: 2, first_seen_at: "2026-09-01T09:00:00.000Z", last_seen_at: "2026-09-01T09:30:00.000Z" },
        { name: "openai/gpt-5", request_count: 1, first_seen_at: "2026-09-01T11:00:00.000Z", last_seen_at: "2026-09-01T11:00:00.000Z" },
      ],
      title: null,
      title_source: null,
      display_title: "Fix the login redirect",
      intent: "Fix the login redirect",
      request_count: 3,
      tokens_in: 150,
      tokens_out: 15,
      cache_read_tokens: 0,
      cache_write_tokens: 0,
      child_session_count: 1,
      device: { id: "install-a", name: "Desk", platform: "linux", arch: "amd64" },
      capture: { status: "saved", saved_exchanges: 3, failed_exchanges: 0, pending_exchanges: 0, last_saved_at: "2026-09-01T11:00:01.000Z" },
      git_status: "pushed",
      commit_count: 1,
      goal: null,
    });
    expect(all.body.sessions[1]).toMatchObject({ id: "root-b", display_title: "Write docs", git_status: "committed", commit_count: 2, capture: { status: "partial" } });
    expect(all.body.sessions[2]).toMatchObject({ id: "root-c", display_title: "root-c", git_status: "none", capture: { status: "empty" }, device: null });

    const dashboard = (await (await dashboardRequest("/dashboard/api/sessions")).json()) as Json;
    expect(dashboard).toEqual(all.body);

    const page1 = await api<Json>("/api/sessions?limit=2");
    expect(page1.body.sessions.map((session: Json) => session.id)).toEqual(["root-a", "root-b"]);
    const page2 = await api<Json>(`/api/sessions?limit=2&cursor=${page1.body.next_cursor}`);
    expect(page2.body).toMatchObject({ sessions: [{ id: "root-c" }], next_cursor: null });

    const ids = async (query: string) => ((await api<Json>(`/api/sessions?${query}`)).body.sessions as Json[]).map((session) => session.id);
    expect(await ids("q=redirect")).toEqual(["root-a"]);
    expect(await ids("q=cookie")).toEqual(["root-a", "root-b"]);
    expect(await ids("q=other")).toEqual(["root-b"]);
    expect(await ids("repo=mimir")).toEqual(["root-a"]);
    expect(await ids("harness=pi")).toEqual(["root-c"]);
    expect(await ids("device=Desk")).toEqual(["root-a"]);
    expect(await ids("device=install-a")).toEqual(["root-a"]);
    expect(await ids("state=active")).toEqual(["root-b"]);
    expect(await ids("git_status=pushed")).toEqual(["root-a"]);
    expect(await ids("git_status=committed")).toEqual(["root-b"]);
    expect(await ids("git_status=none")).toEqual(["root-c"]);
    expect(await ids("capture=partial")).toEqual(["root-b"]);
    expect(await ids("capture=empty")).toEqual(["root-c"]);
    expect(await ids("from=2026-09-01&to=2026-09-01")).toEqual(["root-a", "root-b"]);
    expect(await ids("to=2026-08-31")).toEqual(["root-c"]);
    expect(await ids("from=2026-09-01T10:45:00Z")).toEqual(["root-a"]);
    expect(await ids("from=1h")).toEqual([]);
    for (const query of ["state=done", "git_status=landed", "from=yesterday", "from=2026-09-02&to=2026-09-01", "cursor=%%%", "limit=0"])
      expect((await api(`/api/sessions?${query}`)).status).toBe(400);
    expect((await api<Json>("/api/sessions?limit=500")).status).toBe(200);
  });

  it("returns session detail, receipt, files, errors, commits and supporting sessions", async () => {
    await seed();
    const detail = await api<Json>("/api/sessions/root-a");
    expect(detail.status).toBe(200);
    expect(detail.body.session.id).toBe("root-a");
    expect(detail.body.summary).toBeNull();
    expect(detail.body.files).toEqual([{ path: "src/login.ts", count: 2 }]);
    expect(detail.body.errors).toEqual([{ signature: "TypeError: boom", count: 1, last_seen_at: "2026-09-01T09:30:00.000Z" }]);
    expect(detail.body.commits).toMatchObject([{ session_id: "root-a", commit_sha: "a".repeat(40), pushed: true, pushed_at: "2026-09-01T09:25:00.000Z" }]);
    expect(detail.body.supporting_sessions).toMatchObject([{ id: "child-a", parent_session_id: "root-a", request_count: 1, git_status: "none" }]);
    expect(detail.body.receipt).toEqual({
      session_id: "root-a",
      title: "Fix the login redirect",
      repo: "mimir",
      harness: "claude-code",
      device: { id: "install-a", name: "Desk" },
      state: "inactive",
      capture: detail.body.session.capture,
      git_status: "pushed",
      commit_count: 1,
      goal: null,
      dashboard_url: null,
      line: "Saved to Mimir · 3 exchanges · 1 commit",
    });
    expect((await api("/api/sessions/missing")).status).toBe(404);
    expect(await api("/api/sessions/missing/receipt")).toEqual({ status: 404, body: { error: "session not found" } });

    const local = (await (await dashboardRequest("/dashboard/api/sessions/root-b/receipt")).json()) as Json;
    expect(local).toMatchObject({ dashboard_url: "http://localhost/dashboard/sessions/root-b", line: "Partially saved · 1 saved · 1 failed · 2 commits" });
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, boundary) VALUES ('receipt-pending', '2026-09-01T10:00:00.000Z', 'header');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, accepted_at) VALUES ('pending-1', 'receipt-pending', '2026-09-01T10:00:00.000Z', 'chat', 1, 'log/p.json', 'accepted', '2026-09-01T10:00:00.000Z');
    `);
    expect((await api<Json>("/api/sessions/receipt-pending/receipt")).body.line).toBe("Saving to Mimir… · 1 pending");
    expect((await api<Json>("/api/sessions/root-c/receipt")).body.line).toBe("Not captured");
  });

  it("rebuilds turns from archives with pagination, ordering and kind filters", async () => {
    await seed();
    const asc = await api<Json>("/api/sessions/root-a/turns?limit=2");
    expect(asc.body.turns).toEqual([
      {
        exchange_id: "ex-a1",
        session_id: "root-a",
        ts: "2026-09-01T09:00:00.000Z",
        model: "anthropic/claude",
        request_kind: "primary",
        capture_status: "saved",
        user: "Fix the login redirect",
        assistant: null,
        reasoning: null,
        tools: [{ name: "read", input: JSON.stringify({ path: "src/login.ts" }, null, 2), output: "export const login = 1", error: false }],
        truncated: false,
      },
      expect.objectContaining({ exchange_id: "ex-a2", user: null, assistant: "Fixed the redirect.", tools: [] }),
    ]);
    const next = await api<Json>(`/api/sessions/root-a/turns?limit=2&cursor=${asc.body.next_cursor}`);
    expect(next.body).toMatchObject({ turns: [{ exchange_id: "ex-child", session_id: "child-a", user: "Patch cookie path", assistant: "Patched." }], next_cursor: null });
    const desc = await api<Json>("/api/sessions/root-a/turns?order=desc&limit=1");
    expect(desc.body.turns.map((turn: Json) => turn.exchange_id)).toEqual(["ex-child"]);
    expect((await api(`/api/sessions/root-a/turns?order=asc&cursor=${desc.body.next_cursor}`)).status).toBe(400);
    expect((await api<Json>("/api/sessions/root-a/turns?kinds=title")).body.turns).toEqual([]);
    expect((await api("/api/sessions/root-a/turns?kinds=bogus")).status).toBe(400);
    expect((await api("/api/sessions/missing/turns")).status).toBe(404);
    const failed = await api<Json>("/api/sessions/root-b/turns");
    expect(failed.body.turns).toMatchObject([
      { exchange_id: "ex-b-failed", capture_status: "failed", user: null, truncated: false },
      { exchange_id: "ex-b", capture_status: "saved", user: null, truncated: true },
    ]);
  });

  it("searches saved exchange text with FTS ranking grouped by root session", async () => {
    await seed();
    const result = await api<Json>("/api/search?q=cookie%20path");
    expect(result.body.query).toBe("cookie path");
    expect(result.body.results).toHaveLength(1);
    expect(result.body.results[0].session.id).toBe("root-a");
    expect(result.body.results[0].score).toBeGreaterThan(0);
    expect(result.body.results[0].hits.map((hit: Json) => hit.exchange_id).sort()).toEqual(["ex-a1", "ex-child"]);
    expect(result.body.results[0].hits[0].snippet).toContain("**cookie**");
    const cookie = await api<Json>("/api/search?q=cookie&limit=1");
    expect(cookie.body.results).toHaveLength(1);
    expect((await api<Json>("/api/search?q=cookie&repo=other")).body.results.map((r: Json) => r.session.id)).toEqual(["root-b"]);
    expect((await api<Json>("/api/search?q=cookie&from=2026-09-01T10:30:00Z")).body.results.map((r: Json) => r.session.id)).toEqual(["root-a"]);
    expect((await api("/api/search")).status).toBe(400);
    expect((await api("/api/search?q=%22%2A")).status).toBe(400);
  });

  it("aggregates stats by every grouping", async () => {
    await seed();
    const range = "from=2026-08-01&to=2026-09-30";
    const by = async (group: string, extra = "") => (await api<Json>(`/api/stats?group_by=${group}&${range}${extra}`)).body;
    expect(await by("repo")).toEqual({
      from: "2026-08-01T00:00:00.000Z",
      to: "2026-10-01T00:00:00.000Z",
      group_by: "repo",
      groups: [
        { key: "mimir", sessions: 1, requests: 3, tokens_in: 150, tokens_out: 15, commits: 1 },
        { key: "other", sessions: 1, requests: 1, tokens_in: 7, tokens_out: 1, commits: 2 },
      ],
    });
    expect((await by("model")).groups).toEqual([
      { key: "openai/gpt-5", sessions: 2, requests: 2, tokens_in: 57, tokens_out: 6, commits: 0 },
      { key: "anthropic/claude", sessions: 1, requests: 2, tokens_in: 100, tokens_out: 10, commits: 1 },
      { key: "unknown", sessions: 0, requests: 0, tokens_in: 0, tokens_out: 0, commits: 2 },
    ]);
    expect((await by("device")).groups.map((group: Json) => group.key)).toEqual(["Desk", "unknown"]);
    expect((await by("harness")).groups.map((group: Json) => group.key)).toEqual(["claude-code", "codex"]);
    expect((await by("day", "&tz=America/Los_Angeles")).groups).toEqual([
      { key: "2026-09-01", sessions: 2, requests: 4, tokens_in: 157, tokens_out: 16, commits: 2 },
    ]);
    expect((await by("day", "&tz=Pacific/Kiritimati")).groups.map((group: Json) => group.key)).toEqual(["2026-09-01", "2026-09-02"]);
    expect((await api<Json>("/api/stats?group_by=repo")).body.groups).toEqual([]);
    expect((await api("/api/stats")).status).toBe(400);
    expect((await api("/api/stats?group_by=day&tz=Mars/Base")).status).toBe(400);
  });

  it("returns day snapshots with git status instead of outcomes", async () => {
    await seed();
    const day = await api<Json>("/api/days/2026-09-01?tz=UTC");
    expect(day.status).toBe(200);
    expect(day.body.sessions.find((session: Json) => session.id === "root-a")).toMatchObject({ git_status: "pushed", commit_count: 1 });
    expect(day.body.sessions[0]).not.toHaveProperty("outcome");
    expect(day.body.attention).toEqual({ active: ["root-b"], capture: ["root-b"] });
    const workstream = day.body.workstreams.find((stream: Json) => stream.session_ids.includes("root-a"));
    expect(workstream.git).toEqual({ none: 0, committed: 0, pushed: 1 });
    expect(workstream).not.toHaveProperty("outcomes");
    expect(workstream.commits[0]).toMatchObject({ commit_sha: "a".repeat(40), pushed: true });
    expect((await api("/api/days/2026-13-01")).status).toBe(400);
    expect((await api("/api/days/2026-09-01?tz=Nowhere")).status).toBe(400);
  });

  it("lists commits grouped by repository and SHA with sessions", async () => {
    await seed();
    const commits = await api<Json>("/api/commits");
    expect(commits.body.commits.map((commit: Json) => [commit.commit_sha[0], commit.sessions.map((session: Json) => session.id)])).toEqual([
      ["b", ["root-b"]],
      ["a", ["root-a", "root-b"]],
    ]);
    expect(commits.body.commits[1]).toMatchObject({ repository_key: "https://github.com/example/mimir", pushed: true, subject: "Fix redirect" });
    const page = await api<Json>("/api/commits?limit=1");
    expect(page.body.commits).toHaveLength(1);
    const rest = await api<Json>(`/api/commits?limit=1&cursor=${page.body.next_cursor}`);
    expect(rest.body.commits[0].commit_sha).toBe("a".repeat(40));
    expect(rest.body.next_cursor).toBeNull();
    const only = async (query: string) => ((await api<Json>(`/api/commits?${query}`)).body.commits as Json[]).map((commit) => commit.commit_sha[0]);
    expect(await only("repo=https://github.com/example/mimir")).toEqual(["a"]);
    expect(await only("ref=main")).toEqual(["a"]);
    expect(await only("q=docs")).toEqual(["b"]);
    expect(await only("session=child-a")).toEqual(["a"]);
    expect(await only("from=2026-09-01T10:00:00Z")).toEqual(["b"]);
  });

  it("serves devices on both prefixes and device edits only to the dashboard", async () => {
    await seed();
    const devices = await api<Json>("/api/devices");
    expect(devices.body.devices).toEqual([
      {
        id: "install-a",
        name: "Desk",
        platform: "linux",
        arch: "amd64",
        created_at: expect.any(String),
        updated_at: expect.any(String),
        last_seen_at: null,
        revoked_at: null,
        session_count: 1,
        harnesses: ["claude-code"],
        tailnet: null,
      },
    ]);
    const renamed = await dashboardRequest("/dashboard/api/devices/install-a", { method: "PATCH", headers: { "content-type": "application/json" }, body: JSON.stringify({ name: " Laptop " }) });
    expect(await renamed.json()).toMatchObject({ id: "install-a", name: "Laptop" });
    const edit = (path: string, init: RequestInit) => dashboardRequest(path, { ...init, headers: { "content-type": "application/json" } });
    expect((await edit("/dashboard/api/devices/install-a", { method: "PATCH", body: JSON.stringify({ name: "" }) })).status).toBe(400);
    expect((await edit("/dashboard/api/devices/missing", { method: "PATCH", body: JSON.stringify({ name: "x" }) })).status).toBe(404);
    expect((await api("/api/devices/install-a", { method: "PATCH", body: JSON.stringify({ name: "x" }) })).status).toBe(404);
    expect((await api("/api/devices/install-a/revoke", { method: "POST" })).status).toBe(404);
    const revoked = await edit("/dashboard/api/devices/install-a/revoke", { method: "POST" });
    expect(await revoked.json()).toMatchObject({ id: "install-a", revoked_at: expect.any(String) });
    expect((await request("/whoami", { headers: { authorization: "Bearer machine-a" } })).status).toBe(401);
  });

  it("answers unknown query routes with JSON errors", async () => {
    expect(await api("/api/nope")).toEqual({ status: 404, body: { error: "not found" } });
  });
});
