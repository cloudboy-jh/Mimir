import { afterEach, describe, expect, test } from "bun:test";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
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
type CapturedRequest = { url: string; body: unknown; headers: Headers };
type Exec = (command: string, args: string[]) => Promise<{ code: number; stdout: string }>;

function createHarness(exec: Exec = async () => ({ code: 1, stdout: "" })) {
  process.env.MIMIR_URL = "https://mimir.test";
  process.env.MIMIR_TOKEN = "token";
  const requests: CapturedRequest[] = [];
  globalThis.fetch = (async (input: string | URL | Request, init?: RequestInit) => {
    requests.push({ url: String(input), body: init?.body ? JSON.parse(String(init.body)) as unknown : null, headers: new Headers(init?.headers) });
    return new Response("{}", { status: 200 });
  }) as typeof fetch;
  const handlers = new Map<string, Handler>();
  const pi = {
    on: (name: string, handler: Handler) => { handlers.set(name, handler); },
    registerProvider: () => {},
    getSessionName: () => "Pi test",
    exec,
  };
  extension(pi);
  return {
    requests,
    async invoke(name: string, ...args: unknown[]) {
      const handler = handlers.get(name);
      if (!handler) throw new Error(`missing handler: ${name}`);
      await Reflect.apply(handler, undefined, args);
    },
    async providerHeaders() {
      const event: { headers: Record<string, string> } = { headers: {} };
      await this.invoke("before_provider_headers", event, { model: { provider: "openrouter" } });
      return event.headers;
    },
  };
}

function git(cwd: string, ...args: string[]): string {
  return execFileSync("git", ["-C", cwd, ...args], { encoding: "utf8" }).trim();
}

function repository() {
  const cwd = mkdtempSync(join(tmpdir(), "pi-artifact-"));
  git(cwd, "init", "-q");
  git(cwd, "config", "user.name", "Pi Test");
  git(cwd, "config", "user.email", "pi@example.test");
  writeFileSync(join(cwd, "first.txt"), "initial\n");
  git(cwd, "add", ".");
  git(cwd, "commit", "-qm", "initial");
  return cwd;
}

function commit(cwd: string, content: string) {
  writeFileSync(join(cwd, "change.txt"), content);
  git(cwd, "add", ".");
  const output = git(cwd, "commit", "-m", "capture change");
  return { sha: git(cwd, "rev-parse", "HEAD"), output };
}

const turnContext = { sessionManager: { buildSessionContext: () => ({ messages: [] }) } };
function result(shaOutput: string, command = "git add . && git commit -m 'capture change'", isError = false) {
  return { message: { role: "assistant", provider: "openrouter", model: "test", content: [{ type: "toolCall", id: "exec-1", name: "bash", arguments: { command } }] }, toolResults: [{ role: "toolResult", toolCallId: "exec-1", toolName: "bash", isError, content: [{ type: "text", text: shaOutput }] }] };
}

function eventKinds(requests: CapturedRequest[], sessionID: string): unknown[] {
  return requests
    .filter((request) => request.url.endsWith(`/sessions/${sessionID}/events`))
    .map((request) => {
      const body = request.body;
      return body && typeof body === "object" && "kind" in body ? body.kind : undefined;
    });
}

