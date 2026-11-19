import { afterEach, describe, expect, mock, test } from "bun:test";
import { basename, join } from "node:path";
import * as childProcess from "node:child_process";
import { EventEmitter } from "node:events";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";

type SpawnCall = { command: string; args: string[]; input: unknown };
const spawned: SpawnCall[] = [];
mock.module("node:child_process", () => ({
  ...childProcess,
  spawn: (command: string, args: string[]) => {
    const child = Object.assign(new EventEmitter(), {
      stdin: Object.assign(new EventEmitter(), {
        end: (data: string) => { spawned.push({ command, args, input: JSON.parse(data) }); },
      }),
      kill: () => true,
      unref: () => {},
    });
    // A missing binary surfaces asynchronously, as real spawn reports ENOENT.
    if (command === "/missing/mimir") queueMicrotask(() => child.emit("error", new Error("spawn ENOENT")));
    return child;
  },
}));
// The extension must load after spawn is mocked, so a static import cannot be used.
const { default: extension, __testing } = await import("./mimir");

const originalFetch = globalThis.fetch;
const originalURL = process.env.MIMIR_URL;
const originalToken = process.env.MIMIR_TOKEN;
const originalBin = process.env.MIMIR_BIN;
const originalSessionID = process.env.MIMIR_SESSION_ID;

afterEach(() => {
  globalThis.fetch = originalFetch;
  spawned.length = 0;
  if (originalURL === undefined) delete process.env.MIMIR_URL; else process.env.MIMIR_URL = originalURL;
  if (originalToken === undefined) delete process.env.MIMIR_TOKEN; else process.env.MIMIR_TOKEN = originalToken;
  if (originalBin === undefined) delete process.env.MIMIR_BIN; else process.env.MIMIR_BIN = originalBin;
  if (originalSessionID === undefined) delete process.env.MIMIR_SESSION_ID; else process.env.MIMIR_SESSION_ID = originalSessionID;
});

type Handler = (...args: never[]) => unknown;
type CapturedRequest = { url: string; body: unknown; headers: Headers };
type Exec = (command: string, args: string[]) => Promise<{ code: number; stdout: string }>;

