import { describe, expect, test } from "bun:test";
import { join } from "node:path";
import { __testing } from "./mimir";

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
      content: [{ type: "text", text: "Done" }],
      usage: { input: 10, cacheRead: 4, output: 3 },
      stopReason: "stop",
    }, [{ role: "toolResult", toolName: "edit", content: [{ type: "text", text: "updated" }] }], "Auth fix");
    expect(direct).not.toBeNull();
    expect(direct?.usage).toEqual({ input_tokens: 14, output_tokens: 3 });
    expect(direct?.request_kind).toBe("primary");
    expect(direct?.title).toBe("Auth fix");
    expect((direct?.response as { tool_results: unknown[] }).tool_results).toHaveLength(1);

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
});
