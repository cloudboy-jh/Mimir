import { describe, expect, it } from "bun:test";
import { __testing } from "./mimir";

const { parseMimirConfig, resolveConnection, resolveMCPCommand, injectMCP, buildTurnEvent, repoName, createActivityTracker, createDeliveryQueue, postEvent } = __testing;

describe("parseMimirConfig", () => {
  it("extracts and normalizes the url", () => {
    expect(parseMimirConfig('url = "https://mimir.example.workers.dev"\n')).toEqual({ url: "https://mimir.example.workers.dev" });
    expect(parseMimirConfig("url = https://mimir.example/\n")).toEqual({ url: "https://mimir.example" });
    expect(parseMimirConfig("other = 1\n")).toEqual({});
  });
});

describe("resolveConnection", () => {
  const files: Record<string, string> = {
    "/home/u/.mimir/config": 'url = "https://mimir.example"\n',
    "/home/u/.mimir/token": "tok-123\n",
  };
  const readFile = (path: string) => files[path.replace(/\\/g, "/")] ?? null;

  it("prefers environment overrides", () => {
    expect(resolveConnection({ MIMIR_URL: "https://env.example/", MIMIR_TOKEN: "env-tok" }, readFile, "/home/u"))
      .toEqual({ url: "https://env.example", token: "env-tok" });
  });

  it("reads the mimir home directory", () => {
    expect(resolveConnection({}, readFile, "/home/u")).toEqual({ url: "https://mimir.example", token: "tok-123" });
    expect(resolveConnection({ MIMIR_HOME: "/home/u/.mimir" }, readFile, undefined)).toEqual({ url: "https://mimir.example", token: "tok-123" });
  });

  it("is inert without a complete connection", () => {
    expect(resolveConnection({}, () => null, "/home/u")).toBeNull();
    expect(resolveConnection({ MIMIR_URL: "https://env.example" }, readFile, "/home/u")).toEqual({ url: "https://mimir.example", token: "tok-123" });
  });
});

describe("buildTurnEvent", () => {
  const info = {
    id: "msg-1",
    sessionID: "ses_abc",
    role: "assistant",
    modelID: "openai/gpt-5",
    providerID: "openrouter",
    time: { created: 1_000, completed: 2_500 },
    tokens: { input: 10, output: 4, cache: { read: 5 } },
  };

  it("builds a turn event from a completed assistant message", () => {
    expect(buildTurnEvent(info, "mimir")).toMatchObject({
      version: 1,
      kind: "turn",
      session_id: "ses_abc",
      harness: "opencode",
      repo: "mimir",
      ts: new Date(2_500).toISOString(),
      turn: { exchange_id: "msg-1", model: "openai/gpt-5", provider: "openrouter", request_kind: "primary", usage: { input_tokens: 15, output_tokens: 4 }, latency_ms: 1_500 },
    });
  });

  it("ignores in-progress and non-assistant messages", () => {
    expect(buildTurnEvent({ ...info, time: { created: 1_000 } }, "mimir")).toBeNull();
    expect(buildTurnEvent({ ...info, role: "user" }, "mimir")).toBeNull();
    expect(buildTurnEvent(null, "mimir")).toBeNull();
    expect(buildTurnEvent({ ...info, sessionID: "" }, "mimir")).toBeNull();
  });
});

describe("repoName", () => {
  it("handles posix and windows paths", () => {
    expect(repoName("/home/u/projects/mimir")).toBe("mimir");
    expect(repoName("C:\\Users\\u\\projects\\mimir\\")).toBe("mimir");
    expect(repoName(undefined)).toBeNull();
  });
});

describe("createDeliveryQueue", () => {
  it("retries failed delivery and suppresses the same pending exchange", async () => {
    let attempts = 0;
    const scheduled: Array<() => void> = [];
    const queue = createDeliveryQueue(async () => ++attempts >= 2, (callback) => scheduled.push(callback));
    const event = { version: 1 as const, kind: "turn" as const, session_id: "ses-1", harness: "opencode", ts: new Date().toISOString(), turn: { exchange_id: "msg-1" } };
    queue.deliver(event);
    queue.deliver(event);
    await Bun.sleep(0);
    expect(attempts).toBe(1);
    expect(queue.pending()).toBe(1);
    scheduled.shift()?.();
    await Bun.sleep(0);
    expect(attempts).toBe(2);
    expect(queue.pending()).toBe(0);
  });
});

describe("createActivityTracker", () => {
  it("stops heartbeats when the active session is deleted", () => {
    let now = 1_000;
    const activity = createActivityTracker(() => now);
    activity.touch("ses-1");
    expect(activity.active()).toBe("ses-1");
    activity.clear("ses-1");
    expect(activity.active()).toBeNull();
    activity.touch("ses-2");
    now += 5 * 60_000;
    expect(activity.active()).toBeNull();
  });
});

describe("resolveMCPCommand", () => {
  it("loads the receipt-owned binary", () => {
    const readFile = (path: string) => path.replace(/\\/g, "/").endsWith("/.mimir/install-receipt.json")
      ? JSON.stringify({ cli: { path: "C:\\Tools\\mimir.exe" } })
      : null;
    expect(resolveMCPCommand({}, readFile, "/home/u")).toEqual(["C:\\Tools\\mimir.exe", "serve"]);
    expect(resolveMCPCommand({}, () => "not-json", "/home/u")).toBeNull();
  });
});

describe("injectMCP", () => {
  it("adds Mimir without discarding other MCP servers", () => {
    const config = { mcp: { existing: { type: "remote", url: "https://example.test" } } };
    injectMCP(config, ["C:\\Tools\\mimir.exe", "serve"]);
    expect(config.mcp).toEqual({
      existing: { type: "remote", url: "https://example.test" },
      mimir: { type: "local", command: ["C:\\Tools\\mimir.exe", "serve"], enabled: true },
    });
  });
});

describe("postEvent", () => {
  it("treats non-2xx responses and transport failures as unsuccessful", async () => {
    const original = globalThis.fetch;
    const event = { version: 1 as const, kind: "heartbeat" as const, session_id: "ses-1", harness: "opencode", ts: new Date().toISOString() };
    try {
      globalThis.fetch = async () => new Response("nope", { status: 500 });
      expect(await postEvent({ url: "https://mimir.example", token: "tok" }, event)).toBe(false);
      globalThis.fetch = async () => { throw new Error("offline"); };
      expect(await postEvent({ url: "https://mimir.example", token: "tok" }, event)).toBe(false);
      globalThis.fetch = async () => new Response("{}", { status: 200 });
      expect(await postEvent({ url: "https://mimir.example", token: "tok" }, event)).toBe(true);
    } finally {
      globalThis.fetch = original;
    }
  });
});
