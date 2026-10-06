import { describe, expect, it } from "vitest";
import type { CommitCapture, CommitTimelineEntry, Device, Exchange, LogEnvelope, Session, SessionDetail, SessionExchange } from "../src/lib/api";
import { fixtureRequest } from "../src/lib/dev-fixtures";

describe("device fixtures", () => {
  it("lists, renames, and revokes devices", async () => {
    const listed = await fixtureRequest<{ devices: Device[] }>("/dashboard/api/devices");
    const device = listed.devices.find((item) => !item.revoked_at);
    expect(device).toBeDefined();

    const renamed = await fixtureRequest<{ device: Device }>(`/dashboard/api/devices/${device!.id}`, {
      method: "PATCH",
      body: JSON.stringify({ name: "Renamed fixture" }),
    });
    expect(renamed.device.name).toBe("Renamed fixture");

    const revoked = await fixtureRequest<{ device: Device }>(`/dashboard/api/devices/${device!.id}/revoke`, { method: "POST" });
    expect(revoked.device.revoked_at).not.toBeNull();
  });

  it("includes a device that has never been seen", async () => {
    const listed = await fixtureRequest<{ devices: Device[] }>("/dashboard/api/devices");
    expect(listed.devices.some((device) => device.last_seen_at === null)).toBe(true);
  });

  it("does not expose revocation or aggregate metadata through session device identity", async () => {
    const result = await fixtureRequest<{ sessions: Session[] }>("/dashboard/api/sessions?limit=1");
    expect(result.sessions[0].device).not.toHaveProperty("revoked_at");
    expect(result.sessions[0].device).not.toHaveProperty("session_count");
  });
});

const rich = "ses_fixture_multi_model_result";
const sharedSha = "7ad8d9e43a61c59fe22379f8e5ca68dbe8c41120";
type RequestPage = { exchanges: Exchange[]; next_cursor: string | null };
type SessionPage = { sessions: Session[]; next_cursor: string | null };
type CommitPage = { commits: CommitTimelineEntry[]; next_cursor: string | null };

describe("fixture archive boundaries", () => {
  it("returns the complete saved envelope rather than request metadata from the archive namespace", async () => {
    const detail = await fixtureRequest<{ exchange: Exchange; log_url: string }>("/dashboard/api/log/req_fixture_02");
    const archive = await fixtureRequest<LogEnvelope>(detail.log_url);
    expect(archive.exchange_id).toBe(detail.exchange.id);
    expect(archive.response.format).toBe("json");
    expect(archive.request).toHaveProperty("messages");
    if (archive.response.format === "json") {
      expect(archive.response.body).toMatchObject({ messages: [{ role: "assistant", parts: [{ type: "tool", callID: "call_sample_read_header", state: { status: "completed" } }] }] });
    }
    await expect(fixtureRequest(`${detail.log_url}/extra`)).rejects.toThrow();
    await expect(fixtureRequest("/other/api/log/req_fixture_02")).rejects.toThrow();
    await expect(fixtureRequest(`/dashboard/api/sessions/${rich}/unknown`)).rejects.toThrow();
  });

  it("keeps missing captures inspectable as receipts without manufacturing an archive", async () => {
    for (const [id, status] of [["req_fixture_active_pending", "accepted"], ["req_fixture_failed_capture", "failed"]]) {
      const detail = await fixtureRequest<{ exchange: Exchange; log_url: string }>(`/dashboard/api/log/${id}`);
      expect(detail.exchange.capture_status).toBe(status);
      await expect(fixtureRequest(detail.log_url)).rejects.toThrow();
    }
  });

  it("preserves unknown vendor evidence for raw inspection", async () => {
    const envelope = await fixtureRequest<LogEnvelope>("/dashboard/dev-fixtures/log/req_fixture_unknown_shape");
    expect(envelope.request).toHaveProperty("vendor_payload.future_format", true);
    expect(envelope.response).toHaveProperty("body.vendor_output.fragments");
    const detail = await fixtureRequest<SessionDetail>("/dashboard/api/sessions/ses_fixture_harness_cursor");
    expect(detail.summary?.partial).toBe(true);
  });
});

