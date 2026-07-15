import { describe, expect, it } from "vitest";
import { deriveSessionFields, extractUsage, parseCapturedResponse, readBoundedText, redact } from "./capture";

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
    expect(redact({ token: "secret-value", value: "customer-123" }, ["customer-[0-9]+"]))
      .toEqual({ token: "[REDACTED]", value: "[REDACTED]" });
  });

  it("rejects streams above the capture limit", async () => {
    const stream = new Blob(["too large"]).stream();
    await expect(readBoundedText(stream, 3)).rejects.toThrow("capture limit exceeded");
  });

  it("does not treat dotted protocol names as files", () => {
    expect(deriveSessionFields({ object: "chat.completion.chunk", file: "src/auth.ts" }))
      .toEqual({ files: ["src/auth.ts"], errors: [] });
  });
});