describe("Pi Mimir extension", () => {
  test("resolves connection without exposing credentials in the extension", () => {
    const files = new Map([
      [join("/home", ".mimir", "config"), 'url = "https://mimir.example/"\n'],
      [join("/home", ".mimir", "token"), "machine-token\n"],
    ]);
    expect(__testing.resolveConnection({}, (path) => files.get(path) ?? null, "/home")).toEqual({
      url: "https://mimir.example",
      token: "machine-token",
    });
    expect(__testing.resolveConnection({ MIMIR_URL: "https://env.example/", MIMIR_TOKEN: "env-token" }, () => null, "/home")).toEqual({
      url: "https://env.example",
      token: "env-token",
    });
  });

  test("exports the exact session ID for CLI outcomes and restores the inherited value", async () => {
    process.env.MIMIR_SESSION_ID = "inherited-session";
    const harness = createHarness();
    await harness.invoke("session_start", { reason: "startup" }, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "first-session" },
    });
    expect(process.env.MIMIR_SESSION_ID).toBe("first-session");

    await harness.invoke("session_start", { reason: "new" }, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "second-session" },
    });
    expect(process.env.MIMIR_SESSION_ID).toBe("second-session");

    await harness.invoke("session_shutdown", { reason: "shutdown" });
    expect(process.env.MIMIR_SESSION_ID).toBe("inherited-session");
  });

  test("builds bounded direct-provider exchanges and skips OpenRouter", () => {
    const snapshot = {
      startedAt: Date.now() - 25,
      request: { messages: [{ role: "user", content: "fix the auth race" }] },
    };
    const direct = __testing.buildExchange("session-1", 2, snapshot, {
      role: "assistant",
      provider: "anthropic",
      model: "claude-sonnet",
      timestamp: Date.now(),
      content: [
        { type: "toolCall", id: "read-1", name: "read", arguments: { path: "src/auth.ts" } },
        { type: "toolCall", id: "edit-1", name: "edit", arguments: { path: "src/auth.ts" } },
      ],
      usage: { input: 10, cacheRead: 4, cacheWrite: 2, output: 3 },
      stopReason: "stop",
    }, [
      { role: "toolResult", toolCallId: "read-1", toolName: "read", content: "loaded" },
      { role: "toolResult", toolCallId: "edit-1", toolName: "edit", isError: true, content: "Error: write failed" },
    ], "Auth fix");
    expect(direct).not.toBeNull();
    expect(direct?.usage).toEqual({ input_tokens: 10, output_tokens: 3, cache_read_tokens: 4, cache_write_tokens: 2 });
    expect(direct?.request_kind).toBe("primary");
    expect(direct?.title).toBe("Auth fix");
    expect(direct?.tool_activity).toEqual([
      { name: "read", input: { path: "src/auth.ts" }, status: "succeeded", output: "loaded" },
      { name: "edit", input: { path: "src/auth.ts" }, status: "failed", output: "Error: write failed" },
    ]);

    expect(__testing.buildExchange("session-1", 2, snapshot, {
      role: "assistant",
      provider: "openrouter",
      model: "anthropic/claude-sonnet",
      timestamp: Date.now(),
      content: [],
    }, [])).toBeNull();
  });

  test("bounds strings and canonicalizes unsafe session IDs", () => {
    const value = "x".repeat(100_000);
    expect(new TextEncoder().encode(__testing.boundedString(value)).byteLength).toBeLessThanOrEqual(64 * 1024);
    expect(__testing.canonicalSessionID("safe-session:1")).toBe("safe-session:1");
    expect(__testing.canonicalSessionID("unsafe session")).toMatch(/^pi-[a-f0-9]{32}$/);
  });

  test("delivery queue retries and deduplicates", async () => {
    let calls = 0;
    const scheduled: Array<() => void> = [];
    const queue = __testing.createDeliveryQueue(
      async () => ++calls >= 2,
      (callback) => { scheduled.push(callback); return {}; },
    );
    queue.deliver("same", "/events", {});
    queue.deliver("same", "/events", {});
    await Bun.sleep(0);
    expect(calls).toBe(1);
    expect(queue.pending()).toBe(1);
    scheduled.shift()?.();
    await Bun.sleep(0);
    expect(calls).toBe(2);
    expect(queue.pending()).toBe(0);
  });

  test("configures exact headers without activating an idle session", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", { reason: "startup" }, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "draft-session" },
    });

    expect(await harness.providerHeaders()).toMatchObject({
      "x-mimir-session": "draft-session",
      "x-mimir-harness": "pi",
    });
    expect(eventKinds(harness.requests, "draft-session")).toEqual([]);

    await harness.invoke("session_shutdown", { reason: "shutdown" });
    expect(eventKinds(harness.requests, "draft-session")).toEqual([]);
  });

  test("activates once when the first real turn starts", async () => {
    const harness = createHarness();
    const context = { sessionManager: { buildSessionContext: () => ({ messages: [] }) } };
    await harness.invoke("session_start", { reason: "startup" }, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "active-session" },
    });
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, context);
    await harness.invoke("turn_start", { turnIndex: 1, timestamp: Date.now() }, context);

    expect(eventKinds(harness.requests, "active-session")).toEqual(["heartbeat"]);

    await harness.invoke("session_shutdown", { reason: "shutdown" });
    expect(eventKinds(harness.requests, "active-session")).toEqual(["heartbeat", "end"]);
  });

  test("same-ID reload preserves activity without creating another session", async () => {
    const harness = createHarness();
    const sessionContext = {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "reload-session" },
    };
    await harness.invoke("session_start", { reason: "startup" }, sessionContext);
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, {
      sessionManager: { buildSessionContext: () => ({ messages: [] }) },
    });
    await harness.invoke("session_start", { reason: "reload" }, sessionContext);

    expect(await harness.providerHeaders()).toMatchObject({ "x-mimir-session": "reload-session" });
    expect(eventKinds(harness.requests, "reload-session")).toEqual(["heartbeat", "heartbeat"]);

    await harness.invoke("session_shutdown", { reason: "shutdown" });
    expect(eventKinds(harness.requests, "reload-session")).toEqual(["heartbeat", "heartbeat", "end"]);
  });

  test("switching sessions ends only active work", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", { reason: "startup" }, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "active-a" },
    });
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, {
      sessionManager: { buildSessionContext: () => ({ messages: [] }) },
    });
    await harness.invoke("session_start", { reason: "new" }, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "draft-b" },
    });

    expect(eventKinds(harness.requests, "active-a")).toEqual(["heartbeat", "end"]);
    expect(eventKinds(harness.requests, "draft-b")).toEqual([]);
    expect(await harness.providerHeaders()).toMatchObject({ "x-mimir-session": "draft-b" });

    await harness.invoke("session_shutdown", { reason: "shutdown" });
    expect(eventKinds(harness.requests, "draft-b")).toEqual([]);
  });

  test("stale async initialization cannot replace the current session", async () => {
    let releaseOld!: () => void;
    let markOldStarted!: () => void;
    const oldMetadata = new Promise<void>((resolve) => { releaseOld = resolve; });
    const oldStarted = new Promise<void>((resolve) => { markOldStarted = resolve; });
    const harness = createHarness(async (_command, args) => {
      if (args[1] === "C:/old") {
        markOldStarted();
        await oldMetadata;
      }
      return { code: 1, stdout: "" };
    });
    const oldStart = harness.invoke("session_start", { reason: "startup" }, {
      cwd: "C:/old",
      sessionManager: { getSessionId: () => "stale-session" },
    });
    await oldStarted;
    await harness.invoke("session_start", { reason: "new" }, {
      cwd: "C:/current",
      sessionManager: { getSessionId: () => "current-session" },
    });
    releaseOld();
    await oldStart;

    expect(await harness.providerHeaders()).toMatchObject({ "x-mimir-session": "current-session" });
    expect(eventKinds(harness.requests, "stale-session")).toEqual([]);
    expect(eventKinds(harness.requests, "current-session")).toEqual([]);

    await harness.invoke("session_shutdown", { reason: "shutdown" });
  });

  test("captures a successful Pi commit with redacted bounded patch, ISO date and exact session", async () => {
    const cwd = repository();
    try {
      const harness = createHarness();
      await harness.invoke("session_start", {}, { cwd, sessionManager: { getSessionId: () => "pi-exact" } });
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, turnContext);
      const { sha, output } = commit(cwd, "api_key=supersecret\nBearer supersecret\n");
      await harness.invoke("turn_end", { turnIndex: 0, ...result(output) });
      const uploads = harness.requests.filter((item) => item.url.endsWith("/sessions/pi-exact/git-artifacts"));
      expect(uploads).toHaveLength(1);
      expect(uploads[0]!.headers.get("authorization")).toBe("Bearer token");
      expect(uploads[0]!.headers.get("x-mimir-session")).toBe("pi-exact");
      const body = uploads[0]!.body as { version: number; commits: Array<{ commit_sha: string; committed_at: string; patch: string; provenance: string }> };
      expect(body.version).toBe(1);
      expect(body.commits[0]!.commit_sha).toBe(sha);
      expect(body.commits[0]!.provenance).toBe("pi");
      expect(body.commits[0]!.committed_at).toMatch(/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$/);
      expect(body.commits[0]!.patch).toContain("api_key=[REDACTED]");
      expect(body.commits[0]!.patch).not.toContain("supersecret");
      expect(Buffer.byteLength(body.commits[0]!.patch)).toBeLessThanOrEqual(4 * 1024 * 1024);
      await harness.invoke("session_shutdown", { reason: "shutdown" });
    } finally { rmSync(cwd, { recursive: true, force: true }); }
  });

  test("rejects failed, unrelated and unmatched commit output and oversized patches", async () => {
    const cwd = repository();
    try {
      const harness = createHarness();
      await harness.invoke("session_start", {}, { cwd, sessionManager: { getSessionId: () => "negative" } });
      for (const [index, kind] of ["failed", "unrelated", "unmatched", "large"].entries()) {
        await harness.invoke("turn_start", { turnIndex: index, timestamp: Date.now() }, turnContext);
        const { output } = commit(cwd, kind === "large" ? "x".repeat(5 * 1024 * 1024) : kind);
        const turn = result(output, kind === "unrelated" ? "git status" : undefined, kind === "failed");
        if (kind === "unmatched") turn.toolResults[0]!.content = [{ type: "text", text: "[main deadbee] other commit" }];
        await harness.invoke("turn_end", { turnIndex: index, ...turn });
      }
      expect(harness.requests.filter((item) => item.url.includes("/git-artifacts"))).toHaveLength(0);
      await harness.invoke("session_shutdown", { reason: "shutdown" });
    } finally { rmSync(cwd, { recursive: true, force: true }); }
  });

  test("retries artifact upload and finishes it before switching or shutting down", async () => {
    const cwd = repository();
    try {
      const harness = createHarness();
      const recordedFetch = globalThis.fetch;
      let attempts = 0;
      globalThis.fetch = (async (input: string | URL | Request, init?: RequestInit) => {
        const response = await recordedFetch(input, init);
        if (String(input).endsWith("/git-artifacts") && ++attempts === 1) return new Response("retry", { status: 503 });
        return response;
      }) as typeof fetch;
      await harness.invoke("session_start", {}, { cwd, sessionManager: { getSessionId: () => "old-session" } });
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, turnContext);
      const { output } = commit(cwd, "retry");
      const ending = harness.invoke("turn_end", { turnIndex: 0, ...result(output) });
      await harness.invoke("session_start", {}, { cwd, sessionManager: { getSessionId: () => "new-session" } });
      await ending;
      expect(harness.requests.filter((item) => item.url.endsWith("/sessions/old-session/git-artifacts"))).toHaveLength(2);
      expect(harness.requests.filter((item) => item.url.endsWith("/sessions/new-session/git-artifacts"))).toHaveLength(0);
      expect(harness.requests.findIndex((item) => item.url.endsWith("/sessions/old-session/events") && (item.body as { kind?: string }).kind === "end"))
        .toBeGreaterThan(harness.requests.findLastIndex((item) => item.url.endsWith("/sessions/old-session/git-artifacts")));
      await harness.invoke("session_shutdown", { reason: "shutdown" });
    } finally { rmSync(cwd, { recursive: true, force: true }); }
  });

  test("shutdown waits for in-flight artifact delivery", async () => {
    const cwd = repository();
    try {
      const harness = createHarness();
      await harness.invoke("session_start", {}, { cwd, sessionManager: { getSessionId: () => "closing" } });
      await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, turnContext);
      const { output } = commit(cwd, "closing");
      const ending = harness.invoke("turn_end", { turnIndex: 0, ...result(output) });
      await harness.invoke("session_shutdown", { reason: "shutdown" });
      await ending;
      const artifact = harness.requests.findIndex((item) => item.url.endsWith("/sessions/closing/git-artifacts"));
      const end = harness.requests.findIndex((item) => item.url.endsWith("/sessions/closing/events") && (item.body as { kind?: string }).kind === "end");
      expect(artifact).toBeGreaterThanOrEqual(0);
      expect(end).toBeGreaterThan(artifact);
    } finally { rmSync(cwd, { recursive: true, force: true }); }
  });
});
