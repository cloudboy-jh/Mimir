import { afterEach, describe, expect, test } from "bun:test";
import { join } from "node:path";
import extension, { __testing } from "./mimir";

const originalFetch = globalThis.fetch;
const originalURL = process.env.MIMIR_URL;
const originalToken = process.env.MIMIR_TOKEN;

afterEach(() => {
  globalThis.fetch = originalFetch;
  if (originalURL === undefined) delete process.env.MIMIR_URL; else process.env.MIMIR_URL = originalURL;
  if (originalToken === undefined) delete process.env.MIMIR_TOKEN; else process.env.MIMIR_TOKEN = originalToken;
});

type Handler = (...args: never[]) => unknown;
type CapturedRequest = { url: string; body: unknown };
type Exec = (command: string, args: string[]) => Promise<{ code: number; stdout: string }>;

function createHarness(exec: Exec = async () => ({ code: 1, stdout: "" })) {
  process.env.MIMIR_URL = "https://mimir.test";
  process.env.MIMIR_TOKEN = "token";
  const requests: CapturedRequest[] = [];
  globalThis.fetch = (async (input: string | URL | Request, init?: RequestInit) => {
    requests.push({ url: String(input), body: init?.body ? JSON.parse(String(init.body)) as unknown : null });
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
      usage: { input: 10, cacheRead: 4, output: 3 },
      stopReason: "stop",
    }, [
      { role: "toolResult", toolCallId: "read-1", toolName: "read", content: "loaded" },
      { role: "toolResult", toolCallId: "edit-1", toolName: "edit", isError: true, content: "Error: write failed" },
    ], "Auth fix");
    expect(direct).not.toBeNull();
    expect(direct?.usage).toEqual({ input_tokens: 14, output_tokens: 3 });
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
});
