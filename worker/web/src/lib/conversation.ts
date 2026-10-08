import type { LogEnvelope, SessionExchange } from "./api";
import { structuredEvidence, type StructuredBlock, type StructuredMessage } from "./structured-exchange";

export type ConversationCapture = { exchange: SessionExchange; envelope?: LogEnvelope; error?: string; loading?: boolean };
export type ConversationEntry = ConversationCapture & { messages: StructuredMessage[]; replayed: number; pairedResults: Record<string, StructuredBlock[]> };
export type ConversationSection = ConversationEntry & { captures: SessionExchange[] };

/** Join only contiguous saved primary captures, never a branch or context boundary. */
export function conversationSections(entries: ConversationEntry[]): ConversationSection[] {
  const sections: ConversationSection[] = [];
  for (const entry of entries) {
    const previous = sections.at(-1);
    const last = previous?.messages.at(-1);
    const lastTurn = previous && conversationTurns(previous.messages).at(-1);
    const first = entry.messages[0];
    const continues = lastTurn?.role === "assistant" && (first?.role === "assistant" && lastTurn.name === first.name || first && ["tool", "toolResult"].includes(first.role) && first.blocks.every(block => block.type === "tool-call" || block.type === "tool-result"));
    const boundary = last?.blocks.some(block => block.type === "context") || first?.blocks.some(block => block.type === "context");
    if (previous?.envelope && entry.envelope && previous.exchange.request_kind === "primary" && entry.exchange.request_kind === "primary" && previous.exchange.session_id === entry.exchange.session_id && continues && !boundary) {
      previous.messages.push(...entry.messages);
      previous.captures.push(entry.exchange);
      for (const [callId, results] of Object.entries(entry.pairedResults)) (previous.pairedResults[callId] ??= []).push(...results);
    } else {
      const pairedResults: Record<string, StructuredBlock[]> = Object.create(null);
      for (const [id, results] of Object.entries(entry.pairedResults)) pairedResults[id] = [...results];
      sections.push({ ...entry, messages: [...entry.messages], pairedResults, captures: [entry.exchange] });
    }
  }
  return sections;
}

export type ConversationTurn = { id: string; role: string; name?: string; messages: StructuredMessage[] };

/** Keep source messages intact while presenting adjacent assistant fragments as one turn. */
export function conversationTurns(messages: StructuredMessage[]): ConversationTurn[] {
  const turns: ConversationTurn[] = [];
  for (const message of messages) {
    const previous = turns.at(-1);
    const boundary = message.blocks.some(block => block.type === "context");
    const assistant = message.role === "assistant";
    const tool = ["tool", "toolResult"].includes(message.role) && message.blocks.every(block => block.type === "tool-call" || block.type === "tool-result");
    const previousBoundary = previous?.messages.at(-1)?.blocks.some(block => block.type === "context");
    if (previous?.role === "assistant" && !boundary && !previousBoundary && (assistant && previous.name === message.name || tool)) {
      previous.messages.push(message);
    } else {
      turns.push({ id: message.id, role: message.role, name: message.name, messages: [message] });
    }
  }
  return turns;
}
function sameMessage(left: StructuredMessage, right: StructuredMessage): boolean {
  if (left.role !== right.role || left.name !== right.name || left.blocks.length !== right.blocks.length) return false;
  return left.blocks.every((block, index) => {
    const other = right.blocks[index]!;
    return block.type === other.type && block.title === other.title && block.callId === other.callId && block.text === other.text && block.failed === other.failed && block.outputCaptured === other.outputCaptured;
  });
}

/** Only remove an exact, adjacent context suffix replay, within one branch. */
export function adjacentReplay(previous: StructuredMessage[], incoming: StructuredMessage[]): number {
  const maximum = Math.min(previous.length, incoming.length);
  for (let count = maximum; count >= 2; count--) {
    let matches = true;
    for (let index = 0; index < count; index++) if (!sameMessage(previous[previous.length - count + index]!, incoming[index]!)) { matches = false; break; }
    if (matches) return count;
  }
  // A linked tool result is unambiguous; a repeated single user turn is not.
  const last = previous.at(-1);
  const first = incoming[0];
  if (last && first && last.blocks.length && last.blocks.every((block) => block.type === "tool-result" && block.callId) && sameMessage(last, first)) return 1;
  return 0;
}

export function projectConversation(captures: ConversationCapture[]): ConversationEntry[] {
  const prior = new Map<string, StructuredMessage[]>();
  const calls = new Map<string, Map<string, ConversationEntry>>();
  return captures.map((capture) => {
    const entry: ConversationEntry = { ...capture, messages: [], replayed: 0, pairedResults: Object.create(null) };
    const branch = capture.exchange.session_id;
    if (capture.exchange.request_kind !== "primary") {
      if (capture.envelope) entry.messages = [...structuredEvidence(capture.envelope, "request").messages, ...structuredEvidence(capture.envelope, "response").messages];
      return entry;
    }
    if (!capture.envelope) {
      prior.delete(branch);
      calls.delete(branch);
      return entry;
    }
    const request = structuredEvidence(capture.envelope, "request").messages;
    const response = structuredEvidence(capture.envelope, "response").messages;
    entry.replayed = adjacentReplay(prior.get(branch) ?? [], request);
    entry.messages = [...request.slice(entry.replayed), ...response];
    prior.set(branch, [...request, ...response]);
    for (const message of entry.messages) for (const block of message.blocks) {
      if (!block.callId) continue;
      let branchCalls = calls.get(branch);
      if (!branchCalls) { branchCalls = new Map(); calls.set(branch, branchCalls); }
      if (block.type === "tool-call") branchCalls.set(block.callId, entry);
      if (block.type === "tool-result") {
        const call = branchCalls.get(block.callId);
        if (call && call !== entry) (call.pairedResults[block.callId] ??= []).push(block);
      }
    }
    return entry;
  });
}

/** Project captures oldest-first so replay trimming compares each turn with its predecessor, then present sections in the requested order. */
export function orderedConversation(context: ConversationCapture[], page: ConversationCapture[], order: "asc" | "desc"): ConversationSection[] {
  const chronological = order === "desc" ? [...page].reverse() : page;
  const sections = conversationSections(projectConversation([...context, ...chronological]).slice(context.length));
  return order === "desc" ? sections.reverse() : sections;
}

/** The latest primary capture per branch; it is the replay checkpoint for any later capture on that branch. */
export function branchCheckpoints(chronological: SessionExchange[]): SessionExchange[] {
  const latest = new Map<string, SessionExchange>();
  for (const exchange of chronological) if (exchange.request_kind === "primary") latest.set(exchange.session_id, exchange);
  return [...latest.values()];
}

/** The primary capture whose archive `projectConversation` needs to trim replayed history from `id`. */
export function replayPredecessor(chronological: SessionExchange[], id: string): SessionExchange | undefined {
  const index = chronological.findIndex((exchange) => exchange.id === id);
  const branch = chronological[index]?.session_id;
  for (let cursor = index - 1; cursor >= 0; cursor--) {
    const exchange = chronological[cursor]!;
    if (exchange.session_id === branch && exchange.request_kind === "primary") return exchange;
  }
  return undefined;
}
