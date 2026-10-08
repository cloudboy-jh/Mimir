import { describe, expect, it } from "vitest";
import type { LogEnvelope, SessionExchange } from "../src/lib/api";
import { adjacentReplay, conversationSections, conversationTurns, orderedConversation, projectConversation, replayPredecessor, type ConversationCapture, type ConversationSection } from "../src/lib/conversation";
import { structuredEvidence } from "../src/lib/structured-exchange";

function capture(id: string, branch: string, messages: unknown[], response: unknown): ConversationCapture {
  const exchange: SessionExchange = { id, session_id: branch, ts: "2026-10-01T00:00:00Z", model: "model", provider: "provider", finish_reason: "stop", latency_ms: 100, harness: "pi", input_tokens: 10, output_tokens: 10, request_excerpt: "", response_excerpt: "", request_kind: "primary", capture_status: "saved", capture_reason: null, failure_code: null };
  const envelope: LogEnvelope = { schema_version: 1, exchange_id: id, session_id: branch, captured_at: exchange.ts, endpoint: "/v1/messages", request: { messages }, response: { format: "json", body: response } };
  return { exchange, envelope };
}
const user = { role: "user", content: "Inspect the file." };
const assistant = { role: "assistant", content: "I will inspect it." };

describe("branch-local conversation projection", () => {
  it("removes adjacent replay, but preserves new repeated text and stable source IDs", () => {
    const first = capture("one", "main", [user], assistant);
    const second = capture("two", "main", [user, assistant, user], { role: "assistant", content: "Again." });
    const entries = projectConversation([first, second]);
    expect(entries[1]?.replayed).toBe(2);
    expect(entries[1]?.messages.map(({ blocks }) => blocks[0]?.text)).toEqual(["Inspect the file.", "Again."]);
    expect(entries[1]?.messages[0]?.id).toBe(structuredEvidence(second.envelope!, "request").messages[2]?.id);
  });
  it("never merges replay or links calls across sub-agent branches", () => {
    const entries = projectConversation([
      capture("main-one", "main", [user], assistant),
      capture("child-one", "child", [user, assistant], { role: "assistant", content: [{ type: "toolCall", id: "shared", name: "read", arguments: {} }] }),
      capture("main-two", "main", [user, assistant, { role: "toolResult", toolCallId: "shared", content: "main output" }], { role: "assistant", content: "Main finished." }),
    ]);
    expect(entries[1]?.replayed).toBe(0);
    expect(entries[2]?.replayed).toBe(2);
    expect(entries[1]?.pairedResults).toEqual({});
  });
  it("retains repeated single user turns and non-adjacent equal text", () => {
    const first = capture("one", "main", [user], assistant);
    const incoming = structuredEvidence(capture("two", "main", [user], assistant).envelope!, "request").messages;
    expect(adjacentReplay(structuredEvidence(first.envelope!, "request").messages, incoming)).toBe(0);
    const entries = projectConversation([first, capture("two", "main", [{ role: "user", content: "Different." }], { role: "assistant", content: "Other." }), capture("three", "main", [user, assistant], assistant)]);
    expect(entries[2]?.replayed).toBe(0);
  });
  it("resets adjacency across a missing capture and keeps its explicit failure", () => {
    const missing = capture("gap", "main", [], {});
    missing.envelope = undefined;
    missing.exchange.capture_status = "failed";
    missing.exchange.capture_reason = "archive unavailable";
    const entries = projectConversation([capture("one", "main", [user], assistant), missing, capture("two", "main", [user, assistant], assistant)]);
    expect(entries[1]?.exchange.capture_reason).toBe("archive unavailable");
    expect(entries[1]?.messages).toEqual([]);
    expect(entries[2]?.replayed).toBe(0);
  });
  it.each([true, false])("does not let loaded=%s auxiliary requests replace primary checkpoints", (loaded) => {
    const auxiliary = capture("compact", "main", [{ role: "user", content: "Compact history." }], { role: "assistant", content: "Summary." });
    auxiliary.exchange.request_kind = "compaction";
    if (!loaded) auxiliary.envelope = undefined;
    const entries = projectConversation([capture("one", "main", [user], assistant), auxiliary, capture("two", "main", [user, assistant, { role: "user", content: "Continue." }], assistant)]);
    expect(entries[1]?.exchange.request_kind).toBe("compaction");
    expect(entries[2]?.replayed).toBe(2);
  });
  it("pairs a later result by call ID while retaining its actual request-side provenance", () => {
    const call = { role: "assistant", content: [{ type: "toolCall", id: "call-1", name: "read", arguments: { path: "a.ts" } }] };
    const result = { role: "toolResult", toolCallId: "call-1", content: "file body" };
    const entries = projectConversation([capture("one", "main", [user], call), capture("two", "main", [user, call, result], assistant)]);
    expect(entries[1]?.replayed).toBe(2);
    expect(entries[0]?.pairedResults["call-1"]?.[0]).toMatchObject({ type: "tool-result", exchangeId: "two", side: "request", text: "file body" });
    expect(entries[1]?.messages[0]?.blocks[0]?.type).toBe("tool-result");
  });
  it("treats arbitrary branch and call identifiers as keys, not delimiters or prototypes", () => {
    const entries = projectConversation([
      capture("call-a", "main", [user], { role: "assistant", content: [{ type: "toolCall", id: "child:call", name: "read", arguments: {} }] }),
      capture("call-b", "main:child", [user], { role: "assistant", content: [{ type: "toolCall", id: "call", name: "read", arguments: {} }] }),
      capture("result-a", "main", [{ role: "toolResult", toolCallId: "child:call", content: "parent output" }], assistant),
      capture("prototype-call", "main", [user], { role: "assistant", content: [{ type: "toolCall", id: "__proto__", name: "read", arguments: {} }] }),
      capture("prototype-result", "main", [{ role: "toolResult", toolCallId: "__proto__", content: "safe output" }], assistant),
    ]);
    expect(entries[0]?.pairedResults["child:call"]?.[0]?.text).toBe("parent output");
    expect(entries[1]?.pairedResults["call"]).toBeUndefined();
    expect(entries[3]?.pairedResults["__proto__"]?.[0]?.text).toBe("safe output");
  });
});

