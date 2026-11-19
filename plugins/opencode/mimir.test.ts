import { afterEach, describe, expect, it, mock } from "bun:test";
import * as childProcess from "node:child_process";
import { EventEmitter } from "node:events";

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
// The plugin must load after spawn is mocked, so a static import cannot be used.
const { default: plugin, MimirPlugin, __testing } = await import("./mimir");

const { parseMimirConfig, resolveConnection, mimirExecutable, commitConfirmations, mimirToolSpecs, buildTurnEvent, buildDirectExchange, repoName, createActivityTracker, createDeliveryQueue, createDirectExchangeReporter, postEvent, postDirectExchange, buildHarnessLoad, loadHarnessLoad, postHarnessLoad, reportHarnessLoad } = __testing;

const originalEnv = { MIMIR_URL: process.env.MIMIR_URL, MIMIR_TOKEN: process.env.MIMIR_TOKEN, MIMIR_BIN: process.env.MIMIR_BIN };
const originalFetch = globalThis.fetch;

afterEach(() => {
  spawned.length = 0;
  globalThis.fetch = originalFetch;
  for (const [key, value] of Object.entries(originalEnv)) {
    if (value === undefined) delete process.env[key]; else process.env[key] = value;
  }
});

async function startPlugin(respond: (request: Request) => Response | Promise<Response> = () => new Response("{}")) {
  process.env.MIMIR_URL = "https://mimir.example";
  process.env.MIMIR_TOKEN = "tok";
  process.env.MIMIR_BIN = "/opt/mimir/bin/mimir";
  globalThis.fetch = (async (input: string | URL | Request, init?: RequestInit) => respond(new Request(input, init))) as typeof fetch;
  return plugin.server({ directory: "/work/mimir/worker", worktree: "/work/mimir" } as never);
}

const toolContext = { sessionID: "ses_1", abort: new AbortController().signal, metadata() {} } as never;

describe("commit capture through mimir _hook git", () => {
  it("resolves the Mimir binary from MIMIR_BIN, then config executable, then PATH", () => {
    const files: Record<string, string> = { "/home/u/.mimir/config": 'url = "https://mimir.example"\nexecutable=/usr/local/bin/mimir\n' };
    const read = (path: string) => files[path.replace(/\\/g, "/")] ?? null;
    expect(mimirExecutable({ MIMIR_BIN: " /opt/mimir " }, read, "/home/u")).toBe("/opt/mimir");
    expect(mimirExecutable({}, read, "/home/u")).toBe("/usr/local/bin/mimir");
    files["/custom/config"] = 'executable = "C:\\Tools\\mimir.exe"\r\n';
    expect(mimirExecutable({ MIMIR_HOME: "/custom" }, read, "/home/u")).toBe("C:\\Tools\\mimir.exe");
    expect(mimirExecutable({}, () => null, "/home/u")).toBe("mimir");
  });

  it("extracts only commit confirmation lines", () => {
    expect(commitConfirmations("lint ok\n[main 1a2b3c4] fix\n 1 file changed")).toBe("[main 1a2b3c4] fix");
    expect(commitConfirmations("[main (root-commit) 1a2b3c4] first")).toBe("[main (root-commit) 1a2b3c4] first");
    expect(commitConfirmations("nothing to commit, working tree clean")).toBeNull();
    expect(commitConfirmations(undefined)).toBeNull();
  });

  it("hands shell commit output to the exact session and refreshes on idle and deletion", async () => {
    const hooks = await startPlugin();
    const after = hooks["tool.execute.after"]!;
    await after({ tool: "bash", sessionID: "ses_1", callID: "c1", args: {} }, { title: "", output: "[main 1a2b3c4] fix\n 1 file changed", metadata: {} });
    await after({ tool: "read", sessionID: "ses_1", callID: "c2", args: {} }, { title: "", output: "[main 1a2b3c4] fix", metadata: {} });
    await after({ tool: "bash", sessionID: "", callID: "c3", args: {} }, { title: "", output: "[main 1a2b3c4] fix", metadata: {} });
    await after({ tool: "bash", sessionID: "ses_1", callID: "c4", args: {} }, { title: "", output: "nothing to commit", metadata: {} });
    expect(spawned).toEqual([{
      command: "/opt/mimir/bin/mimir",
      args: ["_hook", "git"],
      input: { session_id: "ses_1", cwd: "/work/mimir/worker", harness: "opencode", repo: "mimir", tool_output: "[main 1a2b3c4] fix" },
    }]);
    await hooks.event!({ event: { type: "session.idle", properties: { sessionID: "ses_1" } } } as never);
    await hooks.event!({ event: { type: "session.deleted", properties: { info: { id: "ses_2" } } } } as never);
    expect(spawned.slice(1).map((call) => call.input)).toEqual([
      { session_id: "ses_1", cwd: "/work/mimir/worker", harness: "opencode", repo: "mimir", refresh: true },
      { session_id: "ses_2", cwd: "/work/mimir/worker", harness: "opencode", repo: "mimir", refresh: true },
    ]);
  });

  it("a failed Mimir binary never interrupts the tool", async () => {
    const hooks = await startPlugin();
    process.env.MIMIR_BIN = "/missing/mimir";
    await hooks["tool.execute.after"]!({ tool: "bash", sessionID: "ses_1", callID: "c1", args: {} }, { title: "", output: "[main 1a2b3c4] fix", metadata: {} });
    expect(spawned.map((call) => call.command)).toEqual(["/missing/mimir"]);
  });
});

