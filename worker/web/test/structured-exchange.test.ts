import { describe, expect, it } from "vitest";
import type { LogEnvelope } from "../src/lib/api";
import { structuredEvidence } from "../src/lib/structured-exchange";

function envelope(events: unknown[], content = ""): LogEnvelope {
  return {
    schema_version: 1,
    exchange_id: "exchange-1",
    session_id: "session-1",
    captured_at: "2026-09-17T00:00:00Z",
    endpoint: "/v1/chat/completions",
    request: {},
    response: { format: "reconstructed_sse", content, events },
  };
}

describe("structuredEvidence streamed responses", () => {
  it("assembles OpenAI tool-call deltas without requiring text content", () => {
    const evidence = structuredEvidence(envelope([
      { choices: [{ delta: { role: "assistant", tool_calls: [{ index: 0, id: "call-1", function: { name: "read", arguments: "{\"path\":" } }] } }] },
      { choices: [{ delta: { tool_calls: [{ index: 0, function: { arguments: "\"src/app.ts\"}" } }] }, finish_reason: "tool_calls" }] },
    ]), "response");

    expect(evidence).toEqual({
      recognized: true,
      messages: [{
        role: "assistant",
        blocks: [{ type: "tool-call", title: "read", text: "{\"path\":\"src/app.ts\"}" }],
      }],
    });
  });

  it("assembles Anthropic streamed tool-use blocks beside text", () => {
    const evidence = structuredEvidence(envelope([
      { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "Checking the file." } },
      { type: "content_block_start", index: 1, content_block: { type: "tool_use", id: "tool-1", name: "read", input: {} } },
      { type: "content_block_delta", index: 1, delta: { type: "input_json_delta", partial_json: "{\"path\":\"src/app.ts\"}" } },
    ], "Checking the file."), "response");

    expect(evidence.messages[0]?.blocks).toEqual([
      { type: "text", text: "Checking the file." },
      { type: "tool-call", title: "read", text: "{\"path\":\"src/app.ts\"}" },
    ]);
  });
});
