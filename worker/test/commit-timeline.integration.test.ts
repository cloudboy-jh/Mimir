import { describe, expect, it, vi } from "vitest";
import { dashboardRequest, env, request } from "./support";
import { normalizeRepositoryUrl } from "../src/sessions/git-repository";
import repositoryMigration from "../migrations/0021_commit_repository_identity.sql?raw";
const SHA = "a".repeat(40);
const TIME = "2026-08-20T10:00:00.000Z";
const PATCH = "diff --git a/a.ts b/a.ts\n--- a/a.ts\n+++ b/a.ts\n@@ -0,0 +1 @@\n+one\n";
async function seed(id: string, repo: string | null, sha = SHA, digest = "b".repeat(64), capture = "saved", committed: string | null = TIME) {
  await env.DB.prepare("INSERT INTO sessions(id, started_at, boundary, repo, title, work_outcome) VALUES (?, ?, 'header', 'display-label', ?, 'discarded')").bind(id, TIME, `Session ${id}`).run();
  await env.DB.prepare("INSERT INTO session_git_artifacts(session_id, commit_sha, committed_at, subject, repository_url, repository_key, ref, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, created_at) VALUES (?, ?, ?, 'Change reader', ?, ?, 'main', 'git', ?, ?, 20, 1, 1, 0, ?, ?, ?)")
    .bind(id, sha, committed, repo, normalizeRepositoryUrl(repo) ?? `session:${id}`, `sessions/${id}/git/${sha}/${digest}.patch`, digest, capture, TIME, TIME).run();
}

