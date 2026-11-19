import { describe, expect, it } from "vitest";
import { buildTurnContents, finalAssistantText, MAX_TOOL_TEXT, searchText, type TranscriptCapture } from "./transcript";

function capture(id: string, branch: string, messages: unknown[], response: unknown, primary = true): TranscriptCapture {
  return { exchange_id: id, session_id: branch, primary, envelope: { exchange_id: id, request: { messages }, response: { format: "json", body: response } } };
}
const system = { role: "system", content: "You are helpful." };
const user = { role: "user", content: "Inspect the file." };
const assistant = { role: "assistant", content: "I will inspect it." };
const call = { role: "assistant", content: [{ type: "toolCall", id: "call-1", name: "read", arguments: { path: "a.ts" } }] };
const result = { role: "toolResult", toolCallId: "call-1", content: "file body" };

describe("turn projection", () => {
  it("keeps only the new user input after replayed context", () => {
    const turns = buildTurnContents([
      capture("one", "main", [system, user], assistant),
      capture("two", "main", [system, user, assistant, { role: "user", content: "Again." }], { role: "assistant", content: "Done again." }),
    ]);
    expect(turns.get("one")).toMatchObject({ user: "Inspect the file.", assistant: "I will inspect it.", tools: [], truncated: false });
    expect(turns.get("two")).toMatchObject({ user: "Again.", assistant: "Done again." });
  });

  it("pairs a tool result from the next request with the earlier call", () => {
    const turns = buildTurnContents([
      capture("one", "main", [user], call),
      capture("two", "main", [user, call, result], assistant),
    ]);
    expect(turns.get("one")!.tools).toEqual([{ name: "read", input: JSON.stringify({ path: "a.ts" }, null, 2), output: "file body", error: false }]);
    expect(turns.get("two")).toMatchObject({ user: null, assistant: "I will inspect it.", tools: [] });
  });

  it("never pairs calls across sub-agent branches", () => {
    const turns = buildTurnContents([
      capture("parent", "main", [user], call),
      capture("child", "child", [user, call, result], assistant),
    ]);
    expect(turns.get("parent")!.tools[0]!.output).toBeNull();
    expect(turns.get("child")!.tools).toEqual([{ name: "tool", input: null, output: "file body", error: false }]);
  });

  it("reads streamed responses, reasoning and reported tool activity", () => {
    const turns = buildTurnContents([
      {
        exchange_id: "sse",
        session_id: "main",
        primary: true,
        envelope: {
          exchange_id: "sse",
          request: { messages: [user] },
          response: {
            format: "reconstructed_sse",
            content: "",
            events: [
              'data: {"choices":[{"delta":{"reasoning_content":"Think."}}]}',
              'data: {"choices":[{"delta":{"content":"Hello "}}]}',
              'data: {"choices":[{"delta":{"content":"world"}}]}',
              "data: [DONE]",
            ],
          },
          tool_activity: [{ name: "bash", input: { command: "npm test" }, status: "failed", output: "x".repeat(MAX_TOOL_TEXT + 1) }],
        },
      },
    ]);
    const turn = turns.get("sse")!;
    expect(turn).toMatchObject({ assistant: "Hello world", reasoning: "Think.", truncated: true });
    expect(turn.tools[0]).toMatchObject({ name: "bash", error: true });
    expect(turn.tools[0]!.output).toHaveLength(MAX_TOOL_TEXT);
  });

  it("indexes the text an exchange adds and finds the final assistant answer", () => {
    const envelope = capture("one", "main", [system, user, call, result], { role: "assistant", content: "The file is fine." }).envelope!;
    expect(searchText(envelope)).toBe("file body\nThe file is fine.");
    expect(finalAssistantText(envelope)).toBe("The file is fine.");
  });
});
