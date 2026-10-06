import { describe, expect, it } from "vitest";
import type { LogEnvelope } from "../src/lib/api";
import { structuredEvidence } from "../src/lib/structured-exchange";

function envelope(request: unknown, response: LogEnvelope["response"]): LogEnvelope {
  return { schema_version: 1, exchange_id: "exchange-1", session_id: "session-1", captured_at: "2026-09-17T00:00:00Z", endpoint: "/v1/chat/completions", request, response };
}

describe("saved evidence normalization", () => {
  it("assembles OpenAI indexed calls and reasoning without merging separate choices", () => {
    const capture = envelope({}, { format: "reconstructed_sse", content: "", events: [
      { choices: [{ index: 0, delta: { role: "assistant", reasoning_content: "Inspect first.", tool_calls: [{ index: 0, id: "call-1", function: { name: "read", arguments: "{\"path\":" } }] } }] },
      { choices: [{ index: 0, delta: { tool_calls: [{ index: 0, function: { arguments: "\"src/app.ts\"}" } }] }, finish_reason: "tool_calls" }, { index: 1, delta: { content: "Alternative" } }] },
    ] });
    const evidence = structuredEvidence(capture, "response");
    expect(evidence.messages[0]?.blocks.map(({ type }) => type)).toEqual(["reasoning", "tool-call"]);
    expect(evidence.messages[0]?.blocks[1]).toMatchObject({ callId: "call-1", title: "read", text: "{\"path\":\"src/app.ts\"}" });
    expect(evidence.messages[1]?.blocks[0]?.text).toBe("Alternative");
  });

  it("retains Anthropic block order and saved thinking alongside tool input", () => {
    const evidence = structuredEvidence(envelope({}, { format: "reconstructed_sse", content: "Checking the file.", events: [
      { type: "content_block_start", index: 0, content_block: { type: "thinking", thinking: "" } },
      { type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "Check its callers." } },
      { type: "content_block_start", index: 1, content_block: { type: "tool_use", id: "tool-1", name: "read", input: {} } },
      { type: "content_block_delta", index: 1, delta: { type: "input_json_delta", partial_json: "{\"path\":\"src/app.ts\"}" } },
      { type: "content_block_delta", index: 2, delta: { type: "text_delta", text: "Checking the file." } },
    ] }), "response");
    expect(evidence.messages[0]?.blocks.map(({ type }) => type)).toEqual(["reasoning", "tool-call", "text"]);
    expect(evidence.messages[0]?.blocks[0]?.text).toBe("Check its callers.");
    expect(evidence.messages[0]?.blocks[1]).toMatchObject({ callId: "tool-1", text: "{\"path\":\"src/app.ts\"}" });
  });

  it("reads OpenCode parts and exposes real tool errors without discarding unsupported parts", () => {
    const capture = envelope({ messages: [{ info: { id: "msg-1", role: "assistant" }, parts: [
      { type: "text", text: "Inspecting." },
      { type: "tool", callID: "call-read", tool: "read", state: { status: "error", input: { path: "missing.ts" }, error: "File not found" } },
      { type: "file", url: "asset://capture.png", mime: "image/png" },
      { type: "compaction", auto: true },
    ] }] }, { format: "json", body: {} });
    const blocks = structuredEvidence(capture, "request").messages[0]!.blocks;
    expect(blocks.map(({ type }) => type)).toEqual(["text", "tool-call", "tool-result", "data", "context"]);
    expect(blocks[2]).toMatchObject({ callId: "call-read", failed: true, text: "File not found" });
    expect(blocks[3]?.text).toContain("asset://capture.png");
  });

  it("links Pi/OMP call identifiers and retains thinking and tool-result failures", () => {
    const evidence = structuredEvidence(envelope({ messages: [
      { role: "assistant", content: [{ type: "thinking", thinking: "Inspect the boundary." }, { type: "toolCall", id: "pi-1", name: "bash", arguments: { command: "npm test" } }] },
      { role: "toolResult", toolCallId: "pi-1", toolName: "bash", isError: true, content: [{ type: "text", text: "Exit 1: failing assertion" }] },
    ] }, { format: "json", body: {} }), "request");
    expect(evidence.messages[0]?.blocks[0]).toMatchObject({ type: "reasoning", text: "Inspect the boundary." });
    expect(evidence.messages[0]?.blocks[1]?.callId).toBe(evidence.messages[1]?.blocks[0]?.callId);
    expect(evidence.messages[1]?.blocks[0]).toMatchObject({ type: "tool-result", failed: true });
    expect(evidence.messages[1]?.blocks[0]?.text).toContain("failing assertion");
  });

  it("reconstructs Responses API reasoning, tool argument deltas and text", () => {
    const evidence = structuredEvidence(envelope({}, { format: "reconstructed_sse", content: "", events: [
      { type: "response.output_item.added", output_index: 0, item: { type: "reasoning", id: "reason-1", summary: [] } },
      { type: "response.reasoning_summary_text.delta", output_index: 0, summary_index: 0, delta: "Check the file." },
      { type: "response.output_item.added", output_index: 1, item: { type: "function_call", id: "item-1", call_id: "responses-call", name: "read", arguments: "" } },
      { type: "response.function_call_arguments.delta", output_index: 1, delta: "{\"path\":\"a.ts\"}" },
      { type: "response.output_text.delta", output_index: 2, content_index: 0, delta: "Done." },
    ] }), "response");
    const blocks = evidence.messages.flatMap(({ blocks }) => blocks);
    expect(blocks.find(({ type, text }) => type === "reasoning" && text === "Check the file.")).toBeDefined();
    expect(blocks.find(({ type }) => type === "tool-call")).toMatchObject({ callId: "responses-call", text: "{\"path\":\"a.ts\"}" });
    expect(blocks.find(({ type }) => type === "text")?.text).toBe("Done.");
  });

  it("preserves malformed message and stream evidence instead of silently dropping it", () => {
    const evidence = structuredEvidence(envelope({ messages: [null, "not a message", { role: "user", content: [{ type: "future", payload: { value: 42 } }] }] }, { format: "reconstructed_sse", content: "", events: ["data: {broken", { type: "future_delta", payload: "keep me" }, { type: "error", error: { message: "upstream failed" } }] }), "request");
    expect(evidence.messages).toHaveLength(3);
    expect(evidence.messages[0]?.blocks[0]?.text).toBe("null");
    expect(evidence.messages[1]?.blocks[0]?.text).toBe("not a message");
    expect(evidence.messages[2]?.blocks[0]?.text).toContain("42");
    const response = structuredEvidence(envelope({}, { format: "reconstructed_sse", content: "", events: ["data: {broken", { type: "future_delta", payload: "keep me" }, { type: "error", error: { message: "upstream failed" } }] }), "response");
    expect(response.messages.flatMap(({ blocks }) => blocks).map(({ text }) => text).join("\n")).toContain("keep me");
    expect(response.messages.flatMap(({ blocks }) => blocks).find(({ type }) => type === "error")?.text).toContain("upstream failed");
  });

  it("gives tool_activity distinct linked call IDs and preserves missing outputs", () => {
    const capture = envelope({}, { format: "json", body: { role: "assistant", content: "Checked." } });
    capture.tool_activity = [{ name: "read", input: { path: "a.ts" }, status: "succeeded" }, { name: "bash", input: {}, status: "failed", output: "Permission denied" }];
    const messages = structuredEvidence(capture, "response").messages;
    expect(messages[1]?.blocks[0]?.callId).toBe(messages[1]?.blocks[1]?.callId);
    expect(messages[1]?.blocks[0]?.callId).not.toBe(messages[2]?.blocks[0]?.callId);
    expect(messages[2]?.blocks[1]).toMatchObject({ failed: true, outputCaptured: true, text: "Permission denied" });
    expect(messages[1]?.blocks[1]).toMatchObject({ type: "tool-result", outputCaptured: false });
  });

  it("keeps stable IDs and direct source provenance across reconstruction", () => {
    const capture = envelope({ messages: [{ role: "user", content: "Hello" }] }, { format: "json", body: { role: "assistant", content: "Hello" } });
    const first = structuredEvidence(capture, "request").messages[0]!;
    const reloaded = structuredEvidence(JSON.parse(JSON.stringify(capture)), "request").messages[0]!;
    expect(first.id).toBe(reloaded.id);
    expect(first.blocks[0]?.id).toBe(reloaded.blocks[0]?.id);
    expect(first.blocks[0]).toMatchObject({ exchangeId: "exchange-1", side: "request" });
    expect(first.id).not.toBe(structuredEvidence(capture, "response").messages[0]?.id);
  });
});
