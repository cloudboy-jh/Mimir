import { afterEach, describe, expect, test } from "bun:test";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import extension, { __testing } from "./mimir";

const originalFetch = globalThis.fetch;
const originalURL = process.env.MIMIR_URL;
const originalToken = process.env.MIMIR_TOKEN;
const originalSessionID = process.env.MIMIR_SESSION_ID;

afterEach(() => {
  globalThis.fetch = originalFetch;
  if (originalURL === undefined) delete process.env.MIMIR_URL; else process.env.MIMIR_URL = originalURL;
  if (originalToken === undefined) delete process.env.MIMIR_TOKEN; else process.env.MIMIR_TOKEN = originalToken;
  if (originalSessionID === undefined) delete process.env.MIMIR_SESSION_ID; else process.env.MIMIR_SESSION_ID = originalSessionID;
});

type Handler = (...args: never[]) => unknown;
type CapturedRequest = { url: string; body: unknown; authorization?: string };
type RegisteredProvider = { headers: Record<string, string> };

function createHarness(respond?: (url: string) => number) {
  process.env.MIMIR_URL = "https://mimir.test";
  process.env.MIMIR_TOKEN = "token";
  const requests: CapturedRequest[] = [];
  globalThis.fetch = (async (input: string | URL | Request, init?: RequestInit) => {
    requests.push({ url: String(input), body: init?.body ? JSON.parse(String(init.body)) as unknown : null, authorization: new Headers(init?.headers).get("authorization") ?? undefined });
    return new Response("{}", { status: respond?.(String(input)) ?? 200 });
  }) as typeof fetch;
  const handlers = new Map<string, Handler>();
  const providers: RegisteredProvider[] = [];
  const pi = {
    on: (name: string, handler: Handler) => { handlers.set(name, handler); },
    registerProvider: (_name: string, value: RegisteredProvider) => { providers.push(value); },
    getSessionName: () => "OMP test",
    exec: async () => ({ code: 1, stdout: "" }),
  };
  extension(pi);
  return {
    requests,
    providers,
    async invoke(name: string, ...args: unknown[]) {
      const handler = handlers.get(name);
      if (!handler) throw new Error(`missing handler: ${name}`);
      await Reflect.apply(handler, undefined, args);
    },
  };
}

function repository() {
  const cwd = mkdtempSync(join(tmpdir(), "mimir-omp-git-"));
  const run = (...args: string[]) => execFileSync("git", ["-C", cwd, ...args], { encoding: "utf8" }).trim();
  run("init", "-q");
  run("config", "user.email", "test@example.com");
  run("config", "user.name", "Test");
  run("config", "core.autocrlf", "false");
  const commit = (content: string) => {
    writeFileSync(join(cwd, "file.txt"), content);
    run("add", "file.txt");
    run("commit", "-qm", "test commit");
    return run("rev-parse", "HEAD");
  };
  commit("initial\n");
  return { cwd, commit, cleanup: () => rmSync(cwd, { recursive: true, force: true }) };
}

function unbornRepository() {
  const cwd = mkdtempSync(join(tmpdir(), "mimir-omp-root-"));
  const run = (...args: string[]) => execFileSync("git", ["-C", cwd, ...args], { encoding: "utf8" }).trim();
  run("init", "-q");
  run("config", "user.email", "test@example.com");
  run("config", "user.name", "Test");
  run("config", "core.autocrlf", "false");
  const commit = () => {
    writeFileSync(join(cwd, "file.txt"), "api_key=supersecret\n");
    run("add", "file.txt");
    const output = run("commit", "-m", "root commit");
    return { sha: run("rev-parse", "HEAD"), output };
  };
  return { cwd, commit, cleanup: () => rmSync(cwd, { recursive: true, force: true }) };
}

const turnMessage = { role: "assistant", provider: "openrouter", model: "test", content: [{ type: "toolCall", id: "1", name: "bash", arguments: { command: "git commit -m 'test commit'" } }] };
function result(sha: string) { return [{ toolCallId: "1", toolName: "bash", content: `[main ${sha.slice(0, 7)}] test commit\n 1 file changed` }]; }

function eventKinds(requests: CapturedRequest[], sessionID: string): unknown[] {
  return requests
    .filter((request) => request.url.endsWith(`/sessions/${sessionID}/events`))
    .map((request) => {
      const body = request.body;
      return body && typeof body === "object" && "kind" in body ? body.kind : undefined;
    });
}


