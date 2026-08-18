import { describe, expect, it } from "bun:test";
import plugin, { MimirPlugin, __testing } from "./mimir";

const { parseMimirConfig, resolveConnection, buildTurnEvent, buildDirectExchange, repoName, createActivityTracker, createDeliveryQueue, createDirectExchangeReporter, postEvent, postDirectExchange, formatSessionReceipt, buildHarnessLoad, loadHarnessLoad, postHarnessLoad, reportHarnessLoad, gitEvidence, workspaceGitEvidence, outcomeGitEvidence, landedGitEvidenceError, noteCommitRef, mergeOutcomeEvidence, normalizeRemoteUrl, redactEvidenceText, boundedBytes } = __testing;

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

describe("session outcome tools", () => {
  it("uses the exact OpenCode session and returns authoritative status", async () => {
    const original = { MIMIR_URL: process.env.MIMIR_URL, MIMIR_TOKEN: process.env.MIMIR_TOKEN };
    process.env.MIMIR_URL = "https://mimir.example";
    process.env.MIMIR_TOKEN = "tok";
    const originalFetch = globalThis.fetch;
    const requests: Array<{ url: string; method: string; body?: string }> = [];
    globalThis.fetch = async (input, init) => {
      const url = String(input);
      requests.push({ url, method: init?.method ?? "GET", body: typeof init?.body === "string" ? init.body : undefined });
      if (url.endsWith("/status")) return Response.json({ session_id: "child/session", outcome: "landed", capture: { status: "saved", saved_exchanges: 3 }, dashboard_url: "https://mimir.example/dashboard/sessions/child" });
      return Response.json({ ok: true });
    };
    try {
      const hooks = await plugin.server({ directory: "/repo/mimir" } as never);
      let title = "";
      const output = await hooks.tool!.mimir_session_outcome.execute({ outcome: "landed", reason: "tests passed", evidence: "commit abc123" }, { sessionID: "child/session", metadata(input: { title?: string }) { title = input.title ?? ""; } } as never);
      expect(output).toContain("◆ Mimir  Saved · 3 exchanges · LANDED");
      expect(output).toContain("View session: https://mimir.example/dashboard/sessions/child");
      expect(title).toBe("Mimir receipt");
      expect(requests).toContainEqual(expect.objectContaining({ url: "https://mimir.example/sessions/child%2Fsession/outcome", method: "POST" }));
      expect(requests).toContainEqual(expect.objectContaining({ url: "https://mimir.example/sessions/child%2Fsession/status", method: "GET" }));
    } finally {
      globalThis.fetch = originalFetch;
      if (original.MIMIR_URL === undefined) delete process.env.MIMIR_URL; else process.env.MIMIR_URL = original.MIMIR_URL;
      if (original.MIMIR_TOKEN === undefined) delete process.env.MIMIR_TOKEN; else process.env.MIMIR_TOKEN = original.MIMIR_TOKEN;
    }
  });

  it("does not overwrite the outcome when explicit Git evidence cannot be resolved", async () => {
    const original = { MIMIR_URL: process.env.MIMIR_URL, MIMIR_TOKEN: process.env.MIMIR_TOKEN };
    process.env.MIMIR_URL = "https://mimir.example";
    process.env.MIMIR_TOKEN = "tok";
    const originalFetch = globalThis.fetch;
    const requests: string[] = [];
    globalThis.fetch = async (input) => {
      requests.push(String(input));
      return Response.json({ ok: true });
    };
    try {
      const hooks = await plugin.server({ directory: "/repo/does-not-exist" } as never);
      const output = await hooks.tool!.mimir_session_outcome.execute(
        { outcome: "landed", reason: "tests passed", commit: "a329f20" },
        { sessionID: "child/session", metadata() {} } as never,
      );
      expect(output).toContain("requested Git commit could not be resolved; outcome was not recorded");
      expect(requests.some((url) => url.endsWith("/sessions/child%2Fsession/outcome"))).toBe(false);
    } finally {
      globalThis.fetch = originalFetch;
      if (original.MIMIR_URL === undefined) delete process.env.MIMIR_URL; else process.env.MIMIR_URL = original.MIMIR_URL;
      if (original.MIMIR_TOKEN === undefined) delete process.env.MIMIR_TOKEN; else process.env.MIMIR_TOKEN = original.MIMIR_TOKEN;
    }
  });
});

