import { describe, expect, it } from "bun:test";
import plugin, { MimirPlugin, __testing } from "./mimir";

const { parseMimirConfig, resolveConnection, resolveMCPCommand, injectMCP, buildTurnEvent, buildDirectExchange, repoName, createActivityTracker, createDeliveryQueue, createDirectExchangeReporter, postEvent, postDirectExchange, buildHarnessLoad, loadHarnessLoad, postHarnessLoad, reportHarnessLoad } = __testing;

describe("plugin exports", () => {
  it("exposes an identified OpenCode server plugin module", () => {
    expect(plugin.id).toBe("mimir");
    expect(typeof plugin.server).toBe("function");
    expect(plugin.server).toBe(MimirPlugin);
  });
});

describe("chat.headers hook", () => {
  it("binds OpenRouter requests to the exact session", async () => {
    const original = { MIMIR_URL: process.env.MIMIR_URL, MIMIR_TOKEN: process.env.MIMIR_TOKEN };
    process.env.MIMIR_URL = "https://mimir.example";
    process.env.MIMIR_TOKEN = "tok";
    const fetch = globalThis.fetch;
    globalThis.fetch = async () => new Response("{}", { status: 200 });
    try {
      const hooks = await plugin.server({ directory: "C:\\repo\\mimir" } as never);
      const headers = { headers: {} as Record<string, string> };
      await hooks["chat.headers"]!({ sessionID: "ses_test", model: { providerID: "openrouter" } } as never, headers);
      expect(headers.headers).toEqual({ "x-mimir-session": "ses_test", "x-mimir-harness": "opencode", "x-mimir-repo": "mimir" });
      const other = { headers: {} as Record<string, string> };
      await hooks["chat.headers"]!({ sessionID: "ses_test", model: { providerID: "anthropic" } } as never, other);
      expect(other.headers).toEqual({});
    } finally {
      globalThis.fetch = fetch;
      process.env.MIMIR_URL = original.MIMIR_URL;
      process.env.MIMIR_TOKEN = original.MIMIR_TOKEN;
    }
  });
});

describe("startup build identity", () => {
  it("posts only source identity and safe receipt provenance to the authenticated integration path", async () => {
    const load = buildHarnessLoad("loaded plugin source", JSON.stringify({
      bundle_version: "v2.3.4",
      installation_id: "install-1",
      cli: { version: "2.3.4", commit: "abc123", path: "/secret/mimir", sha256: "cli-hash" },
      source: "/private/checkout",
      token: "do-not-send",
    }));
    let request: Request | undefined;
    const original = globalThis.fetch;
    try {
      globalThis.fetch = async (input, init) => {
        request = new Request(input, init);
        return new Response("{}", { status: 204 });
      };
      expect(await postHarnessLoad({ url: "https://mimir.example", token: "tok-secret" }, load)).toBe(true);
    } finally {
      globalThis.fetch = original;
    }
    expect(request?.url).toBe("https://mimir.example/integrations/harness-loads");
    expect(request?.method).toBe("POST");
    expect(request?.headers.get("authorization")).toBe("Bearer tok-secret");
    expect(await request?.json()).toEqual({
      version: 1,
      harness: "opencode",
      source_sha256: "1f276ede474cf6948a22d1f3dc41be29d345f91672da261558f922e1826aed59",
      bundle_version: "v2.3.4",
      cli_version: "2.3.4",
      cli_commit: "abc123",
      installation_id: "install-1",
    });
  });

  it("reports source identity when the install receipt is missing", () => {
    const files: Record<string, string> = { "/plugin/mimir.ts": "source" };
    expect(loadHarnessLoad({ MIMIR_HOME: "/mimir" }, (path) => files[path] ?? null, undefined, "/plugin/mimir.ts"))
      .toEqual({ version: 1, harness: "opencode", source_sha256: "41cf6794ba4200b839c53531555f0f3998df4cbb01a4d5cb0b94e3ca5e23947d" });
  });

  it("contains network and non-2xx failures and retries asynchronously", async () => {
    const original = globalThis.fetch;
    try {
      globalThis.fetch = async () => new Response("nope", { status: 503 });
      const load = buildHarnessLoad("source", null);
      expect(await postHarnessLoad({ url: "https://mimir.example", token: "tok" }, load)).toBe(false);
      globalThis.fetch = async () => { throw new Error("offline"); };
      expect(await postHarnessLoad({ url: "https://mimir.example", token: "tok" }, load)).toBe(false);

      let attempts = 0;
      const scheduled: Array<() => void> = [];
      reportHarnessLoad({ url: "https://mimir.example", token: "tok" }, load, async () => ++attempts >= 2, (callback) => scheduled.push(callback));
      await Bun.sleep(0);
      expect(attempts).toBe(1);
      scheduled.shift()?.();
      await Bun.sleep(0);
      expect(attempts).toBe(2);
    } finally {
      globalThis.fetch = original;
    }
  });
});

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