describe("fixture filter and scope semantics", () => {
  it("filters the complete dataset before stable timestamp-and-ID pagination", async () => {
    const all = await fixtureRequest<RequestPage>("/dashboard/api/log?request_kind=primary&order=asc&limit=100");
    const seen: Exchange[] = [];
    let cursor: string | null = null;
    do {
      const page = await fixtureRequest<RequestPage>(`/dashboard/api/log?request_kind=primary&order=asc&limit=1${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`);
      seen.push(...page.exchanges);
      cursor = page.next_cursor;
    } while (cursor);
    expect(seen.map((row) => row.id)).toEqual(all.exchanges.map((row) => row.id));
    expect(new Set(seen.map((row) => row.id)).size).toBe(seen.length);
    const tied = seen.filter((row) => row.id === "req_fixture_11" || row.id === "req_fixture_claude_01");
    expect(tied.map((row) => row.id)).toEqual(["req_fixture_11", "req_fixture_claude_01"]);
    expect(tied[0].ts).toBe(tied[1].ts);
    const responseSearch = await fixtureRequest<RequestPage>("/dashboard/api/log?q=narrower%20review%20patch&limit=1");
    expect(responseSearch.exchanges.map((row) => row.id)).toEqual(["req_fixture_claude_01"]);
    const day = await fixtureRequest<RequestPage>("/dashboard/api/log?from=2026-10-05&to=2026-10-05&limit=100");
    expect(day.exchanges.map((row) => row.id).sort()).toEqual(all.exchanges.concat(
      (await fixtureRequest<RequestPage>("/dashboard/api/log?request_kind=title")).exchanges,
      (await fixtureRequest<RequestPage>("/dashboard/api/log?request_kind=summary")).exchanges,
      (await fixtureRequest<RequestPage>("/dashboard/api/log?request_kind=compaction")).exchanges,
    ).map((row) => row.id).sort());
  });

  it("isolates own requests and facets while supporting explicit subtree scope", async () => {
    const path = `/dashboard/api/sessions/${rich}/exchanges`;
    const own = await fixtureRequest<{ exchanges: SessionExchange[] }>(`${path}?session=${rich}&q=rejected%20patch&limit=100`);
    const tree = await fixtureRequest<{ exchanges: SessionExchange[] }>(`${path}?q=rejected%20patch&limit=100`);
    expect(own.exchanges).toEqual([]);
    expect(tree.exchanges.map((row) => row.id)).toEqual(["req_fixture_sub_03"]);
    await expect(fixtureRequest(`${path}?session=ses_fixture_harness_pi`)).rejects.toThrow();
    const ownFacets = await fixtureRequest<{ providers: string[] }>(`/dashboard/api/facets?session=${rich}&scope=own`);
    const treeFacets = await fixtureRequest<{ providers: string[] }>(`/dashboard/api/facets?session=${rich}&scope=tree`);
    expect(ownFacets.providers).not.toContain("deepseek");
    expect(treeFacets.providers).toContain("deepseek");
  });

  it("combines exact tool/error and auxiliary filters without hiding capture failures", async () => {
    const failed = await fixtureRequest<RequestPage>("/dashboard/api/log?errors=true&capture_status=failed&limit=100");
    expect(failed.exchanges.map((row) => row.id).sort()).toEqual(["req_fixture_capture_failed_only", "req_fixture_failed_capture"]);
    const pendingTools = await fixtureRequest<RequestPage>("/dashboard/api/log?tool=bash&capture_status=accepted");
    expect(pendingTools.exchanges).toEqual([]);
    const toolFailure = await fixtureRequest<RequestPage>(`/dashboard/api/log?session=${rich}&tool=bash&errors=true`);
    expect(toolFailure.exchanges.map((row) => row.id)).toEqual(["req_fixture_09"]);
    const compacted = await fixtureRequest<RequestPage>("/dashboard/api/log?request_kind=compaction");
    expect(compacted.exchanges.map((row) => row.id)).toEqual(["req_fixture_compaction"]);
  });

  it("filters tree evidence independently from root state and work outcome", async () => {
    const branch = await fixtureRequest<SessionPage>("/dashboard/api/sessions?provider=deepseek&commits=true&state=inactive");
    expect(branch.sessions.map((row) => row.id)).toEqual([rich]);
    const pending = await fixtureRequest<SessionPage>("/dashboard/api/sessions?capture=pending&state=active");
    expect(pending.sessions.map((row) => row.id)).toEqual(["ses_fixture_active_capture"]);
    const failed = await fixtureRequest<SessionPage>("/dashboard/api/sessions?capture=failed");
    expect(failed.sessions.map((row) => row.id)).toEqual(["ses_fixture_capture_failed"]);
    const partial = await fixtureRequest<SessionPage>("/dashboard/api/sessions?capture=partial&outcome=discarded");
    expect(partial.sessions.map((row) => row.id)).toEqual(["ses_fixture_failed_work"]);
  });
});