function createHarness(exec: Exec = async () => ({ code: 1, stdout: "" })) {
  process.env.MIMIR_URL = "https://mimir.test";
  process.env.MIMIR_TOKEN = "token";
  process.env.MIMIR_BIN = "/opt/mimir/bin/mimir";
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

function repository() {
  const cwd = mkdtempSync(join(tmpdir(), "pi-repo-"));
  childProcess.execFileSync("git", ["-C", cwd, "init", "-q"]);
  return cwd;
}

const turnContext = { sessionManager: { buildSessionContext: () => ({ messages: [] }) } };
const confirmation = "[main 1a2b3c4] capture change\n 1 file changed, 1 insertion(+)";
function commitTurn(output: string, toolName = "bash", isError = false) {
  return {
    message: { role: "assistant", provider: "openrouter", model: "test", content: [{ type: "toolCall", id: "exec-1", name: toolName, arguments: { command: "git commit -m 'capture change'" } }] },
    toolResults: [{ role: "toolResult", toolCallId: "exec-1", toolName, isError, content: [{ type: "text", text: output }] }],
  };
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

  test("resolves the Mimir binary from MIMIR_BIN, then config executable, then PATH", () => {
    const config = join("/home", ".mimir", "config");
    const files = new Map([[config, 'url = "https://mimir.example"\nexecutable=/usr/local/bin/mimir\n']]);
    const read = (path: string) => files.get(path) ?? null;
    expect(__testing.mimirExecutable({ MIMIR_BIN: " /opt/mimir " }, read, "/home")).toBe("/opt/mimir");
    expect(__testing.mimirExecutable({}, read, "/home")).toBe("/usr/local/bin/mimir");
    files.set(join("/custom", "config"), 'executable = "C:\\Tools\\mimir.exe"\r\n');
    expect(__testing.mimirExecutable({ MIMIR_HOME: "/custom" }, read, "/home")).toBe("C:\\Tools\\mimir.exe");
    expect(__testing.mimirExecutable({}, () => null, "/home")).toBe("mimir");
    expect(__testing.parseMimirConfig("other = 1\n")).toEqual({});
  });

  test("extracts commit confirmations only from successful shell results", () => {
    expect(__testing.commitConfirmations(commitTurn(`hook output\n${confirmation}`).toolResults)).toBe("[main 1a2b3c4] capture change");
    expect(__testing.commitConfirmations([{ toolName: "bash", content: "[main (root-commit) 1a2b3c4] first" }])).toBe("[main (root-commit) 1a2b3c4] first");
    expect(__testing.commitConfirmations(commitTurn(confirmation, "bash", true).toolResults)).toBeNull();
    expect(__testing.commitConfirmations(commitTurn(confirmation, "read").toolResults)).toBeNull();
    expect(__testing.commitConfirmations(commitTurn("nothing to commit").toolResults)).toBeNull();
    expect(__testing.commitConfirmations(undefined)).toBeNull();
  });

  test("hands commit output and the session-end refresh to mimir _hook git", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", {}, { cwd: "/work/repo", sessionManager: { getSessionId: () => "pi-exact" } });
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, turnContext);
    await harness.invoke("turn_end", { turnIndex: 0, ...commitTurn(confirmation) });
    await harness.invoke("turn_start", { turnIndex: 1, timestamp: Date.now() }, turnContext);
    await harness.invoke("turn_end", { turnIndex: 1, ...commitTurn("On branch main\nnothing to commit") });
    expect(spawned).toEqual([{
      command: "/opt/mimir/bin/mimir",
      args: ["_hook", "git"],
      input: { session_id: "pi-exact", cwd: "/work/repo", harness: "pi", tool_output: "[main 1a2b3c4] capture change" },
    }]);
    await harness.invoke("session_shutdown", { reason: "shutdown" });
    expect(spawned.at(-1)?.input).toEqual({ session_id: "pi-exact", cwd: "/work/repo", harness: "pi", refresh: true });
    expect(harness.requests.some((request) => request.url.includes("/git-artifacts"))).toBe(false);
  });

  test("missing session ID clears attribution and prevents commit capture and exchanges", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", {}, { cwd: "/work/repo", sessionManager: { getSessionId: () => "old" } });
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, turnContext);
    await harness.invoke("session_start", {}, { cwd: "/work/repo", sessionManager: { getSessionId: () => undefined } });
    await harness.invoke("turn_end", { turnIndex: 0, ...commitTurn(confirmation) });
    expect(await harness.providerHeaders()).toEqual({});
    expect(process.env.MIMIR_SESSION_ID).toBe(originalSessionID);
    expect(harness.requests.filter((item) => item.url.endsWith("/exchanges"))).toHaveLength(0);
    expect(eventKinds(harness.requests, "old")).toEqual(["heartbeat", "end"]);
    expect(spawned.map((call) => call.input)).toEqual([{ session_id: "old", cwd: "/work/repo", harness: "pi", refresh: true }]);
    await harness.invoke("session_shutdown", {});
  });

  test("a failed Mimir binary never interrupts the harness", async () => {
    const harness = createHarness();
    process.env.MIMIR_BIN = "/missing/mimir";
    await harness.invoke("session_start", {}, { cwd: "/work/repo", sessionManager: { getSessionId: () => "pi-error" } });
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, turnContext);
    await harness.invoke("turn_end", { turnIndex: 0, ...commitTurn(confirmation) });
    expect(spawned.map((call) => call.command)).toEqual(["/missing/mimir"]);
    await harness.invoke("session_shutdown", {});
    expect(eventKinds(harness.requests, "pi-error")).toEqual(["heartbeat", "end"]);
  });

  test("exports the exact session ID for the CLI and restores the inherited value", async () => {
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

  test("reports harness load provenance from the managed-file manifest", () => {
    expect(__testing.buildHarnessLoad("source", JSON.stringify({
      installation_id: "install-1", bundle_version: "v2.3.4", cli: { version: "2.3.4", commit: "abc123" }, files: { "a": "b" },
    }))).toEqual({
      version: 1, harness: "pi", source_sha256: "41cf6794ba4200b839c53531555f0f3998df4cbb01a4d5cb0b94e3ca5e23947d",
      bundle_version: "v2.3.4", cli_version: "2.3.4", cli_commit: "abc123", installation_id: "install-1",
    });
    expect(__testing.buildHarnessLoad("source", "not json")).toEqual({ version: 1, harness: "pi", source_sha256: "41cf6794ba4200b839c53531555f0f3998df4cbb01a4d5cb0b94e3ca5e23947d" });
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
    expect(spawned).toEqual([]);
  });

  test("activates once when the first real turn starts", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", { reason: "startup" }, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "active-session" },
    });
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, turnContext);
    await harness.invoke("turn_start", { turnIndex: 1, timestamp: Date.now() }, turnContext);

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
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, turnContext);
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
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, turnContext);
    await harness.invoke("session_start", { reason: "new" }, {
      cwd: "C:/repo",
      sessionManager: { getSessionId: () => "draft-b" },
    });

    expect(eventKinds(harness.requests, "active-a")).toEqual(["heartbeat", "end"]);
    expect(eventKinds(harness.requests, "draft-b")).toEqual([]);
    expect(await harness.providerHeaders()).toMatchObject({ "x-mimir-session": "draft-b" });
    expect(spawned.map((call) => call.input)).toEqual([{ session_id: "active-a", cwd: "C:/repo", harness: "pi", refresh: true }]);

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

  test("repo is the git top-level for subdirectories and absent outside git", async () => {
    const cwd = repository();
    const outside = mkdtempSync(join(tmpdir(), "pi-no-git-"));
    try {
      const nested = join(cwd, "worker", "src");
      mkdirSync(nested, { recursive: true });
      const harness = createHarness(async (command, args) => {
        try { return { code: 0, stdout: childProcess.execFileSync(command, args, { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }) }; }
        catch { return { code: 1, stdout: "" }; }
      });
      await harness.invoke("session_start", {}, { cwd: nested, sessionManager: { getSessionId: () => "in-git" } });
      expect((await harness.providerHeaders())["x-mimir-repo"]).toBe(basename(cwd));
      await harness.invoke("session_start", {}, { cwd: outside, sessionManager: { getSessionId: () => "no-git" } });
      const headers = await harness.providerHeaders();
      expect(headers["x-mimir-session"]).toBe("no-git");
      expect(headers).not.toHaveProperty("x-mimir-repo");
      await harness.invoke("session_shutdown", {});
    } finally {
      rmSync(cwd, { recursive: true, force: true });
      rmSync(outside, { recursive: true, force: true });
    }
  });
});