describe("Oh My Pi extension", () => {
  test("artifact metadata is safe, baseline-observed, and guarded across checkout switches", async () => {
    const repo = repository();
    const git = (...args: string[]) => execFileSync("git", ["-C", repo.cwd, ...args], { encoding: "utf8" }).trim();
    try {
      git("checkout", "-qb", "artifact-test");
      const base = git("rev-parse", "HEAD");
      expect(await __testing.artifactMetadata(repo.cwd)).toEqual({ repository_url: null, ref: "artifact-test" });
      for (const origin of ["git@github.com:owner/repo.git", "https://user:password@github.com/owner/repo.git?token=secret#fragment", "ssh://git@github.com/owner/repo.git"]) {
        git("config", "remote.origin.url", origin);
        expect(await __testing.artifactMetadata(repo.cwd)).toEqual({ repository_url: "https://github.com/owner/repo", ref: "artifact-test" });
      }
      const metadata = await __testing.artifactMetadata(repo.cwd);
      const sha = repo.commit("new\n");
      expect((await __testing.collectCommits(repo.cwd, base, [sha.slice(0, 7)], metadata))[0]).toMatchObject({ commit_sha: sha, ...metadata });
      git("checkout", "-qb", "switched");
      expect((await __testing.collectCommits(repo.cwd, base, [sha.slice(0, 7)], metadata))[0]).toMatchObject({ commit_sha: sha, ref: null });
      git("remote", "set-url", "origin", "https://github.com/other/repo.git");
      expect((await __testing.collectCommits(repo.cwd, base, [sha.slice(0, 7)], metadata))[0]?.repository_url).toBeNull();
      git("checkout", "--detach", "-q");
      const detached = await __testing.artifactMetadata(repo.cwd);
      expect(detached.ref).toBeNull();
      expect((await __testing.collectCommits(repo.cwd, base, [sha.slice(0, 7)], detached))[0]).toMatchObject({ commit_sha: sha, ref: null });
      git("checkout", "--detach", "-q", base);
      expect(await __testing.collectCommits(repo.cwd, sha, [base.slice(0, 7)], metadata)).toEqual([]);
      for (const origin of [null, "C:\\private\\repo", "file:///private/repo"]) expect(__testing.normalizeRemoteUrl(origin)).toBeNull();
    } finally { repo.cleanup(); }
  });
  test("captures an evidenced root commit once under the exact session", async () => {
    const repo = unbornRepository();
    try {
      const harness = createHarness();
      const ctx = { cwd: repo.cwd, sessionManager: { getSessionId: () => "omp-root", buildSessionContext: () => ({ messages: [] }) } };
      await harness.invoke("session_start", {}, ctx);
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx);
      const { sha, output } = repo.commit();
      expect(output).toContain("(root-commit)");
      const turn = { turnIndex: 0, message: turnMessage, toolResults: [{ toolCallId: "1", content: output }] };
      await harness.invoke("turn_end", turn);
      await harness.invoke("turn_end", turn);
      const uploads = harness.requests.filter((request) => request.url.endsWith("/git-artifacts"));
      expect(uploads).toHaveLength(1);
      expect(uploads[0]!.url).toEndWith("/sessions/omp-root/git-artifacts");
      expect(uploads[0]!.body).toMatchObject({ commits: [{ commit_sha: sha, parent_commit_sha: null, provenance: "oh-my-pi" }] });
      expect(JSON.stringify(uploads[0]!.body)).not.toContain("supersecret");
      await harness.invoke("session_shutdown", {});
    } finally { repo.cleanup(); }
  });

  test("does not attribute a root commit without matching successful turn evidence", async () => {
    const repo = unbornRepository();
    try {
      const harness = createHarness();
      const ctx = { cwd: repo.cwd, sessionManager: { getSessionId: () => "omp-root-negative", buildSessionContext: () => ({ messages: [] }) } };
      await harness.invoke("session_start", {}, ctx);
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx);
      const { sha, output } = repo.commit();
      await harness.invoke("turn_end", { turnIndex: 0, message: turnMessage, toolResults: [{ toolCallId: "1", content: output.replace(sha.slice(0, 7), "deadbee") }] });
      await harness.invoke("turn_start", { turnIndex: 1, timestamp: Date.now() }, ctx);
      await harness.invoke("turn_end", { turnIndex: 1, message: turnMessage, toolResults: [{ toolCallId: "1", content: output }] });
      expect(harness.requests.filter((request) => request.url.endsWith("/git-artifacts"))).toHaveLength(0);
      await harness.invoke("session_shutdown", {});
    } finally { repo.cleanup(); }
  });

  test("captures only a newly committed SHA named by successful tool output, with redaction and exact delivery", async () => {
    const repo = repository();
    try {
      execFileSync("git", ["-C", repo.cwd, "checkout", "-qb", "artifact-test"]);
      execFileSync("git", ["-C", repo.cwd, "remote", "add", "origin", "https://user:password@github.com/owner/repo.git?token=secret#fragment"]);
      const harness = createHarness();
      const ctx = { cwd: repo.cwd, sessionManager: { getSessionId: () => "exact-git", buildSessionContext: () => ({ messages: [] }) } };
      await harness.invoke("session_start", {}, ctx);
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx);
      const sha = repo.commit("api_key=supersecret\n");
      await harness.invoke("turn_end", { turnIndex: 0, message: turnMessage, toolResults: [{ toolCallId: "1", toolName: "bash", content: [{ type: "text", text: `[main ${sha.slice(0, 7)}] test commit\n 1 file changed` }] }] });
      await harness.invoke("session_shutdown", {});
      const artifacts = harness.requests.filter((request) => request.url.endsWith("/sessions/exact-git/git-artifacts"));
      expect(artifacts).toHaveLength(1);
      expect(artifacts[0].authorization).toBe("Bearer token");
      expect(artifacts[0].body).toMatchObject({ version: 1, commits: [{ commit_sha: sha, provenance: "oh-my-pi", repository_url: "https://github.com/owner/repo", ref: "artifact-test" }] });
      expect((artifacts[0].body as { commits: Array<{ committed_at: string }> }).commits[0].committed_at).toMatch(/\.\d{3}Z$/);
      expect(JSON.stringify(artifacts[0].body)).not.toContain("supersecret");
    } finally { repo.cleanup(); }
  });

  test("captures git -c commit once with matched output, not a different tool's output", async () => {
    const repo = repository();
    try {
      const harness = createHarness();
      const ctx = { cwd: repo.cwd, sessionManager: { getSessionId: () => "exact", buildSessionContext: () => ({ messages: [] }) } };
      await harness.invoke("session_start", {}, ctx);
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx);
      const sha = repo.commit("changed\n");
      const message = { ...turnMessage, content: [{ ...turnMessage.content[0], arguments: { command: "git -c user.name=Test commit -m test" } }] };
      const turn = { turnIndex: 0, message, toolResults: [{ toolCallId: "other", content: `[main ${sha.slice(0, 7)}] test` }, ...result(sha)] };
      await harness.invoke("turn_end", turn);
      await harness.invoke("turn_end", turn);
      expect(harness.requests.filter((request) => request.url.endsWith("/git-artifacts"))).toHaveLength(1);
      await harness.invoke("session_shutdown", {});
    } finally { repo.cleanup(); }
  });

  test("missing session ID clears headers and prevents stale turn capture", async () => {
    const repo = repository();
    try {
      const harness = createHarness();
      const ctx = { cwd: repo.cwd, sessionManager: { getSessionId: () => "old", buildSessionContext: () => ({ messages: [] }) } };
      await harness.invoke("session_start", {}, ctx);
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx);
      await harness.invoke("session_switch", {}, { cwd: repo.cwd, sessionManager: { getSessionId: () => undefined } });
      const sha = repo.commit("changed\n");
      await harness.invoke("turn_end", { turnIndex: 0, message: { ...turnMessage, provider: "anthropic" }, toolResults: result(sha) });
      expect(harness.providers.at(-1)?.headers["x-mimir-session"]).toBeUndefined();
      expect(process.env.MIMIR_SESSION_ID).toBe(originalSessionID);
      expect(harness.requests.filter((request) => /\/(git-artifacts|exchanges)$/.test(request.url))).toHaveLength(0);
      expect(eventKinds(harness.requests, "old")).toEqual(["heartbeat", "end"]);
      await harness.invoke("session_shutdown", {});
    } finally { repo.cleanup(); }
  });

  test("does not upload a commit with no matching tool output", async () => {
    const repo = repository();
    try {
      const harness = createHarness();
      const ctx = { cwd: repo.cwd, sessionManager: { getSessionId: () => "negative", buildSessionContext: () => ({ messages: [] }) } };
      await harness.invoke("session_start", {}, ctx);
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx);
      const sha = repo.commit("changed\n");
      await harness.invoke("turn_end", { turnIndex: 0, message: { ...turnMessage, content: [{ ...turnMessage.content[0], arguments: { command: "echo git commit" } }] }, toolResults: result(sha) });
      expect(harness.requests.filter((request) => request.url.endsWith("/git-artifacts"))).toHaveLength(0);
      await harness.invoke("session_shutdown", {});
    } finally { repo.cleanup(); }
  });

  test("does not attribute a background commit or one from an earlier turn", async () => {
    const repo = repository();
    try {
      const harness = createHarness();
      const ctx = { cwd: repo.cwd, sessionManager: { getSessionId: () => "git-session", buildSessionContext: () => ({ messages: [] }) } };
      await harness.invoke("session_start", {}, ctx);
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx);
      const sha = repo.commit("changed\n");
      await harness.invoke("turn_end", { turnIndex: 0, message: turnMessage, toolResults: [{ ...result(sha)[0], isError: true }] });
      await harness.invoke("turn_start", { turnIndex: 1, timestamp: Date.now() }, ctx);
      await harness.invoke("turn_end", { turnIndex: 1, message: turnMessage, toolResults: result(sha) });
      await harness.invoke("session_shutdown", {});
      expect(harness.requests.filter((request) => request.url.endsWith("/git-artifacts"))).toHaveLength(0);
    } finally { repo.cleanup(); }
  });

  test("retries artifact delivery through shutdown", async () => {
    const repo = repository();
    try {
      let failures = 0;
      const harness = createHarness((url) => url.endsWith("/git-artifacts") && failures++ < 2 ? 503 : 200);
      const ctx = { cwd: repo.cwd, sessionManager: { getSessionId: () => "retry-git", buildSessionContext: () => ({ messages: [] }) } };
      await harness.invoke("session_start", {}, ctx);
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx);
      const sha = repo.commit("changed\n");
      await harness.invoke("turn_end", { turnIndex: 0, message: turnMessage, toolResults: result(sha) });
      await harness.invoke("session_shutdown", {});
      expect(harness.requests.filter((request) => request.url.endsWith("/git-artifacts"))).toHaveLength(3);
    } finally { repo.cleanup(); }
  });

  test("keeps commit attribution across a switch while a retry is pending", async () => {
    const repo = repository();
    try {
      let failures = 0;
      const harness = createHarness((url) => url.endsWith("/git-artifacts") && failures++ === 0 ? 503 : 200);
      const ctx = (id: string) => ({ cwd: repo.cwd, sessionManager: { getSessionId: () => id, buildSessionContext: () => ({ messages: [] }) } });
      await harness.invoke("session_start", {}, ctx("first"));
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx("first"));
      const sha = repo.commit("changed\n");
      const ending = harness.invoke("turn_end", { turnIndex: 0, message: turnMessage, toolResults: result(sha) });
      await harness.invoke("session_switch", {}, ctx("second"));
      await ending;
      expect(harness.requests.filter((request) => request.url.endsWith("/git-artifacts")).map((request) => request.url)).toEqual([
        "https://mimir.test/sessions/first/git-artifacts", "https://mimir.test/sessions/first/git-artifacts",
      ]);
      await harness.invoke("session_shutdown", {});
    } finally { repo.cleanup(); }
  });

  test("skips patches larger than the collection cap", async () => {
    const repo = repository();
    try {
      const harness = createHarness();
      const ctx = { cwd: repo.cwd, sessionManager: { getSessionId: () => "large", buildSessionContext: () => ({ messages: [] }) } };
      await harness.invoke("session_start", {}, ctx);
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx);
      const sha = repo.commit("x".repeat(5 * 1024 * 1024));
      await harness.invoke("turn_end", { turnIndex: 0, message: turnMessage, toolResults: result(sha) });
      await harness.invoke("session_shutdown", {});
      expect(harness.requests.filter((request) => request.url.endsWith("/git-artifacts"))).toHaveLength(0);
    } finally { repo.cleanup(); }
  });
  test("canonicalizes unsafe session IDs with an OMP-specific prefix", () => {
    expect(__testing.sessionID("valid-session")).toBe("valid-session");
    expect(__testing.sessionID("unsafe session")).toMatch(/^oh-my-pi-[0-9a-f]{32}$/);
  });

  test("exports the exact session ID for CLI outcomes and restores the inherited value", async () => {
    process.env.MIMIR_SESSION_ID = "inherited-session";
    const harness = createHarness();
    await harness.invoke("session_start", {}, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "first-session" },
    });
    expect(process.env.MIMIR_SESSION_ID).toBe("first-session");

    await harness.invoke("session_switch", {}, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "second-session" },
    });
    expect(process.env.MIMIR_SESSION_ID).toBe("second-session");

    await harness.invoke("session_shutdown", {});
    expect(process.env.MIMIR_SESSION_ID).toBe("inherited-session");
  });

  test("configures exact headers without activating a draft session", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", {}, { cwd: "C:/repo", sessionManager: { getSessionId: () => "draft-session" } });

    expect(harness.providers.at(-1)?.headers).toMatchObject({
      "x-mimir-session": "draft-session",
      "x-mimir-harness": "oh-my-pi",
    });
    expect(eventKinds(harness.requests, "draft-session")).toEqual([]);

    await harness.invoke("session_shutdown", {});
    expect(eventKinds(harness.requests, "draft-session")).toEqual([]);
  });

  test("activates once when the first real turn starts", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", {}, { cwd: "C:/repo", sessionManager: { getSessionId: () => "active-session" } });
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, { sessionManager: { buildSessionContext: () => ({ messages: [] }) } });
    expect(eventKinds(harness.requests, "active-session")).toEqual(["heartbeat"]);

    await harness.invoke("turn_start", { turnIndex: 1, timestamp: Date.now() }, { sessionManager: { buildSessionContext: () => ({ messages: [] }) } });
    expect(eventKinds(harness.requests, "active-session")).toEqual(["heartbeat"]);

    await harness.invoke("session_shutdown", {});
    expect(eventKinds(harness.requests, "active-session")).toEqual(["heartbeat", "end"]);
  });

  test("reports canonical tool activity for direct-provider turns", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", {}, { cwd: "C:/repo", sessionManager: { getSessionId: () => "tool-session" } });
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() - 25 }, { sessionManager: { buildSessionContext: () => ({ messages: [{ role: "user", content: "Update src/auth.ts" }] }) } });
    await harness.invoke("turn_end", {
      turnIndex: 0,
      message: {
        role: "assistant",
        provider: "anthropic",
        model: "claude-sonnet",
        timestamp: Date.now(),
        content: [
          { type: "toolCall", id: "read-1", name: "read", arguments: { path: "src/auth.ts" } },
          { type: "toolCall", id: "edit-1", name: "edit", arguments: { path: "src/auth.ts" } },
        ],
        usage: { input: 4, output: 2 },
      },
      toolResults: [
        { toolCallId: "read-1", toolName: "read", content: "loaded" },
        { toolCallId: "edit-1", toolName: "edit", isError: true, content: "Error: write failed" },
      ],
    });
    const exchange = harness.requests.find((captured) => captured.url.endsWith("/sessions/tool-session/exchanges"));
    expect(exchange?.body).toMatchObject({
      request_kind: "primary",
      tool_activity: [
        { name: "read", input: { path: "src/auth.ts" }, status: "succeeded", output: "loaded" },
        { name: "edit", input: { path: "src/auth.ts" }, status: "failed", output: "Error: write failed" },
      ],
    });
  });

  test("switching unused drafts creates no session events", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", {}, { cwd: "C:/repo", sessionManager: { getSessionId: () => "draft-a" } });
    await harness.invoke("session_switch", {}, { cwd: "C:/repo", sessionManager: { getSessionId: () => "draft-b" } });

    expect(eventKinds(harness.requests, "draft-a")).toEqual([]);
    expect(eventKinds(harness.requests, "draft-b")).toEqual([]);
    expect(harness.providers.at(-1)?.headers["x-mimir-session"]).toBe("draft-b");

    await harness.invoke("session_shutdown", {});
    expect(eventKinds(harness.requests, "draft-b")).toEqual([]);
  });

  test("switching an active session ends it before activating the next turn", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", {}, { cwd: "C:/repo", sessionManager: { getSessionId: () => "active-a" } });
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, { sessionManager: { buildSessionContext: () => ({ messages: [] }) } });

    await harness.invoke("session_switch", {}, { cwd: "C:/repo", sessionManager: { getSessionId: () => "active-b" } });
    expect(eventKinds(harness.requests, "active-a")).toEqual(["heartbeat", "end"]);
    expect(eventKinds(harness.requests, "active-b")).toEqual([]);

    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, { sessionManager: { buildSessionContext: () => ({ messages: [] }) } });
    expect(eventKinds(harness.requests, "active-b")).toEqual(["heartbeat"]);

    await harness.invoke("session_shutdown", {});
    expect(eventKinds(harness.requests, "active-b")).toEqual(["heartbeat", "end"]);
  });

  test("retains each agent's exact parent across nested sessions and switches", async () => {
    const harness = createHarness();
    const context = (id: string, parentSessionId: string | null) => ({
      cwd: "C:/repo", parentSessionId,
      sessionManager: { getSessionId: () => id, buildSessionContext: () => ({ messages: [] }) },
    });
    for (const [id, parent] of [["root", null], ["child", "root"], ["grandchild", "child"], ["other-root", null]] as const) {
      await harness.invoke("session_switch", {}, context(id, parent));
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, context(id, parent));
    }
    await harness.invoke("session_shutdown", {});
    const events = harness.requests.filter((request) => request.url.endsWith("/events")).map((request) => request.body);
    expect(events).toMatchObject([
      { kind: "heartbeat", session_id: "root", parent_session_id: null },
      { kind: "end", session_id: "root", parent_session_id: null },
      { kind: "heartbeat", session_id: "child", parent_session_id: "root" },
      { kind: "end", session_id: "child", parent_session_id: "root" },
      { kind: "heartbeat", session_id: "grandchild", parent_session_id: "child" },
      { kind: "end", session_id: "grandchild", parent_session_id: "child" },
      { kind: "heartbeat", session_id: "other-root", parent_session_id: null },
      { kind: "end", session_id: "other-root", parent_session_id: null },
    ]);
  });

  test("recovers exact parents from OMP artifact paths when the host omits parent metadata", async () => {
    const directory = mkdtempSync(join(tmpdir(), "mimir-omp-parent-"));
    try {
      const rootFile = join(directory, "root.jsonl");
      const artifacts = join(directory, "root");
      const childFile = join(artifacts, "Child.jsonl");
      const grandchildFile = join(artifacts, "Child.Grandchild.jsonl");
      mkdirSync(artifacts);
      writeFileSync(rootFile, `{"type":"session","id":"root"}\n`);
      writeFileSync(childFile, `{"type":"title","title":""}\n{"type":"session","id":"child"}\n`);
      writeFileSync(grandchildFile, `{"type":"session","id":"grandchild"}\n`);

      const harness = createHarness();
      const context = (id: string, file: string) => ({
        cwd: "C:/repo",
        sessionManager: {
          getSessionId: () => id,
          getSessionFile: () => file,
          buildSessionContext: () => ({ messages: [] }),
        },
      });
      for (const [id, file] of [["root", rootFile], ["child", childFile], ["grandchild", grandchildFile]] as const) {
        await harness.invoke("session_switch", {}, context(id, file));
        await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, context(id, file));
      }
      await harness.invoke("session_shutdown", {});

      const events = harness.requests
        .filter((request) => request.url.endsWith("/events")
          && request.body !== null
          && typeof request.body === "object"
          && "kind" in request.body
          && request.body.kind === "heartbeat")
        .map((request) => request.body);
      expect(events).toMatchObject([
        { session_id: "root", parent_session_id: null },
        { session_id: "child", parent_session_id: "root" },
        { session_id: "grandchild", parent_session_id: "child" },
      ]);
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });

  test("canonicalizes parent IDs exactly like session IDs and never reports self-parentage", async () => {
    const harness = createHarness();
    for (const [id, parent] of [["child", "unsafe parent"], ["self", "self"]]) {
      await harness.invoke("session_switch", {}, { cwd: "C:/repo", parentSessionId: parent, sessionManager: { getSessionId: () => id } });
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, { sessionManager: { buildSessionContext: () => ({ messages: [] }) } });
    }
    await harness.invoke("session_shutdown", {});
    expect(harness.requests.find((request) => request.url.endsWith("/sessions/child/events"))?.body).toMatchObject({
      parent_session_id: __testing.sessionID("unsafe parent"),
    });
    expect(harness.requests.find((request) => request.url.endsWith("/sessions/self/events"))?.body).toMatchObject({
      parent_session_id: null,
    });
  });
});
