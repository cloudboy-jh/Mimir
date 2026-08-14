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

describe("Oh My Pi extension", () => {
  test("canonicalizes unsafe session IDs with an OMP-specific prefix", () => {
    expect(__testing.sessionID("valid-session")).toBe("valid-session");
    expect(__testing.sessionID("unsafe session")).toMatch(/^oh-my-pi-[0-9a-f]{32}$/);
  });

  test("registers exact session headers and sends the initial heartbeat", async () => {
    process.env.MIMIR_URL = "https://mimir.test";
    process.env.MIMIR_TOKEN = "token";
    const requests: Array<{ url: string; body: any }> = [];
    globalThis.fetch = (async (input: string | URL | Request, init?: RequestInit) => {
      requests.push({ url: String(input), body: init?.body ? JSON.parse(String(init.body)) : null });
      return new Response("{}", { status: 200 });
    }) as typeof fetch;
    const handlers = new Map<string, (...args: any[]) => any>();
    const providers: any[] = [];
    const pi = {
      on: (name: string, handler: (...args: any[]) => any) => handlers.set(name, handler),
      registerProvider: (_name: string, value: any) => providers.push(value),
      getSessionName: () => "OMP test",
      exec: async () => ({ code: 1, stdout: "" }),
    };
    extension(pi);
    await handlers.get("session_start")?.({}, { cwd: "C:/repo", sessionManager: { getSessionId: () => "omp-session" } });
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(providers.at(-1).headers).toMatchObject({ "x-mimir-session": "omp-session", "x-mimir-harness": "oh-my-pi" });
    expect(requests.some((request) => request.url.endsWith("/sessions/omp-session/events") && request.body.kind === "heartbeat")).toBe(true);
    await handlers.get("session_shutdown")?.({});
  });
});
