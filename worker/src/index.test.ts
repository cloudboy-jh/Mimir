import { describe, expect, it } from "vitest";
import { buildUpstreamHeaders, deriveSessionFields, extractUsage, parseCapturedResponse, readBoundedText, redact, requestToken, shouldSave, validateConfigValues } from "./index";

describe("proxy boundaries", () => {
  it("accepts bearer and Anthropic authentication", () => {
    expect(requestToken(new Headers({ authorization: "Bearer machine" }))).toBe("machine");
    expect(requestToken(new Headers({ "x-api-key": "machine" }))).toBe("machine");
  });

  it("replaces auth and strips private metadata upstream", () => {
    const headers = buildUpstreamHeaders(new Headers({ authorization: "Bearer machine", "x-api-key": "machine", "x-mimir-session": "session", "x-mimir-repo": "repo" }), "openrouter");
    expect(headers.get("authorization")).toBe("Bearer openrouter");
    expect(headers.get("x-api-key")).toBeNull();
    expect(headers.get("x-mimir-session")).toBeNull();
    expect(headers.get("x-mimir-repo")).toBeNull();
  });

  it("applies repository and model exclusions", () => {
    const config = { enabled: true, excludeRepos: ["private-*"] , excludeModels: ["*/free"], gapMinutes: 15 };
    expect(shouldSave(config, "private-app", "anthropic/claude")).toBe(false);
    expect(shouldSave(config, "public-app", "model/free")).toBe(false);
    expect(shouldSave(config, "public-app", "anthropic/claude")).toBe(true);
  });
});

describe("config", () => {
  it("rejects unknown and malformed values", () => {
    expect(validateConfigValues({ unknown: true })).toContain("unknown config key");
    expect(validateConfigValues({ "session.gap_minutes": 0 })).toContain("1 to 1440");
    expect(validateConfigValues({ "save.enabled": "yes" })).toContain("boolean");
    expect(validateConfigValues({ "save.enabled": true, "save.exclude_repos": ["private-*"] })).toBe("");
  });
});

describe("capture", () => {
  it("reassembles OpenAI SSE text and usage", () => {
    const response = parseCapturedResponse('data: {"choices":[{"delta":{"content":"hello "}}]}\n\ndata: {"choices":[{"delta":{"content":"world"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}\n\ndata: [DONE]\n', "text/event-stream") as Record<string, unknown>;
    expect(response.content).toBe("hello world");
    expect(extractUsage(response)).toEqual({ prompt_tokens: 3, completion_tokens: 2 });
  });

  it("reassembles Anthropic SSE text and usage", () => {
    const response = parseCapturedResponse('event: message_start\ndata: {"message":{"usage":{"input_tokens":4}}}\n\nevent: content_block_delta\ndata: {"delta":{"text":"hi"}}\n\nevent: message_delta\ndata: {"usage":{"output_tokens":1}}\n', "text/event-stream") as Record<string, unknown>;
    expect(response.content).toBe("hi");
    expect(extractUsage(response)).toEqual({ prompt_tokens: 4, completion_tokens: 1 });
  });

  it("redacts builtin and configured patterns", () => {
    expect(redact({ token: "secret-value", value: "customer-123" }, ["customer-[0-9]+"])).toEqual({ token: "[REDACTED]", value: "[REDACTED]" });
  });

  it("rejects streams above the capture limit", async () => {
    const stream = new Blob(["too large"]).stream();
    await expect(readBoundedText(stream, 3)).rejects.toThrow("capture limit exceeded");
  });

  it("does not treat dotted protocol names as files", () => {
    expect(deriveSessionFields({ object: "chat.completion.chunk", file: "src/auth.ts" })).toEqual({ files: ["src/auth.ts"], errors: [] });
  });
});
