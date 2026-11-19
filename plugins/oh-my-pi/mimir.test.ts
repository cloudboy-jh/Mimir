import { afterEach, describe, expect, mock, test } from "bun:test";
import * as childProcess from "node:child_process";
import { EventEmitter } from "node:events";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";

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
const originalEnv = { MIMIR_URL: process.env.MIMIR_URL, MIMIR_TOKEN: process.env.MIMIR_TOKEN, MIMIR_BIN: process.env.MIMIR_BIN, MIMIR_HOME: process.env.MIMIR_HOME, MIMIR_SESSION_ID: process.env.MIMIR_SESSION_ID };
const originalSessionID = originalEnv.MIMIR_SESSION_ID;

afterEach(() => {
  globalThis.fetch = originalFetch;
  spawned.length = 0;
  for (const [key, value] of Object.entries(originalEnv)) {
    if (value === undefined) delete process.env[key]; else process.env[key] = value;
  }
});

type Handler = (...args: never[]) => unknown;
type CapturedRequest = { url: string; body: unknown; authorization?: string };
type RegisteredProvider = { baseUrl: string; apiKey: string; headers: Record<string, string>; authHeader: boolean };

function createHarness(respond?: (url: string) => number, exec = async (_command: string, _args: string[]) => ({ code: 1, stdout: "" })) {
  process.env.MIMIR_URL = "https://mimir.test";
  process.env.MIMIR_TOKEN = "token";
  process.env.MIMIR_BIN = "/opt/mimir/bin/mimir";
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
    exec,
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

const confirmation = "[main 1a2b3c4] test commit\n 1 file changed";
const turnMessage = { role: "assistant", provider: "openrouter", model: "test", content: [{ type: "toolCall", id: "1", name: "bash", arguments: { command: "git commit -m 'test commit'" } }] };
const ctx = (id: string, cwd = "/work/repo") => ({ cwd, sessionManager: { getSessionId: () => id, buildSessionContext: () => ({ messages: [] }) } });

function eventKinds(requests: CapturedRequest[], sessionID: string): unknown[] {
  return requests
    .filter((request) => request.url.endsWith(`/sessions/${sessionID}/events`))
    .map((request) => {
      const body = request.body;
      return body && typeof body === "object" && "kind" in body ? body.kind : undefined;
    });
}

describe("repo attribution", () => {
  test("repo is the git top-level for subdirectories and absent outside git", async () => {
    const repo = mkdtempSync(join(tmpdir(), "mimir-omp-git-"));
    const outside = mkdtempSync(join(tmpdir(), "mimir-omp-no-git-"));
    try {
      childProcess.execFileSync("git", ["-C", repo, "init", "-q"]);
      const nested = join(repo, "worker", "src");
      mkdirSync(nested, { recursive: true });
      const harness = createHarness(undefined, async (command, args) => {
        try { return { code: 0, stdout: childProcess.execFileSync(command, args, { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }) }; }
        catch { return { code: 1, stdout: "" }; }
      });
      await harness.invoke("session_start", {}, { cwd: nested, sessionManager: { getSessionId: () => "in-git" } });
      expect(harness.providers.at(-1)?.headers["x-mimir-repo"]).toBe(basename(repo));
      await harness.invoke("session_start", {}, { cwd: outside, sessionManager: { getSessionId: () => "no-git" } });
      expect(harness.providers.at(-1)?.headers["x-mimir-session"]).toBe("no-git");
      expect(harness.providers.at(-1)?.headers).not.toHaveProperty("x-mimir-repo");
      await harness.invoke("session_shutdown", {});
    } finally {
      rmSync(repo, { recursive: true, force: true });
      rmSync(outside, { recursive: true, force: true });
    }
  });
});

describe("provider registration", () => {
  test("routes OpenRouter through Mimir's /v1 base with an Authorization header", () => {
    const harness = createHarness();
    expect(harness.providers.at(-1)).toMatchObject({ baseUrl: "https://mimir.test/v1", apiKey: "token", authHeader: true });
  });
});

describe("commit capture through mimir _hook git", () => {
  test("resolves the Mimir binary from MIMIR_BIN, then config executable, then PATH", () => {
    const home = mkdtempSync(join(tmpdir(), "mimir-omp-home-"));
    try {
      process.env.MIMIR_HOME = home;
      process.env.MIMIR_BIN = "/opt/explicit/mimir";
      expect(__testing.mimirExecutable()).toBe("/opt/explicit/mimir");
      delete process.env.MIMIR_BIN;
      expect(__testing.mimirExecutable()).toBe("mimir");
      writeFileSync(join(home, "config"), 'url = "https://mimir.example"\nexecutable=C:\\Tools\\mimir.exe\r\n');
      expect(__testing.mimirExecutable()).toBe("C:\\Tools\\mimir.exe");
    } finally { rmSync(home, { recursive: true, force: true }); }
  });

  test("extracts commit confirmations only from successful shell results", () => {
    expect(__testing.commitConfirmations([{ toolName: "bash", content: [{ type: "text", text: `lint ok\n${confirmation}` }] }])).toBe("[main 1a2b3c4] test commit");
    expect(__testing.commitConfirmations([{ toolName: "bash", isError: true, content: confirmation }])).toBeNull();
    expect(__testing.commitConfirmations([{ toolName: "read", content: confirmation }])).toBeNull();
    expect(__testing.commitConfirmations([{ toolName: "bash", content: "nothing to commit" }])).toBeNull();
  });

  test("hands commit output and the session-end refresh to the exact session", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", {}, ctx("exact-git"));
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx("exact-git"));
    await harness.invoke("turn_end", { turnIndex: 0, message: turnMessage, toolResults: [{ toolCallId: "1", toolName: "bash", content: confirmation }] });
    expect(spawned).toEqual([{
      command: "/opt/mimir/bin/mimir",
      args: ["_hook", "git"],
      input: { session_id: "exact-git", cwd: "/work/repo", harness: "oh-my-pi", tool_output: "[main 1a2b3c4] test commit" },
    }]);
    await harness.invoke("session_switch", {}, ctx("second"));
    expect(spawned.at(-1)?.input).toEqual({ session_id: "exact-git", cwd: "/work/repo", harness: "oh-my-pi", refresh: true });
    expect(harness.requests.some((request) => request.url.includes("/git-artifacts"))).toBe(false);
    await harness.invoke("session_shutdown", {});
    expect(spawned).toHaveLength(2);
  });

  test("missing session ID prevents stale turn capture", async () => {
    const harness = createHarness();
    await harness.invoke("session_start", {}, ctx("old"));
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx("old"));
    await harness.invoke("session_switch", {}, { cwd: "/work/repo", sessionManager: { getSessionId: () => undefined } });
    await harness.invoke("turn_end", { turnIndex: 0, message: { ...turnMessage, provider: "anthropic" }, toolResults: [{ toolCallId: "1", toolName: "bash", content: confirmation }] });
    expect(harness.providers.at(-1)?.headers["x-mimir-session"]).toBeUndefined();
    expect(process.env.MIMIR_SESSION_ID).toBe(originalSessionID);
    expect(harness.requests.filter((request) => request.url.endsWith("/exchanges"))).toHaveLength(0);
    expect(eventKinds(harness.requests, "old")).toEqual(["heartbeat", "end"]);
    expect(spawned.map((call) => call.input)).toEqual([{ session_id: "old", cwd: "/work/repo", harness: "oh-my-pi", refresh: true }]);
    await harness.invoke("session_shutdown", {});
  });

  test("a failed Mimir binary never interrupts the host", async () => {
    const harness = createHarness();
    process.env.MIMIR_BIN = "/missing/mimir";
    await harness.invoke("session_start", {}, ctx("failing"));
    await harness.invoke("turn_start", { turnIndex: 0, timestamp: Date.now() }, ctx("failing"));
    await harness.invoke("turn_end", { turnIndex: 0, message: turnMessage, toolResults: [{ toolCallId: "1", toolName: "bash", content: confirmation }] });
    await harness.invoke("session_shutdown", {});
    expect(spawned.map((call) => call.command)).toEqual(["/missing/mimir", "/missing/mimir"]);
    expect(eventKinds(harness.requests, "failing")).toEqual(["heartbeat", "end"]);
  });
});

describe("Oh My Pi extension", () => {
  test("canonicalizes unsafe session IDs with an OMP-specific prefix", () => {
    expect(__testing.sessionID("valid-session")).toBe("valid-session");
    expect(__testing.sessionID("unsafe session")).toMatch(/^oh-my-pi-[0-9a-f]{32}$/);
  });

  test("exports the exact session ID for the CLI and restores the inherited value", async () => {
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
    expect(spawned).toEqual([]);
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