describe("Mimir MCP tools", () => {
  it("registers exactly the forwarding query tools", async () => {
    const hooks = await startPlugin();
    expect(Object.keys(hooks.tool!).sort()).toEqual([
      "mimir_commit_patch", "mimir_commits", "mimir_day", "mimir_devices", "mimir_search",
      "mimir_session", "mimir_sessions", "mimir_stats", "mimir_turns",
    ]);
  });

  it("declares the MCP argument contract", () => {
    const shape = (args: Record<string, { isOptional(): boolean }>) => Object.fromEntries(Object.entries(args).map(([key, schema]) => [key, schema.isOptional() ? "optional" : "required"]));
    expect(shape(mimirToolSpecs.search.args)).toEqual({ query: "required", repo: "optional", from: "optional", to: "optional", limit: "optional" });
    expect(Object.keys(mimirToolSpecs.sessions.args)).toEqual(["q", "repo", "harness", "device", "state", "git_status", "from", "to", "limit", "cursor"]);
    expect(shape(mimirToolSpecs.session.args)).toEqual({ id: "required" });
    expect(shape(mimirToolSpecs.turns.args)).toEqual({ id: "required", cursor: "optional", limit: "optional", order: "optional" });
    expect(shape(mimirToolSpecs.stats.args)).toEqual({ group_by: "required", from: "optional", to: "optional", tz: "optional" });
    expect(shape(mimirToolSpecs.day.args)).toEqual({ date: "optional", tz: "optional" });
    expect(mimirToolSpecs.devices.args).toEqual({});
    expect(Object.keys(mimirToolSpecs.commits.args)).toEqual(["repo", "ref", "q", "session", "from", "to", "limit", "cursor"]);
    expect(shape(mimirToolSpecs.commit_patch.args)).toEqual({ session_id: "required", sha: "required", max_bytes: "optional" });
  });

  it("forwards tools/call with the machine token and returns the result JSON text", async () => {
    const requests: Request[] = [];
    const result = { sessions: [], next_cursor: null };
    const hooks = await startPlugin((request) => {
      if (request.url.endsWith("/mcp")) requests.push(request);
      return Response.json({ jsonrpc: "2.0", id: 1, result: { content: [{ type: "text", text: JSON.stringify(result) }], structuredContent: result } });
    });
    expect(await hooks.tool!.mimir_sessions.execute({ repo: "mimir", limit: 5 }, toolContext)).toBe(JSON.stringify(result));
    expect(requests).toHaveLength(1);
    expect(requests[0]!.url).toBe("https://mimir.example/mcp");
    expect(requests[0]!.method).toBe("POST");
    expect(requests[0]!.headers.get("authorization")).toBe("Bearer tok");
    expect(await requests[0]!.json()).toEqual({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "sessions", arguments: { repo: "mimir", limit: 5 } } });
  });

  it("surfaces tool errors, JSON-RPC errors and HTTP failures", async () => {
    let reply = () => Response.json({ jsonrpc: "2.0", id: 1, result: { content: [{ type: "text", text: "query is required" }], isError: true } });
    const hooks = await startPlugin(() => reply());
    await expect(hooks.tool!.mimir_search.execute({ query: "x" }, toolContext)).rejects.toThrow("Mimir search failed: query is required");
    reply = () => Response.json({ jsonrpc: "2.0", id: 1, error: { code: -32601, message: "Method not found" } });
    await expect(hooks.tool!.mimir_devices.execute({}, toolContext)).rejects.toThrow("Mimir devices failed: Method not found");
    reply = () => new Response("unauthorized", { status: 401 });
    await expect(hooks.tool!.mimir_stats.execute({ group_by: "repo" }, toolContext)).rejects.toThrow("Mimir stats failed (401): unauthorized");
  });
});

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
      const hooks = await plugin.server({ directory: "C:\\repo\\mimir\\worker", worktree: "C:\\repo\\mimir" } as never);
      const headers = { headers: {} as Record<string, string> };
      await hooks["chat.headers"]!({ sessionID: "ses_test", model: { providerID: "openrouter" } } as never, headers);
      expect(headers.headers).toEqual({ "x-mimir-session": "ses_test", "x-mimir-harness": "opencode", "x-mimir-repo": "mimir" });
      const other = { headers: {} as Record<string, string> };
      await hooks["chat.headers"]!({ sessionID: "ses_test", model: { providerID: "anthropic" } } as never, other);
      expect(other.headers).toEqual({});
      await hooks["chat.headers"]!({ sessionID: "", model: { providerID: "openrouter" } } as never, other);
      expect(other.headers).toEqual({});
      const outside = await plugin.server({ directory: "C:\\Users\\johns", worktree: "/" } as never);
      const unattributed = { headers: {} as Record<string, string> };
      await outside["chat.headers"]!({ sessionID: "ses_home", model: { providerID: "openrouter" } } as never, unattributed);
      expect(unattributed.headers).toEqual({ "x-mimir-session": "ses_home", "x-mimir-harness": "opencode" });
    } finally {
      globalThis.fetch = fetch;
      process.env.MIMIR_URL = original.MIMIR_URL;
      process.env.MIMIR_TOKEN = original.MIMIR_TOKEN;
    }
  });
});

