import { describe, expect, it, vi } from "vitest";
import { addMachineToken, dashboardRequest, env, request } from "./support";

describe("Session Git artifacts", () => {
  it("persists multiline redacted source patches with digests and stats matching retrieved R2 bytes", async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, boundary) VALUES ('redaction-git', '2026-08-20T10:00:00.000Z', 'header');
      INSERT INTO config(key, value) VALUES ('redact.patterns', '["customer-[0-9]+", "["]');
    `);
    const patch = [
      "diff --git a/config.ts b/config.ts", "--- a/config.ts", "+++ b/config.ts", "@@ -1,2 +1,11 @@",
      "-TOKEN=old-secret", '-const apiKey = "old value";',
      "+TOKEN=new-secret", '+const apiKey = "new \\"quoted\\" value";',
      "+const password = 'two words';", '+const path = "C:\\Users\\name\\file.ts";',
      '+const label = "customer-123 café";',
      '+const token = `first second`;', '+password = """first second""";',
      '+const secret =', '+  "next line";',
      '+const apiKey = `first', '+second`;',
      "diff --git a/image.png b/image.png", "index 1234567..abcdef0 100644",
      "Binary files a/image.png and b/image.png differ", "",
    ].join("\n");
    const expected = [
      "diff --git a/config.ts b/config.ts", "--- a/config.ts", "+++ b/config.ts", "@@ -1,2 +1,11 @@",
      "-TOKEN=[REDACTED]", '-const apiKey = "[REDACTED]";',
      "+TOKEN=[REDACTED]", '+const apiKey = "[REDACTED]";',
      "+const password = '[REDACTED]';", '+const path = "C:\\Users\\name\\file.ts";',
      '+const label = "[REDACTED] café";',
      '+const token = `[REDACTED]`;', '+password = """[REDACTED]""";',
      '+const secret =', '+  "[REDACTED]";',
      '+const apiKey = `[REDACTED]', '+[REDACTED]`;',
      "diff --git a/image.png b/image.png", "index 1234567..abcdef0 100644",
      "Binary files a/image.png and b/image.png differ", "",
    ].join("\n");
    const sha = "1".repeat(40);
    const hash = await digest(expected);
    const key = `sessions/redaction-git/git/${sha}/${hash}.patch`;
    const uploaded = await request("/sessions/redaction-git/git-artifacts", {
      method: "POST",
      headers: { authorization: "Bearer machine-token", "content-type": "application/json" },
      body: JSON.stringify({ version: 1, commits: [{ commit_sha: sha, patch }] }),
    });
    expect(uploaded.status).toBe(201);
    const metadata = {
      patch_sha256: hash, patch_r2_key: key, patch_bytes: new TextEncoder().encode(expected).byteLength,
      patch_files: 2, patch_additions: 11, patch_deletions: 2,
    };
    expect(await uploaded.json()).toMatchObject({ artifacts: [{ capture_status: "saved", ...metadata }] });
    expect(await env.DB.prepare(
      "SELECT patch_sha256, patch_r2_key, patch_bytes, patch_files, patch_additions, patch_deletions FROM session_git_artifacts WHERE session_id = 'redaction-git'",
    ).first()).toEqual(metadata);
    const object = await env.LOGS.get(key);
    expect(object).not.toBeNull();
    const stored = await object!.text();
    expect(stored).toBe(expected);
    expect(stored.match(/\r\n|\n|\r/g)).toEqual(patch.match(/\r\n|\n|\r/g));
    expect(stored).not.toMatch(/first|second|next line/);
    expect(await digest(stored)).toBe(hash);
    expect(object!.customMetadata?.sha256).toBe(hash);
    const retrieved = await dashboardRequest(`/dashboard/api/sessions/redaction-git/commits/${sha}/patch`);
    expect(retrieved.status).toBe(200);
    expect(await retrieved.text()).toBe(stored);
  });

  it("preserves legitimate Git binary patch payloads byte for byte", async () => {
    await env.DB.exec("INSERT INTO sessions(id, started_at, boundary) VALUES ('binary-git', '2026-08-20T10:00:00.000Z', 'header')");
    const patch = "diff --git a/file.bin b/file.bin\nnew file mode 100644\nindex 0000000000000000000000000000000000000000..fba23eab21e54f04e017bd5b9e1cbd7d3cfae964\nGIT binary patch\nliteral 4\nLcmZQzWM;W)01f~L\n\nliteral 0\nHcmV?d00001\n\n";
    const sha = "2".repeat(40);
    const hash = await digest(patch);
    const key = `sessions/binary-git/git/${sha}/${hash}.patch`;
    const uploaded = await request("/sessions/binary-git/git-artifacts", {
      method: "POST", headers: { authorization: "Bearer machine-token", "content-type": "application/json" },
      body: JSON.stringify({ version: 1, commits: [{ commit_sha: sha, patch }] }),
    });
    expect(uploaded.status).toBe(201);
    expect(await uploaded.json()).toMatchObject({ artifacts: [{ patch_sha256: hash, patch_r2_key: key,
      patch_bytes: new TextEncoder().encode(patch).byteLength, patch_files: 1, patch_additions: 0, patch_deletions: 0 }] });
    const stored = await (await env.LOGS.get(key))!.text();
    expect(stored).toBe(patch);
    expect(await digest(stored)).toBe(hash);
    expect(await (await dashboardRequest(`/dashboard/api/sessions/binary-git/commits/${sha}/patch`)).text()).toBe(patch);
  });

  it("stores independent redacted root-owned commits idempotently", async () => {
    await addMachineToken("install-a", "machine-a");
    await env.DB.exec(`
      INSERT INTO sessions(id, installation_id, started_at, boundary) VALUES ('git-root', 'install-a', '2026-08-20T10:00:00.000Z', 'header');
      INSERT INTO sessions(id, parent_session_id, started_at, boundary) VALUES ('git-child', 'git-root', '2026-08-20T10:01:00.000Z', 'header');
      INSERT INTO config(key, value) VALUES ('redact.patterns', '["customer-[0-9]+"]');
    `);
    const firstSHA = "a".repeat(40);
    const secondSHA = "b".repeat(40);
    const firstPatch = "diff --git a/a.ts b/a.ts\n--- a/a.ts\n+++ b/a.ts\n@@ -1 +1 @@\n-customer-123\n+Bearer secret-token\n";
    const secondPatch = "diff --git a/b.ts b/b.ts\n--- a/b.ts\n+++ b/b.ts\n@@ -0,0 +1 @@\n+second\n";
    const body = JSON.stringify({
      version: 1,
      commits: [
        {
          commit_sha: firstSHA,
          parent_commit_sha: null,
          committed_at: "2026-08-20T10:02:00.000Z",
          subject: "First commit",
          repository_url: "https://github.com/example/mimir",
          ref: "refs/heads/main",
          patch: firstPatch,
        },
        {
          commit_sha: secondSHA,
          parent_commit_sha: firstSHA,
          committed_at: "2026-08-20T10:03:00.000Z",
          subject: "Second commit",
          repository_url: "https://github.com/example/mimir",
          ref: "refs/heads/main",
          patch: secondPatch,
        },
      ],
    });
    const headers = {
      authorization: "Bearer machine-a",
      "content-type": "application/json",
    };

    const created = await request("/sessions/git-child/git-artifacts", {
      method: "POST",
      headers,
      body,
    });
    expect(created.status).toBe(201);
    expect(await created.json()).toMatchObject({
      kind: "ok",
      session_id: "git-root",
      duplicates: 0,
      artifacts: [
        {
          commit_sha: firstSHA,
          repository_url: "https://github.com/example/mimir",
          ref: "refs/heads/main",
          provenance: "git",
          capture_status: "saved",
          accepted_at: expect.any(String),
          saved_at: expect.any(String),
          failed_at: null,
          failure_code: null,
          patch_files: 1,
          patch_additions: 1,
          patch_deletions: 1,
        },
        { commit_sha: secondSHA, parent_commit_sha: firstSHA },
      ],
    });
    const repeated = await request("/sessions/git-child/git-artifacts", {
      method: "POST",
      headers,
      body,
    });
    expect(await repeated.json()).toMatchObject({ duplicates: 2 });
    expect(
      await env.DB.prepare("SELECT COUNT(*) AS count FROM session_git_artifacts").first(),
    ).toEqual({ count: 2 });

    const detail = (await (
      await request("/api/sessions/git-root", { headers: { authorization: "Bearer machine-a" } })
    ).json()) as { commits: Array<Record<string, unknown>>; session: Record<string, unknown> };
    expect(detail.commits.map((commit) => commit.commit_sha)).toEqual([firstSHA, secondSHA]);
    expect(detail.commits[0]).toEqual({
      session_id: "git-root",
      commit_sha: firstSHA,
      parent_commit_sha: null,
      subject: "First commit",
      ref: "refs/heads/main",
      repository_url: "https://github.com/example/mimir",
      repository_key: "https://github.com/example/mimir",
      committed_at: "2026-08-20T10:02:00.000Z",
      accepted_at: expect.any(String),
      capture_status: "saved",
      pushed: false,
      pushed_at: null,
      patch_sha256: expect.any(String),
      patch_bytes: expect.any(Number),
      patch_files: 1,
      patch_additions: 1,
      patch_deletions: 1,
    });
    expect(detail.session).toMatchObject({ git_status: "committed", commit_count: 2 });
    const childDetail = (await (await dashboardRequest("/dashboard/api/sessions/git-child")).json()) as { session: Record<string, unknown> };
    expect(childDetail.session).toMatchObject({ git_status: "none", commit_count: 0 });

    const patch = await dashboardRequest(
      `/dashboard/api/sessions/git-child/commits/${firstSHA}/patch`,
    );
    expect(patch.status).toBe(200);
    expect(await patch.text()).toBe(
      firstPatch.replace("customer-123", "[REDACTED]").replace("secret-token", "[REDACTED]"),
    );
  });

  it("keeps sibling results and repairs accepted or failed rows on retry", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, boundary) VALUES ('resumable-git', '2026-08-20T10:00:00.000Z', 'header')",
    ).run();
    const failedSHA = "d".repeat(40);
    const savedSHA = "e".repeat(40);
    const acceptedSHA = "f".repeat(40);
    const failedPatch = "failed patch";
    const savedPatch = "saved patch";
    const acceptedPatch = "accepted patch";
    const acceptedDigest = await digest(acceptedPatch);
    const acceptedKey = `sessions/resumable-git/git/${acceptedSHA}/${acceptedDigest}.patch`;
    await env.DB.prepare(
      "INSERT INTO session_git_artifacts(session_id, commit_sha, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, created_at) VALUES ('resumable-git', ?, 'git', ?, ?, ?, 0, 0, 0, 'accepted', '2026-08-20T10:01:00.000Z', '2026-08-20T10:01:00.000Z')",
    )
      .bind(
        acceptedSHA,
        acceptedKey,
        acceptedDigest,
        new TextEncoder().encode(acceptedPatch).byteLength,
      )
      .run();
    const body = JSON.stringify({
      version: 1,
      commits: [
        { commit_sha: failedSHA, patch: failedPatch },
        { commit_sha: savedSHA, patch: savedPatch },
        { commit_sha: acceptedSHA, patch: acceptedPatch },
      ],
    });
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    vi.spyOn(env.LOGS, "put").mockImplementationOnce(async () => {
      expect(
        await env.DB.prepare(
          "SELECT capture_status FROM session_git_artifacts WHERE session_id = 'resumable-git' AND commit_sha = ?",
        )
          .bind(failedSHA)
          .first(),
      ).toEqual({ capture_status: "accepted" });
      throw new Error("injected Git artifact R2 failure");
    });

    const partial = await request("/sessions/resumable-git/git-artifacts", {
      method: "POST",
      headers,
      body,
    });
    expect(partial.status).toBe(503);
    expect(await partial.json()).toMatchObject({
      kind: "partial",
      duplicates: 0,
      failures: [{ commit_sha: failedSHA, failure_code: "r2_put_failed" }],
      artifacts: [
        { commit_sha: failedSHA, capture_status: "failed" },
        { commit_sha: savedSHA, capture_status: "saved" },
        { commit_sha: acceptedSHA, capture_status: "saved" },
      ],
    });
    expect(
      (
        await env.DB.prepare(
          "SELECT commit_sha, capture_status, failure_code FROM session_git_artifacts ORDER BY commit_sha",
        ).all()
      ).results,
    ).toEqual([
      {
        commit_sha: failedSHA,
        capture_status: "failed",
        failure_code: "r2_put_failed",
      },
      { commit_sha: savedSHA, capture_status: "saved", failure_code: null },
      { commit_sha: acceptedSHA, capture_status: "saved", failure_code: null },
    ]);
    expect(
      (
        await dashboardRequest(
          `/dashboard/api/sessions/resumable-git/commits/${failedSHA}/patch`,
        )
      ).status,
    ).toBe(409);
    expect(
      await (
        await dashboardRequest(
          `/dashboard/api/sessions/resumable-git/commits/${savedSHA}/patch`,
        )
      ).text(),
    ).toBe(savedPatch);

    vi.restoreAllMocks();
    const repaired = await request("/sessions/resumable-git/git-artifacts", {
      method: "POST",
      headers,
      body,
    });
    expect(repaired.status).toBe(201);
    expect(await repaired.json()).toMatchObject({
      kind: "ok",
      duplicates: 2,
      artifacts: [
        { commit_sha: failedSHA, capture_status: "saved", failure_code: null },
        { commit_sha: savedSHA, capture_status: "saved" },
        { commit_sha: acceptedSHA, capture_status: "saved" },
      ],
    });
  });

  it("rejects cross-owner, conflicting, malformed, and oversized ingestion", async () => {
    await addMachineToken("install-a", "machine-a");
    await addMachineToken("install-b", "machine-b");
    await env.DB.prepare(
      "INSERT INTO sessions(id, installation_id, started_at, boundary) VALUES ('owned-git', 'install-a', '2026-08-20T10:00:00.000Z', 'header')",
    ).run();
    const sha = "c".repeat(40);
    const artifact = (patch: string) =>
      JSON.stringify({ version: 1, commits: [{ commit_sha: sha, patch }] });
    const ownerHeaders = {
      authorization: "Bearer machine-a",
      "content-type": "application/json",
    };
    expect(
      (
        await request("/sessions/owned-git/git-artifacts", {
          method: "POST",
          headers: { ...ownerHeaders, authorization: "Bearer machine-b" },
          body: artifact("one"),
        })
      ).status,
    ).toBe(403);
    expect(
      (
        await request("/sessions/owned-git/git-artifacts", {
          method: "POST",
          headers: ownerHeaders,
          body: artifact("one"),
        })
      ).status,
    ).toBe(201);
    expect(
      (
        await request("/sessions/owned-git/git-artifacts", {
          method: "POST",
          headers: ownerHeaders,
          body: artifact("two"),
        })
      ).status,
    ).toBe(409);
    expect(
      (
        await request("/sessions/owned-git/git-artifacts", {
          method: "POST",
          headers: ownerHeaders,
          body: JSON.stringify({ version: 1, commits: [] }),
        })
      ).status,
    ).toBe(400);
    expect(
      (
        await request("/sessions/owned-git/git-artifacts", {
          method: "POST",
          headers: { ...ownerHeaders, "content-length": String(6 * 1024 * 1024) },
          body: "{}",
        })
      ).status,
    ).toBe(413);
  });


  it("upgrades a saved commit to pushed, never back, and truncates patch reads on request", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, boundary) VALUES ('pushed-git', '2026-08-20T10:00:00.000Z', 'header')").run();
    const sha = "9".repeat(40);
    const patch = "diff --git a/p.ts b/p.ts\n+pushed\n";
    const post = (pushed?: boolean) =>
      request("/sessions/pushed-git/git-artifacts", {
        method: "POST",
        headers: { authorization: "Bearer machine-token", "content-type": "application/json" },
        body: JSON.stringify({ version: 1, commits: [{ commit_sha: sha, subject: "Push me", patch, ...(pushed === undefined ? {} : { pushed }) }] }),
      });
    const status = async () =>
      ((await (await request("/api/sessions/pushed-git", { headers: { authorization: "Bearer machine-token" } })).json()) as {
        session: { git_status: string };
        commits: Array<{ pushed: boolean; pushed_at: string | null }>;
      });
    expect((await post()).status).toBe(201);
    expect((await status()).session.git_status).toBe("committed");
    const upgraded = await post(true);
    expect(upgraded.status).toBe(200);
    expect(await upgraded.json()).toMatchObject({ duplicates: 1, artifacts: [{ commit_sha: sha, pushed: 1, pushed_at: expect.any(String) }] });
    const pushed = await status();
    expect(pushed.session.git_status).toBe("pushed");
    expect(pushed.commits[0]!.pushed).toBe(true);
    expect((await post(false)).status).toBe(200);
    expect((await status()).commits[0]).toEqual(pushed.commits[0]);
    expect((await post(true)).status).toBe(200);
    expect((await status()).commits[0]!.pushed_at).toBe(pushed.commits[0]!.pushed_at);
    expect(
      (
        await request("/sessions/pushed-git/git-artifacts", {
          method: "POST",
          headers: { authorization: "Bearer machine-token", "content-type": "application/json" },
          body: JSON.stringify({ version: 1, commits: [{ commit_sha: sha, patch, pushed: "yes" }] }),
        })
      ).status,
    ).toBe(400);

    const truncated = await request(`/api/sessions/pushed-git/commits/${sha}/patch?max_bytes=10`, { headers: { authorization: "Bearer machine-token" } });
    expect(truncated.headers.get("x-mimir-patch-truncated")).toBe("true");
    expect(truncated.headers.get("x-mimir-patch-bytes")).toBe(String(new TextEncoder().encode(patch).byteLength));
    expect(truncated.headers.get("x-mimir-patch-sha256")).toBe(await digest(patch));
    expect(await truncated.text()).toBe(patch.slice(0, 10));
    const full = await request(`/api/sessions/pushed-git/commits/${sha}/patch`, { headers: { authorization: "Bearer machine-token" } });
    expect(full.headers.get("x-mimir-patch-truncated")).toBe("false");
    expect(await full.text()).toBe(patch);
    for (const [path, code] of [
      [`/api/sessions/pushed-git/commits/${"8".repeat(40)}/patch`, 404],
      ["/api/sessions/pushed-git/commits/not-a-sha/patch", 400],
      [`/api/sessions/missing/commits/${sha}/patch`, 404],
      [`/api/sessions/pushed-git/commits/${sha}/patch?max_bytes=0`, 400],
    ] as const)
      expect((await request(path, { headers: { authorization: "Bearer machine-token" } })).status).toBe(code);
  });
});

async function digest(value: string) {
  const bytes = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(value),
  );
  return Array.from(new Uint8Array(bytes), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}