describe("session receipt formatting", () => {
  it("preserves authoritative capture states", () => {
    expect(formatSessionReceipt(JSON.stringify({ capture: { status: "pending", pending_exchanges: 2 }, outcome: "unresolved" }))).toBe("◆ Mimir  Saving… · 2 pending · UNRESOLVED");
    expect(formatSessionReceipt(JSON.stringify({ capture: { status: "partial", saved_exchanges: 12, failed_exchanges: 2 } }))).toBe("◆ Mimir  Partial · 12 saved · 2 failed");
    expect(formatSessionReceipt(JSON.stringify({ capture: { status: "pending", saved_exchanges: 12, failed_exchanges: 2, pending_exchanges: 1 } }))).toBe("◆ Mimir  Partial · 12 saved · 2 failed · 1 pending");
    expect(formatSessionReceipt(JSON.stringify({ capture: { status: "failed", failed_exchanges: 1 } }))).toBe("◆ Mimir  Couldn’t save this session · 1 failed");
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
      response: { message_id: "msg-assistant", parent_message_id: "msg-user", parts: [{ type: "reasoning", text: "trace it" }, { type: "text", text: "fixed" }, { type: "tool", input: { path: "src/auth.ts", invalid: "1" }, output: "loaded" }, { type: "tool", input: { path: "src/auth.ts" }, error: "write failed" }] },
      tool_activity: [
        { name: "read", input: { path: "src/auth.ts", invalid: "1" }, status: "succeeded", output: "loaded" },
        { name: "edit", input: { path: "src/auth.ts" }, status: "failed", output: "write failed" },
      ],
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

describe("gitEvidence", () => {
  const head = "a".repeat(40);
  const base = "b".repeat(40);

  function runner(outputs: Record<string, string | null>) {
    const calls: string[][] = [];
    return {
      calls,
      run: (_command: string, args: string[]) => {
        calls.push(args);
        const key = args.join(" ");
        const stdout = outputs[key];
        return { status: stdout == null ? 1 : 0, stdout: stdout ?? "" };
      },
    };
  }

  it("captures commit, base, and a redacted bounded patch", () => {
    const { run } = runner({
      "rev-parse --verify HEAD^{commit}": `${head}\n`,
      [`rev-parse --verify ${head}~1^{commit}`]: `${base}\n`,
      [`show --format= --patch --unified=3 ${head}`]: "diff --git a/a.ts b/a.ts\n+const token = \"sk_live_1234567890abcdef\";\n",
    });
    const evidence = gitEvidence("/repo", run)!;
    expect(evidence.commit).toBe(head);
    expect(evidence.base_commit).toBe(base);
    expect(evidence.provenance).toBe("opencode-plugin");
    expect(evidence.patch).toContain("diff --git");
    expect(evidence.patch).not.toContain("sk_live_1234567890abcdef");
    expect(evidence.patch).toContain("[REDACTED]");
  });

  it("omits base and patch when git has no parent or fails", () => {
    const { run } = runner({ "rev-parse --verify HEAD^{commit}": head });
    const evidence = gitEvidence("/repo", run)!;
    expect(evidence).toEqual({ commit: head, provenance: "opencode-plugin" });
  });

  it("returns null outside a git work tree", () => {
    const { run } = runner({ "rev-parse --verify HEAD^{commit}": null });
    expect(gitEvidence("/repo", run)).toBeNull();
    expect(gitEvidence(undefined, run)).toBeNull();
  });

  it("captures the origin remote and branch needed to link a commit", () => {
    const { run } = runner({
      "rev-parse --verify HEAD^{commit}": head,
      "remote get-url origin": "git@github.com:owner/repo.git\n",
      "rev-parse --abbrev-ref HEAD": "master\n",
    });
    const evidence = gitEvidence("/repo", run)!;
    expect(evidence.repository_url).toBe("https://github.com/owner/repo");
    expect(evidence.ref).toBe("master");
  });

  it("omits a detached HEAD ref and an unusable remote", () => {
    const { run } = runner({
      "rev-parse --verify HEAD^{commit}": head,
      "remote get-url origin": "/srv/git/repo.git",
      "rev-parse --abbrev-ref HEAD": "HEAD",
    });
    expect(gitEvidence("/repo", run)!).toEqual({ commit: head, provenance: "opencode-plugin" });
  });

  it("caps oversized patches", () => {
    const huge = `+${"x".repeat(64 * 1024)}\n`;
    const { run } = runner({ "rev-parse --verify HEAD^{commit}": head, [`show --format= --patch --unified=3 ${head}`]: huge });
    const patch = gitEvidence("/repo", run)!.patch!;
    expect(new TextEncoder().encode(patch).byteLength).toBeLessThanOrEqual(20 * 1024);
  });

  it("resolves an explicit short SHA and captures that commit", () => {
    const short = "a329f20";
    const resolved = "a329f20" + "c".repeat(33);
    const { calls, run } = runner({
      [`rev-parse --verify ${short}^{commit}`]: resolved,
      [`show --format= --patch --unified=3 ${resolved}`]: "explicit patch",
    });
    expect(gitEvidence("/repo", run, short)).toMatchObject({ commit: resolved, patch: "explicit patch" });
    expect(calls).toContainEqual(["rev-parse", "--verify", `${short}^{commit}`]);
  });
});

describe("workspaceGitEvidence", () => {
  it("falls back to the OpenCode directory when its worktree is unusable", () => {
    const head = "a".repeat(40);
    const calls: string[] = [];
    const run = (_command: string, args: string[], cwd: string) => {
      calls.push(`${cwd}:${args.join(" ")}`);
      return cwd === "/repo" && args.join(" ") === "rev-parse --verify HEAD^{commit}"
        ? { status: 0, stdout: head }
        : { status: 1, stdout: "" };
    };
    expect(workspaceGitEvidence("/stale-worktree", "/repo", run)?.commit).toBe(head);
    expect(calls).toContain("/stale-worktree:rev-parse --verify HEAD^{commit}");
    expect(calls).toContain("/repo:rev-parse --verify HEAD^{commit}");
  });
});

describe("outcomeGitEvidence", () => {
  const commit = "a329f20" + "c".repeat(33);

  it("uses one commit token from the note when HEAD is unavailable", () => {
    const { run } = (() => {
      const calls: string[] = [];
      return {
        calls,
        run: (_command: string, args: string[]) => {
          const key = args.join(" ");
          calls.push(key);
          return key === "rev-parse --verify a329f20^{commit}" ? { status: 0, stdout: commit } : { status: 1, stdout: "" };
        },
      };
    })();
    expect(outcomeGitEvidence(undefined, "/repo", "Commit a329f20", undefined, run)?.commit).toBe(commit);
  });

  it("refuses an ambiguous note", () => {
    expect(noteCommitRef("Commits a329f20 and b418e31")).toBeNull();
    const calls: string[] = [];
    const run = (_command: string, args: string[]) => {
      calls.push(args.join(" "));
      return { status: 1, stdout: "" };
    };
    expect(outcomeGitEvidence(undefined, "/repo", "Commits a329f20 and b418e31", undefined, run)).toBeNull();
    expect(calls).toEqual(["rev-parse --verify HEAD^{commit}"]);
  });

  it("keeps HEAD as the default when note evidence also names a commit", () => {
    const head = "d".repeat(40);
    const calls: string[] = [];
    const run = (_command: string, args: string[]) => {
      const key = args.join(" ");
      calls.push(key);
      return key === "rev-parse --verify HEAD^{commit}" ? { status: 0, stdout: head } : { status: 1, stdout: "" };
    };
    expect(outcomeGitEvidence(undefined, "/repo", "Commit a329f20", undefined, run)?.commit).toBe(head);
    expect(calls).not.toContain("rev-parse --verify a329f20^{commit}");
  });
});

describe("landedGitEvidenceError", () => {
  const commit = "a".repeat(40);

  it("leaves the prior outcome untouched when explicit Git inference fails", () => {
    expect(landedGitEvidenceError("landed", "a329f20", null)).toContain("outcome was not recorded");
  });

  it("requires a retrievable patch for an inferred landed commit", () => {
    expect(landedGitEvidenceError("landed", undefined, { commit, provenance: "opencode-plugin" })).toContain("no retrievable patch");
    expect(landedGitEvidenceError("landed", undefined, { commit, patch: "diff --git a/a b/a", provenance: "opencode-plugin" })).toBeNull();
  });

  it("does not impose Git evidence on non-Git outcomes", () => {
    expect(landedGitEvidenceError("landed", undefined, null)).toBeNull();
    expect(landedGitEvidenceError("discarded", "a329f20", null)).toBeNull();
  });
});

describe("normalizeRemoteUrl", () => {
  it("normalizes every common remote form to a credential-free https URL", () => {
    expect(normalizeRemoteUrl("git@github.com:owner/repo.git")).toBe("https://github.com/owner/repo");
    expect(normalizeRemoteUrl("ssh://git@gitlab.example.com:2222/group/sub/repo.git")).toBe("https://gitlab.example.com/group/sub/repo");
    expect(normalizeRemoteUrl("https://user:secret@github.com/owner/repo.git")).toBe("https://github.com/owner/repo");
    expect(normalizeRemoteUrl("http://git.example.com/owner/repo/")).toBe("https://git.example.com/owner/repo");
  });

  it("rejects values that cannot be browsed", () => {
    for (const value of ["", "   ", "/srv/git/repo.git", "C:\\repos\\repo", "git@localhost:repo.git", "https://github.com/"]) {
      expect(normalizeRemoteUrl(value)).toBeNull();
    }
    expect(normalizeRemoteUrl(null)).toBeNull();
  });
});

describe("mergeOutcomeEvidence", () => {
  const git = { commit: "a".repeat(40), provenance: "opencode-plugin" as const };

  it("keeps a bare agent string as-is without git data", () => {
    expect(mergeOutcomeEvidence("merged in PR 42", null)).toBe("merged in PR 42");
  });

  it("wraps agent text as a note when commit evidence exists", () => {
    expect(mergeOutcomeEvidence("merged in PR 42", git)).toEqual({ note: "merged in PR 42", ...git });
  });

  it("uses git evidence alone when the agent supplied nothing", () => {
    expect(mergeOutcomeEvidence(undefined, git)).toEqual(git);
    expect(mergeOutcomeEvidence("   ", git)).toEqual(git);
  });

  it("stays undefined with no evidence at all", () => {
    expect(mergeOutcomeEvidence(undefined, null)).toBeUndefined();
  });
});

describe("redactEvidenceText and boundedBytes", () => {
  it("redacts builtin credential shapes", () => {
    const text = redactEvidenceText("Bearer abc.def.ghi\napi_key: supersecretvalue\npassword=hunter2value");
    expect(text).toContain("Bearer [REDACTED]");
    expect(text).not.toContain("supersecretvalue");
    expect(text).not.toContain("hunter2value");
  });

  it("bounds by bytes without splitting multibyte characters", () => {
    const bounded = boundedBytes("é".repeat(100), 50);
    expect(new TextEncoder().encode(bounded).byteLength).toBeLessThanOrEqual(50);
  });
});
