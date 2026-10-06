import { describe, expect, it, vi } from "vitest";
import type { GitArtifact } from "../src/sessions/git-artifacts";
import { addMachineToken, env, request, tokenHash } from "./support";

const sha = "a".repeat(40);
const headers = { authorization: "Bearer owner-token", "content-type": "application/json" };
const base = `/sessions/repair-root/git-artifacts/${sha}`;
const patch = "diff --git a/a b/a\n--- a/a\n+++ b/a\n-old\n+customer-123\n+Bearer secret-token\n";
const artifact = {
  commit_sha: sha, patch, parent_commit_sha: "b".repeat(40),
  committed_at: "2026-08-20T10:00:00.000Z", subject: "Corrected commit",
  repository_url: "https://github.com/example/repo", ref: "refs/heads/main", provenance: "cli",
};

async function seed() {
  await addMachineToken("owner", "owner-token");
  await env.DB.exec(`
    INSERT INTO sessions(id, installation_id, started_at, boundary, work_outcome, outcome_reason) VALUES ('repair-root', 'owner', '2026-08-20T10:00:00.000Z', 'header', 'discarded', 'Kept decision');
    INSERT INTO sessions(id, parent_session_id, started_at, boundary) VALUES ('repair-child', 'repair-root', '2026-08-20T10:00:00.000Z', 'header');
    INSERT INTO session_outcome_events(id, session_id, outcome, source, evidence_json, created_at) VALUES ('decision', 'repair-root', 'discarded', 'agent', '{"note":"original"}', '2026-08-20T10:00:00.000Z');
    INSERT INTO config(key, value) VALUES ('redact.patterns', '["customer-[0-9]+"]');
  `);
  expect((await request("/sessions/repair-root/git-artifacts", {
    method: "POST", headers, body: JSON.stringify({ version: 1, commits: [{ commit_sha: sha, patch: "old patch" }] }),
  })).status).toBe(201);
  return row();
}

async function row() {
  return (await env.DB.prepare("SELECT * FROM session_git_artifacts WHERE session_id = 'repair-root' AND commit_sha = ?")
    .bind(sha).first<GitArtifact>())!;
}

function repair(expected: string, value = artifact, path = base) {
  return request(`${path}/repair`, { method: "POST", headers,
    body: JSON.stringify({ expected_digest: expected, artifact: value }) });
}