describe("coherent assistant turns", () => {
  const reasoning = { role: "assistant", content: [{ type: "reasoning", text: "Inspect the caller." }] };
  const call = { role: "assistant", content: [{ type: "toolCall", id: "read-1", name: "read", arguments: { path: "a.ts" } }] };
  const result = { role: "toolResult", toolCallId: "read-1", content: "file body" };
  it("groups reasoning, tools and answer without rewriting source messages or anchors", () => {
    const messages = structuredEvidence(capture("one", "main", [reasoning, call, result, assistant], assistant).envelope!, "request").messages;
    const turns = conversationTurns(messages);
    expect(turns.map(turn => turn.role)).toEqual(["assistant"]);
    expect(turns[0]?.messages).toEqual(messages);
    expect(turns[0]?.messages[0]).toBe(messages[0]);
    expect(turns[0]?.messages.flatMap(message => message.blocks.map(block => block.id))).toEqual(messages.flatMap(message => message.blocks.map(block => block.id)));
  });
  it("keeps actual user, system and context boundaries between assistant fragments", () => {
    for (const boundary of [user, { role: "system", content: "New instructions." }, { role: "assistant", content: [{ type: "compaction", content: "Shortened context." }] }]) {
      const messages = structuredEvidence(capture("one", "main", [reasoning, boundary, assistant], assistant).envelope!, "request").messages;
      expect(conversationTurns(messages).map(turn => turn.messages.map(message => message.id))).toEqual(messages.map(message => [message.id]));
    }
  });
  it("joins adjacent same-branch captures while retaining every source and paired result once", () => {
    const entries = projectConversation([
      capture("one", "main", [user], call),
      capture("two", "main", [user, call, result], assistant),
    ]);
    const sections = conversationSections(entries);
    expect(sections.map(section => section.captures.map(exchange => exchange.id))).toEqual([["one", "two"]]);
    expect(sections[0]?.messages.map(message => message.id)).toEqual(entries.flatMap(entry => entry.messages.map(message => message.id)));
    expect(sections[0]?.pairedResults["read-1"]?.map(block => block.text)).toEqual(["file body"]);
    expect(conversationTurns(sections[0]!.messages).map(turn => turn.role)).toEqual(["user", "assistant"]);
    expect(entries[0]?.messages).toHaveLength(2);
  });
  it("does not join across branch switches, compaction, missing evidence or new user turns", () => {
    const first = capture("one", "main", [user], reasoning);
    const next = capture("two", "main", [user, reasoning], assistant);
    const compaction = capture("compact", "main", [], assistant);
    compaction.exchange.request_kind = "compaction";
    const missing = capture("missing", "main", [], assistant);
    missing.envelope = undefined;
    for (const middle of [capture("child", "child", [user], assistant), compaction, missing]) {
      const entries = projectConversation([first, middle, next]);
      expect(conversationSections(entries).map(section => section.captures.map(exchange => exchange.id))).toEqual([["one"], [middle.exchange.id], ["two"]]);
    }
    const withUser = projectConversation([first, capture("new-user", "main", [user, reasoning, { role: "user", content: "Another task." }], assistant)]);
    expect(conversationSections(withUser).map(section => section.captures.map(exchange => exchange.id))).toEqual([["one"], ["new-user"]]);
  });
});

describe("conversation order", () => {
  const followUp = (text: string) => ({ role: "user", content: text });
  const reply = (text: string) => ({ role: "assistant", content: text });
  const turns = [
    capture("one", "main", [user], assistant),
    capture("two", "main", [user, assistant, followUp("Second.")], reply("Two.")),
    capture("three", "main", [user, assistant, followUp("Second."), reply("Two."), followUp("Third.")], reply("Three.")),
  ];
  const texts = (sections: ConversationSection[]) => sections.map(section => [section.exchange.id, section.messages.map(message => message.blocks[0]?.text)]);
  it("reverses turns newest-first while trimming replay exactly as oldest-first does", () => {
    const oldest = orderedConversation([], turns, "asc");
    const newest = orderedConversation([], [...turns].reverse(), "desc");
    expect(texts(newest)).toEqual(texts(oldest).reverse());
    expect(texts(newest)[0]).toEqual(["three", ["Third.", "Three."]]);
  });
  it("trims a newest-first page's oldest turn against the older context capture", () => {
    const page = orderedConversation([turns[0]!], [turns[2]!, turns[1]!], "desc");
    expect(texts(page)).toEqual([["three", ["Third.", "Three."]], ["two", ["Second.", "Two."]]]);
  });
  it("finds the previous primary capture on the same branch only", () => {
    const auxiliary = capture("title", "main", [], assistant).exchange;
    auxiliary.request_kind = "title";
    const chronological = [capture("main-one", "main", [], assistant).exchange, capture("child-one", "child", [], assistant).exchange, auxiliary, capture("main-two", "main", [], assistant).exchange];
    expect(replayPredecessor(chronological, "main-two")?.id).toBe("main-one");
    expect(replayPredecessor(chronological, "child-one")).toBeUndefined();
  });
});