describe("session hierarchy", () => {
  it("reports OpenCode parent sessions on lifecycle events", async () => {
    const original = { MIMIR_URL: process.env.MIMIR_URL, MIMIR_TOKEN: process.env.MIMIR_TOKEN };
    process.env.MIMIR_URL = "https://mimir.example";
    process.env.MIMIR_TOKEN = "tok";
    const originalFetch = globalThis.fetch;
    let body: Record<string, unknown> | null = null;
    globalThis.fetch = async (_input, init) => {
      body = JSON.parse(String(init?.body));
      return new Response("{}", { status: 200 });
    };
    try {
      const hooks = await plugin.server({ directory: "/repo/mimir" } as never);
      await hooks.event!({ event: { type: "session.created", properties: { info: { id: "child", parentID: "root", title: "Fix session titles" } } } } as never);
      await Bun.sleep(10);
      expect(body).toMatchObject({ kind: "heartbeat", session_id: "child", parent_session_id: "root", title: "Fix session titles" });
    } finally {
      globalThis.fetch = originalFetch;
      if (original.MIMIR_URL === undefined) delete process.env.MIMIR_URL; else process.env.MIMIR_URL = original.MIMIR_URL;
      if (original.MIMIR_TOKEN === undefined) delete process.env.MIMIR_TOKEN; else process.env.MIMIR_TOKEN = original.MIMIR_TOKEN;
    }
  });
});

describe("startup build identity", () => {
  it("posts only source identity and safe manifest provenance to the authenticated integration path", async () => {
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

  it("reports source identity when the managed-file manifest is missing", () => {
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
  it("extracts the url and executable", () => {
    expect(parseMimirConfig('url = "https://mimir.example.workers.dev"\n')).toEqual({ url: "https://mimir.example.workers.dev" });
    expect(parseMimirConfig("url = https://mimir.example/\r\nexecutable=/usr/local/bin/mimir\r\n")).toEqual({ url: "https://mimir.example", executable: "/usr/local/bin/mimir" });
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
      turn: { exchange_id: "msg-1", model: "openai/gpt-5", provider: "openrouter", request_kind: "primary", usage: { input_tokens: 10, output_tokens: 4, cache_read_tokens: 5, cache_write_tokens: 0 }, latency_ms: 1_500 },
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
        { type: "tool", callID: "call-1", tool: "read", state: { status: "completed", input: { path: "src/auth.ts", invalid: 1n }, output: "loaded", title: "Read", metadata: { ignored: true } } },
        { type: "tool", callID: "call-2", tool: "edit", state: { status: "error", input: { path: "src/auth.ts" }, error: "write failed" } },
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
      const hooks = await plugin.server({ client, directory: "/repo/mimir/src", worktree: "/repo/mimir" } as never);
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
      response: { message_id: "msg-assistant", parent_message_id: "msg-user", parts: [{ type: "reasoning", text: "trace it" }, { type: "text", text: "fixed" }, { type: "tool", input: { path: "src/auth.ts", invalid: "1" }, output: "loaded" }, { type: "tool", input: { path: "src/auth.ts" }, error: "write failed" }] },
      tool_activity: [
        { name: "read", input: { path: "src/auth.ts", invalid: "1" }, status: "succeeded", output: "loaded" },
        { name: "edit", input: { path: "src/auth.ts" }, status: "failed", output: "write failed" },
      ],
      usage: { input_tokens: 10, output_tokens: 7, cache_read_tokens: 4, cache_write_tokens: 3 }, latency_ms: 1_250,
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
  it("names the git worktree and nothing outside git", () => {
    expect(repoName("/home/u/projects/mimir")).toBe("mimir");
    expect(repoName("C:\\Users\\u\\projects\\mimir\\")).toBe("mimir");
    expect(repoName("/")).toBeNull();
    expect(repoName("")).toBeNull();
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
