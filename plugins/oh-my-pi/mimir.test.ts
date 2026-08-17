import { afterEach, describe, expect, test } from "bun:test";
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
type RegisteredProvider = { headers: Record<string, string> };

function createHarness() {
  process.env.MIMIR_URL = "https://mimir.test";
  process.env.MIMIR_TOKEN = "token";
  const requests: CapturedRequest[] = [];
  globalThis.fetch = (async (input: string | URL | Request, init?: RequestInit) => {
    requests.push({ url: String(input), body: init?.body ? JSON.parse(String(init.body)) as unknown : null });
    return new Response("{}", { status: 200 });
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

function eventKinds(requests: CapturedRequest[], sessionID: string): unknown[] {
  return requests
    .filter((request) => request.url.endsWith(`/sessions/${sessionID}/events`))
    .map((request) => {
      const body = request.body;
      return body && typeof body === "object" && "kind" in body ? body.kind : undefined;
    });
}


describe("Oh My Pi extension", () => {
  test("canonicalizes unsafe session IDs with an OMP-specific prefix", () => {
    expect(__testing.sessionID("valid-session")).toBe("valid-session");
    expect(__testing.sessionID("unsafe session")).toMatch(/^oh-my-pi-[0-9a-f]{32}$/);
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
});
