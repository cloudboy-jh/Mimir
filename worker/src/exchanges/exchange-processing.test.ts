import { describe, expect, it } from "vitest";
import {
  classifyRequestKind,
  deriveIntent,
  deriveSessionFields,
} from "./evidence";
import {
  extractUsage,
  parseCapturedResponse,
  readBoundedText,
} from "./response-codec";
import { redact } from "./redaction";

describe("capture", () => {
  it("reassembles OpenAI SSE text and usage", () => {
    const response = parseCapturedResponse(
      'data: {"choices":[{"delta":{"content":"hello "}}]}\n\ndata: {"choices":[{"delta":{"content":"world"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}\n\ndata: [DONE]\n',
      "text/event-stream",
    ) as Record<string, unknown>;
    expect(response.content).toBe("hello world");
    expect(extractUsage(response)).toEqual({
      prompt_tokens: 3,
      completion_tokens: 2,
    });
  });

  it("reassembles Anthropic SSE text and usage", () => {
    const response = parseCapturedResponse(
      'event: message_start\ndata: {"message":{"usage":{"input_tokens":4}}}\n\nevent: content_block_delta\ndata: {"delta":{"text":"hi"}}\n\nevent: message_delta\ndata: {"usage":{"output_tokens":1}}\n',
      "text/event-stream",
    ) as Record<string, unknown>;
    expect(response.content).toBe("hi");
    expect(extractUsage(response)).toEqual({
      prompt_tokens: 4,
      completion_tokens: 1,
    });
  });

  it("redacts builtin and configured patterns", () => {
    expect(
      redact({ token: "secret-value", value: "customer-123" }, [
        "customer-[0-9]+",
      ]),
    ).toEqual({ token: "[REDACTED]", value: "[REDACTED]" });
    expect(redact("Bearer machine-secret", [])).toBe("Bearer [REDACTED]");
  });

  it("rejects streams above the capture limit", async () => {
    const stream = new Blob(["too large"]).stream();
    await expect(readBoundedText(stream, 3)).rejects.toThrow(
      "capture limit exceeded",
    );
  });

  it("derives files from provider and harness tool activity", () => {
    const request = {
      messages: [
        {
          role: "user",
          content: "look at worker/src/never-touched.ts and reference/craft.md",
        },
        {
          role: "assistant",
          content: [
            { type: "tool_use", input: { file_path: "worker/src/auth.ts" } },
          ],
        },
        {
          role: "assistant",
          tool_calls: [
            {
              function: {
                name: "edit",
                arguments: JSON.stringify({
                  path: "worker/web/src/lib/api.ts",
                  edits: [{ path: "docs/Spec.md" }],
                }),
              },
            },
          ],
        },
        {
          role: "assistant",
          content: [
            {
              type: "toolCall",
              name: "edit",
              arguments: { path: "plugins/oh-my-pi/mimir.ts" },
            },
          ],
        },
      ],
    };
    expect(deriveSessionFields(request, {}).files).toEqual([
      "worker/src/auth.ts",
      "worker/web/src/lib/api.ts",
      "docs/Spec.md",
      "plugins/oh-my-pi/mimir.ts",
    ]);
  });

  it("ignores prose paths, dependency directories, and non-path values", () => {
    const request = {
      object: "chat.completion.chunk",
      messages: [
        {
          role: "assistant",
          content: [
            {
              type: "tool_use",
              input: { path: "node_modules/reka-ui/dist/index.js" },
            },
            { type: "tool_use", input: { pattern: "worker/src/leaked.ts" } },
          ],
        },
      ],
    };
    expect(deriveSessionFields(request, {}).files).toEqual([]);
  });

  it("derives errors from provider envelopes and stream events", () => {
    expect(
      deriveSessionFields(
        {},
        { error: { code: "rate_limited", message: "slow down" } },
      ).errors,
    ).toEqual(["rate_limited: slow down"]);
    expect(
      deriveSessionFields({}, { error: "upstream unavailable" }).errors,
    ).toEqual(["upstream unavailable"]);
    expect(
      deriveSessionFields(
        {},
        { events: [{ type: "error", error: { message: "overloaded" } }] },
      ).errors,
    ).toEqual(["overloaded"]);
  });

  it("derives errors only from flagged tool failures", () => {
    const failed = {
      messages: [
        {
          role: "user",
          content: [
            {
              type: "tool_result",
              is_error: true,
              content: 'Traceback (most recent call last)\n  File "a.py"',
            },
          ],
        },
      ],
    };
    expect(deriveSessionFields(failed, {}).errors).toEqual([
      "Traceback (most recent call last)",
    ]);
    const succeeded = {
      messages: [
        {
          role: "user",
          content: [
            {
              type: "tool_result",
              content: 'const error = ref("");\nError extends Error {',
            },
          ],
        },
      ],
    };
    expect(deriveSessionFields(succeeded, {}).errors).toEqual([]);
  });

  it("does not treat source code containing the word error as an error", () => {
    const request = {
      messages: [
        {
          role: "user",
          content:
            'const evidenceError = computed(() => {\n  if (error && !exchanges.length) return "x";\n});',
        },
        {
          role: "assistant",
          content:
            "Error / Disabled: explicit text and icon state; never color alone.",
        },
      ],
    };
    expect(
      deriveSessionFields(request, { choices: [{ finish_reason: "stop" }] })
        .errors,
    ).toEqual([]);
  });

  it("classifies known title agents without suppressing ordinary title work", () => {
    expect(
      classifyRequestKind("primary", {
        messages: [
          {
            role: "system",
            content: "You are a title generator. Output only a title.",
          },
        ],
      }),
    ).toBe("title");
    expect(
      classifyRequestKind("primary", {
        messages: [{ role: "user", content: "Fix the title generator" }],
      }),
    ).toBe("primary");
    expect(
      classifyRequestKind("summary", {
        messages: [{ role: "user", content: "real prompt" }],
      }),
    ).toBe("summary");
  });

  it("derives intent from string and block user content", () => {
    expect(
      deriveIntent({
        messages: [{ role: "user", content: "  real   prompt " }],
      }),
    ).toBe("real prompt");
    expect(
      deriveIntent({
        messages: [
          { role: "user", content: [{ type: "text", text: "block prompt" }] },
        ],
      }),
    ).toBe("block prompt");
  });
});