describe("fixture captured commit boundaries", () => {
  it("retains all root associations and divergent patches when one capture matches", async () => {
    const page = await fixtureRequest<CommitPage>(`/dashboard/api/commits?session=${rich}&q=${sharedSha}`);
    expect(page.commits.map((entry) => entry.commit_sha)).toEqual([sharedSha]);
    const entry = page.commits[0];
    expect(entry.capture_count).toBe(2);
    expect(entry.captures.map((capture) => capture.session_id).sort()).toEqual(["ses_fixture_harness_claude_code", rich].sort());
    expect(new Set(entry.captures.map((capture) => capture.patch_sha256)).size).toBe(2);
    const mismatch = await fixtureRequest<CommitPage>(`/dashboard/api/commits?session=${rich}&ref=review/evidence&q=${sharedSha}`);
    expect(mismatch.commits).toEqual([]);
    for (const capture of entry.captures) {
      const patch = await fixtureRequest<string>(`/dashboard/api/sessions/${capture.session_id}/git-artifacts/${sharedSha}/patch`);
      const bytes = new TextEncoder().encode(patch);
      const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", bytes));
      expect(Array.from(digest, (byte) => byte.toString(16).padStart(2, "0")).join("")).toBe(capture.patch_sha256);
      expect(bytes.byteLength).toBe(capture.patch_bytes);
    }
    const rootPatch = await fixtureRequest<string>(`/dashboard/api/sessions/${rich}/git-artifacts/${sharedSha}/patch`);
    const childPatch = await fixtureRequest<string>(`/dashboard/api/sessions/ses_fixture_supporting_review/git-artifacts/${sharedSha}/patch`);
    expect(childPatch).toBe(rootPatch);
    await expect(fixtureRequest(`/dashboard/api/sessions/ses_fixture_harness_pi/git-artifacts/${sharedSha}/patch`)).rejects.toThrow();
  });

  it("does not merge equal SHAs from repositories without configured remotes", async () => {
    const page = await fixtureRequest<CommitPage>("/dashboard/api/commits?q=local%20repository&limit=100");
    expect(page.commits.map((entry) => entry.repository_key).sort()).toEqual(["session:ses_fixture_unknown_repo", "session:ses_fixture_unknown_repo_other"]);
    expect(page.commits[0].commit_sha).toBe(page.commits[1].commit_sha);
  });

  it("paginates associated captures and refuses unsaved patch bodies", async () => {
    const first = await fixtureRequest<{ captures: CommitCapture[]; next_cursor: string | null }>(`/dashboard/api/commits/captures?repo=https://github.com/example/mimir&commit=${sharedSha}&limit=1`);
    expect(first.captures.map((capture) => capture.session_id)).toEqual(["ses_fixture_harness_claude_code"]);
    const second = await fixtureRequest<{ captures: CommitCapture[]; next_cursor: string | null }>(`/dashboard/api/commits/captures?repo=https://github.com/example/mimir&commit=${sharedSha}&limit=1&cursor=${encodeURIComponent(first.next_cursor!)}`);
    expect(second.captures.map((capture) => capture.session_id)).toEqual([rich]);
    expect(second.next_cursor).toBeNull();
    for (const status of ["accepted", "failed"]) {
      const page = await fixtureRequest<CommitPage>(`/dashboard/api/commits?capture_status=${status}`);
      const capture = page.commits[0].captures[0];
      await expect(fixtureRequest(`/dashboard/api/sessions/${capture.session_id}/git-artifacts/${capture.commit_sha}/patch`)).rejects.toThrow();
    }
  });
});
