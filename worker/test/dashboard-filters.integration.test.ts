import { beforeEach, describe, expect, it } from "vitest";
import { dashboardRequest, env } from "./support";

type ExchangePage = { exchanges: Array<{ id: string; response_excerpt: string; request_kind: string; capture_status: string }>; next_cursor: string | null };
type SessionPage = { sessions: Array<{ id: string; capture: { status: string } }>; next_cursor: string | null };

describe("Dashboard filter contracts", () => {
  beforeEach(async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, state, boundary, repo, summary_text, summary_source, summary_status, summary_updated_at) VALUES ('filters-root', '2099-04-10T00:00:00Z', 'inactive', 'header', 'project', 'Investigated queue starvation', 'reconstructed:v2:test', 'ready', '2099-05-01T00:00:00Z');
      INSERT INTO sessions(id, parent_session_id, started_at, state, boundary, repo) VALUES ('filters-child', 'filters-root', '2099-04-10T00:01:00Z', 'inactive', 'header', 'project');
      INSERT INTO sessions(id, started_at, state, boundary, repo, summary_text, summary_source, summary_status, summary_updated_at) VALUES ('filters-other', '2099-04-11T00:00:00Z', 'inactive', 'header', 'other', 'Investigated queue starvation', 'reconstructed:v2:test', 'ready', '2099-05-01T00:00:00Z');
      INSERT INTO sessions(id, started_at, state, boundary) VALUES ('filters-pending', '2099-04-10T00:00:00Z', 'active', 'header');
      INSERT INTO sessions(id, started_at, state, boundary) VALUES ('filters-empty', '2099-04-10T00:00:00Z', 'inactive', 'header');
      INSERT INTO exchanges(id, session_id, ts, endpoint, model, provider, harness, repo, latency_ms, r2_key, request_excerpt, response_excerpt, capture_status, request_kind) VALUES ('filter-start', 'filters-root', '2099-04-10T00:00:00Z', 'chat', 'model-a', 'provider-a', 'codex', 'project', 1, 'log/filter-start.json', 'ordinary prompt', 'Queue recovered after retry', 'saved', 'primary');
      INSERT INTO exchanges(id, session_id, ts, endpoint, model, provider, harness, repo, latency_ms, r2_key, request_excerpt, response_excerpt, capture_status, request_kind) VALUES ('filter-end', 'filters-child', '2099-04-10T23:59:59.999Z', 'chat', 'model-b', 'provider-b', 'opencode', 'project', 1, 'log/filter-end.json', 'ordinary prompt', 'Queue recovered after retry', 'saved', 'primary');
      INSERT INTO exchanges(id, session_id, ts, endpoint, model, provider, harness, repo, latency_ms, r2_key, request_excerpt, response_excerpt, capture_status, request_kind) VALUES ('filter-after', 'filters-other', '2099-04-11T00:00:00Z', 'chat', 'model-a', 'provider-a', 'codex', 'other', 1, 'log/filter-after.json', 'ordinary prompt', 'Queue recovered after retry', 'saved', 'primary');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, response_excerpt, capture_status, request_kind) VALUES ('filter-title', 'filters-root', '2099-04-10T01:00:00Z', 'chat', 1, 'log/filter-title.json', 'Queue repair', 'saved', 'title');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, failure_code) VALUES ('filter-failed', 'filters-root', '2099-04-10T02:00:00Z', 'chat', 1, 'log/filter-failed.json', 'failed', 'r2_write_failed');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status) VALUES ('filter-pending', 'filters-pending', '2099-04-10T03:00:00Z', 'chat', 1, 'log/filter-pending.json', 'accepted');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status) VALUES ('filter-pending-failed', 'filters-pending', '2099-04-10T03:01:00Z', 'chat', 1, 'log/filter-pending-failed.json', 'failed');
      INSERT INTO exchange_errors(exchange_id, session_id, signature) VALUES ('filter-end', 'filters-child', 'queue timeout');
      INSERT INTO session_errors(session_id, signature) VALUES ('filters-child', 'queue timeout');
      INSERT INTO exchange_tools(exchange_id, name) VALUES ('filter-start', 'read');
      INSERT INTO exchange_tools(exchange_id, name) VALUES ('filter-end', 'bash');
      INSERT INTO session_git_artifacts(session_id, commit_sha, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, created_at) VALUES ('filters-child', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', 'git', 'sessions/filters-child/git/patch.patch', 'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff', 10, 1, 1, 0, 'accepted', '2099-04-10T00:00:00Z', '2099-04-10T00:00:00Z');
    `);
  });

  it("searches response excerpts across pages with inclusive UTC days and stable ordering", async () => {
    const filters = "q=RECOVERED&from=2099-04-10&to=2099-04-10&request_kind=primary&capture_status=saved&order=asc&limit=1";
    const first = await (await dashboardRequest(`/dashboard/api/log?${filters}`)).json() as ExchangePage;
    expect(first.exchanges.map((row) => row.id)).toEqual(["filter-start"]);
    expect(first.exchanges[0]!.response_excerpt).toBe("Queue recovered after retry");
    const second = await (await dashboardRequest(`/dashboard/api/log?${filters}&cursor=${first.next_cursor}`)).json() as ExchangePage;
    expect(second.exchanges.map((row) => row.id)).toEqual(["filter-end"]);
    expect(second.next_cursor).toBeNull();
    expect((await dashboardRequest(`/dashboard/api/log?order=desc&cursor=${first.next_cursor}`)).status).toBe(400);
    const exact = await (await dashboardRequest("/dashboard/api/log?from=2099-04-10T23:59:59.999Z&to=2099-04-10T23:59:59.999Z")).json() as ExchangePage;
    expect(exact.exchanges.map((row) => row.id)).toEqual(["filter-end"]);
    const offset = await (await dashboardRequest("/dashboard/api/log?from=2099-04-10T01:00:00%2B01:00&to=2099-04-10T01:00:00%2B01:00")).json() as ExchangePage;
    expect(offset.exchanges.map((row) => row.id)).toEqual(["filter-start"]);
  });

  it("combines exact request tool and error evidence without matching unrelated exchanges in the same session", async () => {
    const matched = await (await dashboardRequest("/dashboard/api/sessions/filters-root/exchanges?q=recovered&tool=bash&errors=true&provider=provider-b&request_kind=primary&capture_status=saved")).json() as ExchangePage;
    expect(matched.exchanges.map((row) => row.id)).toEqual(["filter-end"]);
    const unrelated = await (await dashboardRequest("/dashboard/api/sessions/filters-root/exchanges?tool=read&errors=true")).json() as ExchangePage;
    expect(unrelated.exchanges).toEqual([]);
    const failed = await (await dashboardRequest("/dashboard/api/log?session=filters-root&errors=true&capture_status=failed")).json() as ExchangePage;
    expect(failed.exchanges.map((row) => row.id)).toEqual(["filter-failed"]);
    const auxiliary = await (await dashboardRequest("/dashboard/api/log?session=filters-root&request_kind=title")).json() as ExchangePage;
    expect(auxiliary.exchanges.map((row) => row.id)).toEqual(["filter-title"]);
    const pending = await (await dashboardRequest("/dashboard/api/log?capture_status=accepted")).json() as ExchangePage;
    expect(pending.exchanges.map((row) => row.id)).toEqual(["filter-pending"]);
  });

  it("rejects nonmember scopes and aligns own versus tree facets with exchange membership", async () => {
    expect((await dashboardRequest("/dashboard/api/sessions/filters-root/exchanges?session=filters-other")).status).toBe(400);
    const own = await (await dashboardRequest("/dashboard/api/sessions/filters-root/exchanges?session=filters-root&request_kind=primary&capture_status=saved")).json() as ExchangePage;
    expect(own.exchanges.map((row) => row.id)).toEqual(["filter-start"]);
    const child = await (await dashboardRequest("/dashboard/api/sessions/filters-root/exchanges?session=filters-child")).json() as ExchangePage;
    expect(child.exchanges.map((row) => row.id)).toEqual(["filter-end"]);
    const ownFacets = await (await dashboardRequest("/dashboard/api/facets?session=filters-root&scope=own")).json() as { providers: string[]; tools: string[] };
    const treeFacets = await (await dashboardRequest("/dashboard/api/facets?session=filters-root&scope=tree")).json() as { providers: string[]; tools: string[] };
    expect(ownFacets.providers).toEqual(["provider-a"]);
    expect(ownFacets.tools).toEqual(["read"]);
    expect(treeFacets.providers.sort()).toEqual(["provider-a", "provider-b"]);
    expect(treeFacets.tools.sort()).toEqual(["bash", "read"]);
    expect((await dashboardRequest("/dashboard/api/facets?session=filters-root&scope=foreign")).status).toBe(400);
  });

  it("filters root state, subtree capture, provider, errors, commits and searchable summaries together", async () => {
    const response = await (await dashboardRequest("/dashboard/api/sessions?q=starvation&repo=project&state=inactive&capture=partial&provider=provider-b&errors=true&commits=true&from=2099-04-10&to=2099-04-10")).json() as SessionPage;
    expect(response.sessions.map((row) => row.id)).toEqual(["filters-root"]);
    expect(response.sessions[0]!.capture.status).toBe("partial");
    const pending = await (await dashboardRequest("/dashboard/api/sessions?state=active&capture=pending&errors=true")).json() as SessionPage;
    expect(pending.sessions.map((row) => row.id)).toEqual(["filters-pending"]);
    expect(pending.sessions[0]!.capture.status).toBe("pending");
    const empty = await (await dashboardRequest("/dashboard/api/sessions?capture=empty")).json() as SessionPage;
    expect(empty.sessions.map((row) => row.id)).toEqual(["filters-empty"]);
    const first = await (await dashboardRequest("/dashboard/api/sessions?q=starvation&limit=1")).json() as SessionPage;
    const second = await (await dashboardRequest(`/dashboard/api/sessions?q=starvation&limit=1&cursor=${first.next_cursor}`)).json() as SessionPage;
    expect([...first.sessions, ...second.sessions].map((row) => row.id)).toEqual(["filters-other", "filters-root"]);
    expect(second.next_cursor).toBeNull();
  });

  it("rejects invalid dates, reversed ranges and unsupported enum values", async () => {
    for (const filter of ["from=2099-02-30", "to=not-a-date", "from=2099-04-11&to=2099-04-10", "request_kind=unknown", "capture_status=pending", "errors=false", "order=sideways", "cursor=invalid"]) {
      expect((await dashboardRequest(`/dashboard/api/log?${filter}`)).status).toBe(400);
      expect((await dashboardRequest(`/dashboard/api/sessions/filters-root/exchanges?${filter}`)).status).toBe(400);
    }
    for (const filter of ["state=unknown", "capture=accepted", "errors=false", "commits=false", "from=2099-02-30", "from=2099-04-11&to=2099-04-10", "cursor=invalid"]) {
      expect((await dashboardRequest(`/dashboard/api/sessions?${filter}`)).status).toBe(400);
    }
  });
});