describe("Guarded Git artifact repair", () => {
  it("redacts, retains the old object and immutable audit, leaves outcomes unchanged, and safely retries", async () => {
    const old = await seed();
    const sessions = (await env.DB.prepare("SELECT * FROM sessions").all()).results;
    const events = (await env.DB.prepare("SELECT * FROM session_outcome_events").all()).results;
    const result = await repair(old.patch_sha256);
    expect(result.status).toBe(200);
    const body = await result.json<{ artifacts: GitArtifact[]; repaired: { audit_r2_key: string } }>();
    const current = await row();
    expect(body).toMatchObject({ kind: "ok", duplicates: 0, repaired: {
      commit_sha: sha, previous_digest: old.patch_sha256, patch_sha256: current.patch_sha256, duplicate: false,
    } });
    const { patch: _patch, ...inputMetadata } = artifact;
    expect(current).toMatchObject({ ...inputMetadata, capture_status: "saved",
      patch_files: 1, patch_additions: 2, patch_deletions: 1,
      accepted_at: old.accepted_at, created_at: old.created_at, failed_at: null, failure_code: null,
    });
    const redacted = patch.replace("customer-123", "[REDACTED]").replace("secret-token", "[REDACTED]");
    expect(current.patch_sha256).toBe(await tokenHash(redacted));
    expect(current.patch_bytes).toBe(new TextEncoder().encode(redacted).byteLength);
    expect(await (await env.LOGS.get(old.patch_r2_key))!.text()).toBe("old patch");
    const audit = await (await env.LOGS.get(body.repaired.audit_r2_key))!.json();
    expect(audit).toMatchObject({ version: 1, operation: "git-artifact-repair", session_id: "repair-root",
      installation_id: "owner", token_hash: await tokenHash("owner-token"),
      old: { commit_sha: sha, patch_sha256: old.patch_sha256, patch_r2_key: old.patch_r2_key },
      new: { ...inputMetadata, patch_sha256: current.patch_sha256, patch_r2_key: current.patch_r2_key } });
    const readback = await request(`${base}/patch`, { headers });
    expect(readback.status).toBe(200);
    expect(readback.headers.get("x-mimir-patch-sha256")).toBe(current.patch_sha256);
    expect(await readback.text()).toBe(redacted);
    const retry = await repair(old.patch_sha256);
    expect(retry.status).toBe(200);
    expect(await retry.json()).toMatchObject({ duplicates: 1, repaired: { duplicate: true, audit_r2_key: body.repaired.audit_r2_key } });
    expect(await row()).toEqual(current);
    expect((await env.DB.prepare("SELECT * FROM sessions").all()).results).toEqual(sessions);
    expect((await env.DB.prepare("SELECT * FROM session_outcome_events").all()).results).toEqual(events);
    expect((await request("/sessions/repair-root/git-artifacts", { method: "POST", headers,
      body: JSON.stringify({ version: 1, commits: [artifact] }) })).status).toBe(200);
    expect((await request("/sessions/repair-root/git-artifacts", { method: "POST", headers,
      body: JSON.stringify({ version: 1, commits: [{ ...artifact, subject: "Different" }] }) })).status).toBe(409);
    await env.LOGS.delete(body.repaired.audit_r2_key);
    expect((await repair(old.patch_sha256)).status).toBe(409);
    expect((await request("/sessions/repair-root/git-artifacts", { method: "POST", headers,
      body: JSON.stringify({ version: 1, commits: [artifact] }) })).status).toBe(409);
  });

  it("rejects stale digests, wrong SHA, non-root and cross-owner requests", async () => {
    const old = await seed();
    expect((await repair("0".repeat(64))).status).toBe(409);
    expect((await repair(old.patch_sha256, { ...artifact, commit_sha: "b".repeat(40) })).status).toBe(400);
    expect((await repair(old.patch_sha256, artifact, `/sessions/repair-child/git-artifacts/${sha}`)).status).toBe(400);
    await addMachineToken("other", "other-token");
    for (const suffix of ["repair", "patch"]) {
      const response = await request(`${base}/${suffix}`, { method: suffix === "repair" ? "POST" : "GET",
        headers: { ...headers, authorization: "Bearer other-token" },
        ...(suffix === "repair" ? { body: JSON.stringify({ expected_digest: old.patch_sha256, artifact }) } : {}) });
      expect(response.status).toBe(403);
    }
    expect((await request(`${base}/patch`)).status).toBe(401);
    expect((await request(`${base}/repair`, { method: "POST", headers: { ...headers, authorization: "Bearer machine-token" },
      body: JSON.stringify({ expected_digest: old.patch_sha256, artifact }) })).status).toBe(403);
    expect((await repair(old.patch_sha256, artifact, `/sessions/missing/git-artifacts/${sha}`)).status).toBe(404);
    expect(await row()).toEqual(old);
  });

  it("requires explicit root ownership for patch reads through both root and child routes", async () => {
    const old = await seed();
    await addMachineToken("other", "other-token");
    // The child is unassociated; authorization must use its owned root.
    for (const id of ["repair-root", "repair-child"]) {
      const path = `/sessions/${id}/git-artifacts/${sha}/patch`;
      for (const token of ["machine-token", "other-token"]) {
        const get = vi.spyOn(env.LOGS, "get");
        const denied = await request(path, { headers: { authorization: `Bearer ${token}` } });
        expect(denied.status).toBe(403);
        expect(await denied.json()).toEqual({ error: "session belongs to another installation" });
        expect(get).not.toHaveBeenCalled();
        get.mockRestore();
      }
      const allowed = await request(path, { headers });
      expect(allowed.status).toBe(200);
      expect(allowed.headers.get("x-mimir-patch-sha256")).toBe(old.patch_sha256);
      expect(await allowed.text()).toBe("old patch");
    }
  });

  it.each(["patch", "audit", "d1"])("keeps the old row on %s failure and permits retry", async (stage) => {
    const old = await seed();
    const put = env.LOGS.put.bind(env.LOGS);
    if (stage === "d1") {
      await env.DB.exec("CREATE TRIGGER fail_repair BEFORE UPDATE ON session_git_artifacts BEGIN SELECT RAISE(ABORT, 'injected failure'); END;");
    } else {
      vi.spyOn(env.LOGS, "put").mockImplementation(async (key, value, options) => {
        if (stage === "patch" || key.endsWith(".audit.json")) throw new Error("injected failure");
        return put(key, value, options);
      });
    }
    expect((await repair(old.patch_sha256)).status).toBe(503);
    expect(await row()).toEqual(old);
    expect(await (await env.LOGS.get(old.patch_r2_key))!.text()).toBe("old patch");
    vi.restoreAllMocks();
    if (stage === "d1") await env.DB.exec("DROP TRIGGER fail_repair;");
    expect((await repair(old.patch_sha256)).status).toBe(200);
  });

  it("recovers an applied D1 update whose acknowledgement was lost using the prewritten audit", async () => {
    const old = await seed();
    const prepare = env.DB.prepare.bind(env.DB);
    vi.spyOn(env.DB, "prepare").mockImplementation((sql) => {
      const statement = prepare(sql);
      if (/^UPDATE session_git_artifacts\b/.test(sql)) {
        const bind = statement.bind.bind(statement);
        vi.spyOn(statement, "bind").mockImplementation((...values) => {
          const bound = bind(...values);
          const run = bound.run.bind(bound);
          vi.spyOn(bound, "run").mockImplementation(async () => {
            await run();
            throw new Error("lost acknowledgement after commit");
          });
          return bound;
        });
      }
      return statement;
    });
    expect((await repair(old.patch_sha256)).status).toBe(503);
    vi.restoreAllMocks();
    const current = await row();
    expect(current.patch_sha256).not.toBe(old.patch_sha256);
    const retry = await repair(old.patch_sha256);
    expect(retry.status).toBe(200);
    expect(await retry.json()).toMatchObject({ duplicates: 1, repaired: { duplicate: true } });
    expect(await row()).toEqual(current);
    expect(await env.LOGS.head(old.patch_r2_key)).not.toBeNull();
    expect(await env.LOGS.head(`${current.patch_r2_key}.audit.json`)).not.toBeNull();
  });

  it.each(["old patch", patch])("rejects a competing CAS and retains both audits (%s)", async (desiredPatch) => {
    const old = await seed();
    const put = env.LOGS.put.bind(env.LOGS);
    let competing: Response | undefined;
    let entered = false;
    vi.spyOn(env.LOGS, "put").mockImplementation(async (key, value, options) => {
      const object = await put(key, value, options);
      if (key.endsWith(".audit.json") && !entered) {
        entered = true;
        competing = await repair(old.patch_sha256, { ...artifact, patch: desiredPatch, subject: "Winner" });
      }
      return object;
    });
    expect((await repair(old.patch_sha256, { ...artifact, patch: desiredPatch })).status).toBe(409);
    expect(competing!.status).toBe(200);
    expect(await row()).toMatchObject({ subject: "Winner", patch_sha256: await tokenHash(
      desiredPatch.replace("customer-123", "[REDACTED]").replace("secret-token", "[REDACTED]")),
    });
    const audits = (await env.LOGS.list()).objects.filter((object) => object.key.endsWith(".audit.json"));
    expect(audits).toHaveLength(2);
    expect(await env.LOGS.head(old.patch_r2_key)).not.toBeNull();
  });
});
