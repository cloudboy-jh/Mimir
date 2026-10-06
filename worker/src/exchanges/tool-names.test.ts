import { describe, expect, it } from "vitest";
import { extractToolNames } from "./tool-names";

describe("observed tool names", () => {
  it("indexes provider calls and harness results but excludes advertised tools and argument data", () => {
    const request = {
      tools: [{ type: "function", function: { name: "never_called", parameters: {} } }],
      messages: [{ role: "assistant", tool_calls: [{ function: { name: "read", arguments: "{}" } }] }],
    };
    const response = {
      parts: [
        { type: "tool", tool: "edit", state: { input: { type: "toolCall", name: "source_not_a_call" } } },
        { type: "toolCall", name: "read", arguments: { type: "tool_use", name: "also_source" } },
      ],
    };
    expect(extractToolNames(request, response, [
      { name: "shell", status: "succeeded", input: { command: "go test ./..." }, output: { type: "toolCall", name: "untrusted_output" } },
    ])).toEqual(["edit", "read", "shell"]);
  });

  it("retains names in fragmented streams without inventing names from prose", () => {
    expect(extractToolNames({ messages: [{ role: "user", content: "Use imaginary_tool" }] }, {
      events: [
        { choices: [{ delta: { tool_calls: [{ index: 0, function: { name: "terminal" } }] } }] },
        { choices: [{ delta: { tool_calls: [{ index: 0, function: { arguments: "{}" } }] } }] },
        { type: "content_block_start", content_block: { type: "tool_use", name: "inspect" } },
      ],
    })).toEqual(["inspect", "terminal"]);
  });

  it("rejects malformed identifiers and bounds deeply nested source objects", () => {
    let nested: unknown = { type: "toolCall", name: "too_deep" };
    for (let depth = 0; depth < 20; depth++) nested = { nested };
    expect(extractToolNames(nested, { parts: [
      { type: "toolCall", name: "line\nbreak" },
      { type: "toolCall", name: "x".repeat(129) },
      { type: "toolCall", name: "valid" },
    ] })).toEqual(["valid"]);
  });
});