describe("direct-provider exchanges", () => {
  const assistant = {
    id: "msg-assistant", sessionID: "ses-direct", parentID: "msg-user", role: "assistant",
    modelID: "claude-sonnet-4-6", providerID: "anthropic",
    time: { created: 2_000, completed: 3_250 },
    tokens: { input: 10, output: 7, reasoning: 2, cache: { read: 4, write: 3 } },
  };
  const messages = { data: [
    {
      info: { id: "msg-user", sessionID: "ses-direct", role: "user", time: { created: 1_500 } },
      parts: [
        { type: "text", text: "fix the bug", metadata: { ignored: true } },
        { type: "file", mime: "text/plain", filename: "bug.txt", url: "file:///bug.txt", source: { type: "file", path: "bug.txt", text: { value: "bug", start: 0, end: 3 } } },
      ],
    },
    {
      info: assistant,
      parts: [
        { type: "reasoning", text: "trace it" },
        { type: "text", text: "fixed" },
        { type: "tool", callID: "call-1", tool: "bash", state: { status: "completed", input: { command: "bun test", invalid: 1n }, output: "pass", title: "Test", metadata: { ignored: true } } },
        { type: "tool", callID: "call-2", tool: "read", state: { status: "error", input: { path: "missing" }, error: "not found" } },
      ],
    },
  ] };

  it("fetches and saves a normalized authenticated direct-provider payload", async () => {
    const env = { MIMIR_URL: process.env.MIMIR_URL, MIMIR_TOKEN: process.env.MIMIR_TOKEN };
    const originalFetch = globalThis.fetch;
    process.env.MIMIR_URL = "https://mimir.example";
    process.env.MIMIR_TOKEN = "tok-secret";
    const calls: Array<{ url: string; authorization: string | null; harness: string | null; repo: string | null; body: unknown }> = [];
    const messageCalls: unknown[] = [];
    try {
      globalThis.fetch = async (input, init) => {
        const request = new Request(input, init);
        if (request.url.endsWith("/exchanges")) calls.push({ url: request.url, authorization: request.headers.get("authorization"), harness: request.headers.get("x-mimir-harness"), repo: request.headers.get("x-mimir-repo"), body: await request.json() });
        return new Response("{}", { status: 200 });
      };
      const client = { session: { messages: async (input: unknown) => { messageCalls.push(input); return messages; } } };
      const hooks = await plugin.server({ client, directory: "/repo/mimir" } as never);
      await hooks.event!({ event: { type: "message.updated", properties: { info: assistant } } } as never);
      await Bun.sleep(10);
    } finally {
      globalThis.fetch = originalFetch;
      process.env.MIMIR_URL = env.MIMIR_URL;
      process.env.MIMIR_TOKEN = env.MIMIR_TOKEN;
    }
    expect(messageCalls).toEqual([{ path: { id: "ses-direct" } }]);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toBe("https://mimir.example/sessions/ses-direct/exchanges");
    expect(calls[0]?.authorization).toBe("Bearer tok-secret");
    expect(calls[0]?.harness).toBe("opencode");
    expect(calls[0]?.repo).toBe("mimir");
    expect(calls[0]?.body).toEqual(buildDirectExchange(assistant, messages));
    expect(calls[0]?.body).toMatchObject({
      exchange_id: "msg-assistant", ts: new Date(3_250).toISOString(), provider: "anthropic", model: "claude-sonnet-4-6",
      request: { message_id: "msg-user", messages: [{ role: "user", content: [{ type: "text", text: "fix the bug" }, { type: "file", mime: "text/plain", filename: "bug.txt" }] }] },
      response: { message_id: "msg-assistant", parent_message_id: "msg-user", parts: [{ type: "reasoning", text: "trace it" }, { type: "text", text: "fixed" }, { type: "tool", input: { command: "bun test", invalid: "1" }, output: "pass" }, { type: "tool", error: "not found" }] },
      usage: { input_tokens: 14, output_tokens: 7 }, latency_ms: 1_250,
    });
  });

  it("does not fetch or post a full exchange for OpenRouter", async () => {
    let fetched = 0;
    let posted = 0;
    const reporter = createDirectExchangeReporter(async () => { fetched += 1; return messages; }, async () => { posted += 1; return true; });
    reporter.deliver({ ...assistant, providerID: "openrouter" });
    await Bun.sleep(0);
    expect(fetched).toBe(0);
    expect(posted).toBe(0);
    expect(reporter.pending()).toBe(0);
  });

  it("contains fetch and post failures and retries without duplicate in-flight delivery", async () => {
    let fetches = 0;
    let posts = 0;
    const scheduled: Array<() => void> = [];
    const reporter = createDirectExchangeReporter(
      async () => { if (++fetches === 1) throw new Error("session unavailable"); return messages; },
      async () => ++posts >= 2,
      (callback) => scheduled.push(callback),
    );
    expect(() => reporter.deliver(assistant)).not.toThrow();
    reporter.deliver(assistant);
    await Bun.sleep(0);
    expect(fetches).toBe(1);
    expect(posts).toBe(0);
    scheduled.shift()?.();
    await Bun.sleep(0);
    expect(fetches).toBe(2);
    expect(posts).toBe(1);
    scheduled.shift()?.();
    await Bun.sleep(0);
    expect(fetches).toBe(2);
    expect(posts).toBe(2);
    expect(reporter.pending()).toBe(0);
  });

  it("bounds strings, part counts, and cyclic tool values", () => {
    const cyclic: Record<string, unknown> = { keep: true };
    cyclic.self = cyclic;
    const huge = "x".repeat(100_000);
    const bounded = buildDirectExchange(assistant, { data: [
      { info: messages.data[0]!.info, parts: Array.from({ length: 300 }, () => ({ type: "text", text: huge })) },
      { info: assistant, parts: [{ type: "tool", tool: "test", state: { status: "completed", input: cyclic, output: "ok" } }] },
    ] });
    expect(bounded).not.toBeNull();
    expect(bounded!.request.messages[0].content.length).toBeLessThanOrEqual(256);
    expect(JSON.stringify(bounded)).not.toContain('"self"');
    expect(new TextEncoder().encode(JSON.stringify(bounded)).byteLength).toBeLessThanOrEqual(512 * 1024);
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

describe("postDirectExchange", () => {
  it("contains non-2xx and transport failures", async () => {
    const exchange = buildDirectExchange({
      id: "a", sessionID: "s", parentID: "u", role: "assistant", providerID: "anthropic", modelID: "m",
      time: { created: 1, completed: 2 }, tokens: {},
    }, { data: [
      { info: { id: "u", role: "user", time: { created: 1 } }, parts: [] },
      { info: { id: "a", role: "assistant" }, parts: [] },
    ] })!;
    const original = globalThis.fetch;
    try {
      globalThis.fetch = async () => new Response("nope", { status: 503 });
      expect(await postDirectExchange({ url: "https://mimir.example", token: "tok" }, "s", exchange, null)).toBe(false);
      globalThis.fetch = async () => { throw new Error("offline"); };
      expect(await postDirectExchange({ url: "https://mimir.example", token: "tok" }, "s", exchange, null)).toBe(false);
    } finally {
      globalThis.fetch = original;
    }
  });
});