describe("captured commit timeline", () => {
  it("pages repositories from every artifact status beyond the commit page and keeps unknown session scopes distinct", async () => {
    for (let index = 0; index < 28; index++) await seed(`option-${index}`, `https://github.com/owner/repo-${String(index).padStart(2, "0")}`, SHA, "b".repeat(64), index === 27 ? "failed" : "accepted");
    await seed("same-remote", "git@github.com:owner/repo-00.git");
    await seed("unknown-option-a", null);
    await seed("unknown-option-b", null);
    const first = await (await dashboardRequest("/dashboard/api/commits/repositories?limit=25")).json() as {
      repositories: Array<{ repository_key: string; name: string; host: string | null; commit_count: number; capture_count: number; session_id: string | null }>;
      next_cursor: string;
    };
    expect(first.repositories).toHaveLength(25);
    expect(first.repositories[0]).toEqual({ repository_key: "https://github.com/owner/repo-00", name: "owner/repo-00", host: "github.com", commit_count: 1, capture_count: 2, session_id: null });
    const second = await (await dashboardRequest(`/dashboard/api/commits/repositories?limit=25&cursor=${encodeURIComponent(first.next_cursor)}`)).json() as typeof first;
    expect(second.repositories.map((repo) => repo.repository_key)).toEqual([
      "https://github.com/owner/repo-25", "https://github.com/owner/repo-26", "https://github.com/owner/repo-27",
      "session:unknown-option-a", "session:unknown-option-b",
    ]);
    expect(second.repositories.slice(-2).map((repo) => [repo.host, repo.session_id])).toEqual([[null, "unknown-option-a"], [null, "unknown-option-b"]]);
    expect(second.next_cursor).toBeNull();
  });
  it("pages recorded refs for one repository, counts commits rather than captures and includes pending and failed refs", async () => {
    const repo = "https://github.com/owner/refs";
    await seed("ref-main", repo);
    await seed("ref-main-copy", repo);
    await seed("ref-pending", repo, "c".repeat(40), "b".repeat(64), "accepted");
    await seed("ref-failed", repo, "d".repeat(40), "b".repeat(64), "failed");
    await seed("ref-other", "https://gitlab.com/owner/other");
    await env.DB.prepare("UPDATE session_git_artifacts SET ref = 'feature/pending' WHERE session_id = 'ref-pending'").run();
    await env.DB.prepare("UPDATE session_git_artifacts SET ref = 'fix/failed' WHERE session_id = 'ref-failed'").run();
    await env.DB.prepare("UPDATE session_git_artifacts SET ref = 'other-only' WHERE session_id = 'ref-other'").run();
    const refs: Array<{ ref: string; commit_count: number }> = [];
    let cursor: string | null = null;
    do {
      const result = await (await dashboardRequest(`/dashboard/api/commits/refs?repo=${encodeURIComponent(repo)}&limit=1${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`)).json() as { refs: typeof refs; next_cursor: string | null };
      refs.push(...result.refs); cursor = result.next_cursor;
    } while (cursor);
    expect(refs).toEqual([{ ref: "feature/pending", commit_count: 1 }, { ref: "fix/failed", commit_count: 1 }, { ref: "main", commit_count: 1 }]);
    expect((await dashboardRequest("/dashboard/api/commits/refs")).status).toBe(400);
    expect((await dashboardRequest(`/dashboard/api/commits/repositories?cursor=${"x".repeat(4097)}`)).status).toBe(400);
  });
  it("groups equivalent remotes, preserves divergent digests and filters associations without hiding the others", async () => {
    await seed("capture-a", "git@github.com:Owner/Repo.git");
    await seed("capture-b", "https://token@github.com/owner/repo/", SHA, "c".repeat(64), "failed");
    await seed("other-repo", "https://github.com/owner/other", SHA);
    const bucket = vi.spyOn(env.LOGS, "get");
    const response = await dashboardRequest("/dashboard/api/commits?repo=https%3A%2F%2Fgithub.com%2Fowner%2Frepo&capture_status=failed&session=capture-b");
    expect(response.status).toBe(200);
    const result = await response.json() as { commits: Array<{ repository_key: string; capture_count: number; captures: Array<{ session_id: string; patch_sha256: string; capture_status: string; outcome: string }> }> };
    expect(result.commits).toHaveLength(1);
    expect(result.commits[0]).toMatchObject({ repository_key: "https://github.com/owner/repo", capture_count: 2 });
    expect(result.commits[0]!.captures.map((item) => [item.session_id, item.patch_sha256, item.capture_status, item.outcome])).toEqual([["capture-a", "b".repeat(64), "saved", "discarded"], ["capture-b", "c".repeat(64), "failed", "discarded"]]);
    expect(bucket).not.toHaveBeenCalled();
    expect(await env.DB.prepare("SELECT count(*) AS n FROM session_outcome_events").first()).toEqual({ n: 0 });
    expect(await env.DB.prepare("SELECT count(*) AS n FROM sessions WHERE work_outcome <> 'discarded'").first()).toEqual({ n: 0 });
    // Predicates must hold on one association, not independently across a group.
    const impossible = await dashboardRequest("/dashboard/api/commits?session=capture-a&capture_status=failed");
    expect(await impossible.json()).toMatchObject({ commits: [] });
  });
  it("selects only canonical repository identities even when another session has the same display metadata", async () => {
    const repo = "https://github.com/owner/selected";
    await seed("selected-remote", repo);
    await seed("different-remote", "https://github.com/owner/different");
    await seed("unknown-remote", null);
    await env.DB.prepare("UPDATE sessions SET repo = ?").bind(repo).run();
    const response = await dashboardRequest(`/dashboard/api/commits?repo=${encodeURIComponent(repo)}`);
    const result = await response.json() as { commits: Array<{ repository_key: string; captures: Array<{ session_id: string }> }> };
    expect(result.commits.map(commit => [commit.repository_key, commit.captures.map(capture => capture.session_id)])).toEqual([
      [repo, ["selected-remote"]],
    ]);
  });
  it("keeps identical SHAs from unknown repositories separate and paginates tied timestamps without repeats", async () => {
    await seed("unknown-a", null);
    await seed("unknown-b", null);
    await seed("known", "https://github.com/owner/repo");
    const keys: string[] = [];
    let cursor: string | null = null;
    for (let index = 0; index < 3; index++) {
      const response = await dashboardRequest(`/dashboard/api/commits?limit=1${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`);
      const result = await response.json() as { commits: Array<{ repository_key: string }>; next_cursor: string | null };
      keys.push(...result.commits.map((item) => item.repository_key)); cursor = result.next_cursor;
    }
    expect(keys).toEqual(["session:unknown-b", "session:unknown-a", "https://github.com/owner/repo"]);
    expect(cursor).toBeNull();
  });
  it("bounds duplicate associations and exposes every remaining session through an association cursor", async () => {
    for (let index = 0; index < 53; index++) await seed(`capture-${String(index).padStart(3, "0")}`, "https://github.com/owner/repo");
    const response = await dashboardRequest("/dashboard/api/commits");
    const result = await response.json() as { commits: Array<{ captures: Array<{ session_id: string }>; capture_count: number; captures_next_cursor: string }> };
    const group = result.commits[0]!;
    expect(group.captures).toHaveLength(50); expect(group.capture_count).toBe(53);
    const remainder = await dashboardRequest(`/dashboard/api/commits/captures?repo=${encodeURIComponent("https://github.com/owner/repo")}&commit=${SHA}&cursor=${group.captures_next_cursor}`);
    const more = await remainder.json() as { captures: Array<{ session_id: string }>; next_cursor: string | null };
    expect(more.captures.map((item) => item.session_id)).toEqual(["capture-050", "capture-051", "capture-052"]);
    expect(new Set([...group.captures, ...more.captures].map((item) => item.session_id)).size).toBe(53);
    expect(more.next_cursor).toBeNull();
  });
  it("retrieves each selected session's exact divergent patch rather than a representative group patch", async () => {
    for (const [id, patch] of [["exact-a", PATCH], ["exact-b", PATCH.replace("+one", "+variant")]]) {
      await env.DB.prepare("INSERT INTO sessions(id, started_at, boundary) VALUES (?, ?, 'header')").bind(id, TIME).run();
      const uploaded = await request(`/sessions/${id}/git-artifacts`, { method: "POST", headers: { authorization: "Bearer machine-token", "content-type": "application/json" }, body: JSON.stringify({ version: 1, commits: [{ commit_sha: SHA, repository_url: "git@github.com:owner/repo.git", patch }] }) });
      expect(uploaded.status).toBe(201);
      const retrieved = await dashboardRequest(`/dashboard/api/sessions/${id}/git-artifacts/${SHA}/patch`);
      expect(await retrieved.text()).toBe(patch);
    }
    const result = await (await dashboardRequest("/dashboard/api/commits")).json() as { commits: Array<{ captures: Array<{ patch_sha256: string }> }> };
    expect(new Set(result.commits[0]!.captures.map((item) => item.patch_sha256)).size).toBe(2);
  });
  it("uses capture date when commit time is absent and rejects malformed filters and cursors", async () => {
    await seed("pending", null, SHA, "b".repeat(64), "accepted", null);
    const found = await dashboardRequest(`/dashboard/api/commits?from=${TIME}&to=${TIME}&q=Change%20reader&capture_status=accepted`);
    expect(await found.json()).toMatchObject({ commits: [{ committed_at: null, captures: [{ capture_status: "accepted" }] }] });
    for (const query of ["cursor=not-base64", "outcome=unknown", "capture_status=skipped", "from=garbage", "from=2026-09-01&to=2026-08-01"]) expect((await dashboardRequest(`/dashboard/api/commits?${query}`)).status).toBe(400);
  });
  it("moves a repaired repository identity without leaving the capture in its old group", async () => {
    await seed("repair-identity", "https://github.com/owner/wrong");
    const repaired = await request(`/sessions/repair-identity/git-artifacts/${SHA}/repair`, {
      method: "POST",
      headers: { authorization: "Bearer machine-token", "content-type": "application/json" },
      body: JSON.stringify({ expected_digest: "b".repeat(64), artifact: { commit_sha: SHA, repository_url: "ssh://git@github.com/owner/correct.git", patch: PATCH } }),
    });
    expect(repaired.status).toBe(200);
    const oldGroup = await dashboardRequest("/dashboard/api/commits?repo=https%3A%2F%2Fgithub.com%2Fowner%2Fwrong");
    expect(await oldGroup.json()).toMatchObject({ commits: [] });
    const newGroup = await dashboardRequest("/dashboard/api/commits?repo=https%3A%2F%2Fgithub.com%2Fowner%2Fcorrect");
    expect(await newGroup.json()).toMatchObject({ commits: [{ repository_key: "https://github.com/owner/correct", captures: [{ session_id: "repair-identity", capture_status: "saved" }] }] });
  });
  it("backfills canonical credential-free identities for existing remote transports", async () => {
    const remotes = ["git@github.com:Owner/Repo.git", "ssh://git@github.com:22/owner/repo.git", "https://user:secret@github.com/owner/repo.git/?token=secret", "git://github.com/owner/repo.git"];
    for (let index = 0; index < remotes.length; index++) await seed(`legacy-${index}`, remotes[index]!);
    await env.DB.exec("UPDATE session_git_artifacts SET repository_key = ''");
    const backfill = repositoryMigration.slice(repositoryMigration.indexOf(";") + 1, repositoryMigration.indexOf("CREATE INDEX"));
    await env.DB.exec(backfill.replace(/--[^\n]*/g, "").replace(/\n/g, " "));
    const result = await (await dashboardRequest("/dashboard/api/commits")).json() as { commits: Array<{ repository_key: string; capture_count: number }> };
    expect(result.commits).toMatchObject([{ repository_key: "https://github.com/owner/repo", capture_count: 4 }]);
  });
});
