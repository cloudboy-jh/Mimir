// Pure transcript projection of saved exchange archives (R2 envelopes) into
// structured messages, agent-facing turns and search text. Recognizes OpenAI
// chat, Anthropic messages, Responses API, OpenCode parts, reconstructed SSE
// streams and reported tool activity. No Worker bindings or storage access.

export type Envelope = {
  exchange_id: string;
  request?: unknown;
  response?: unknown;
  tool_activity?: unknown;
};

type BlockType = "text" | "reasoning" | "tool-call" | "tool-result" | "context" | "data" | "error";
export type Block = { type: BlockType; title?: string; callId?: string; failed?: boolean; text: string };
export type Message = { role: string; name?: string; blocks: Block[] };

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}
function printable(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === undefined) return "";
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
function string(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined;
}

function part(value: unknown): Block[] {
  if (typeof value === "string") return value ? [{ type: "text", text: value }] : [];
  const item = record(value);
  if (!item) return [{ type: "data", text: printable(value) }];
  const kind = String(item.type ?? "");
  const callId = string(item.toolCallId ?? item.tool_call_id ?? item.callID ?? item.call_id ?? item.id);
  const title = string(item.name ?? item.toolName ?? item.tool_name ?? item.tool);
  if (["text", "output_text", "input_text"].includes(kind)) return [{ type: "text", text: printable(item.text ?? item.content) }];
  if (["thinking", "reasoning", "reasoning_content", "reasoning_text", "summary_text"].includes(kind)) {
    const summary = Array.isArray(item.summary) ? item.summary.map((entry) => printable(record(entry)?.text ?? entry)).join("\n") : undefined;
    return [{ type: "reasoning", text: printable(item.thinking ?? item.text ?? item.reasoning ?? summary ?? item.content ?? item) }];
  }
  if (["tool_use", "toolCall", "tool_call", "function_call"].includes(kind))
    return [{ type: "tool-call", title, callId, text: printable(item.arguments ?? item.input ?? {}) }];
  if (["tool_result", "toolResult", "function_call_output"].includes(kind))
    return [{ type: "tool-result", title, callId, failed: item.is_error === true || item.isError === true, text: printable(item.content ?? item.output ?? item.result ?? item) }];
  if (kind === "tool") {
    const state = record(item.state);
    const blocks: Block[] = [{ type: "tool-call", title, callId, text: printable(state?.input ?? item.input ?? {}) }];
    if (state?.output !== undefined || state?.error !== undefined)
      blocks.push({ type: "tool-result", title, callId, failed: state.status === "error" || state.error !== undefined, text: printable(state.error ?? state.output) });
    return blocks;
  }
  if (["compaction", "context", "step-start", "step-finish"].includes(kind)) return [{ type: "context", title: kind, text: printable(item) }];
  if (kind === "error" || item.error !== undefined) return [{ type: "error", title, text: printable(item.error ?? item) }];
  return [{ type: "data", title: kind || undefined, text: printable(item) }];
}

function contentBlocks(content: unknown): Block[] {
  if (content === undefined || content === null) return [];
  return Array.isArray(content) ? content.flatMap(part) : part(content);
}

function message(value: unknown, defaultRole = "message"): Message {
  const item = record(value);
  if (!item) return { role: defaultRole, blocks: [{ type: "data", text: printable(value) }] };
  const info = record(item.info);
  const role = string(item.role ?? info?.role) ?? defaultRole;
  let blocks: Block[];
  if (["function_call", "function_call_output", "reasoning", "toolCall", "toolResult"].includes(String(item.type))) blocks = part(item);
  else blocks = contentBlocks(item.parts ?? item.content ?? item.text ?? item.output);
  if (role === "tool" || role === "toolResult")
    blocks = [{ type: "tool-result", title: string(item.name ?? item.toolName), callId: string(item.tool_call_id ?? item.toolCallId ?? item.call_id), failed: item.isError === true || item.is_error === true, text: printable(item.content ?? item.output) }];
  if (item.reasoning_content || item.thinking) blocks.unshift({ type: "reasoning", text: printable(item.reasoning_content ?? item.thinking) });
  for (const raw of Array.isArray(item.tool_calls) ? item.tool_calls : []) {
    const tool = record(raw);
    const fn = record(tool?.function);
    blocks.push({ type: "tool-call", title: string(fn?.name ?? tool?.name), callId: string(tool?.id), text: printable(fn?.arguments ?? tool?.arguments ?? raw) });
  }
  if (item.errorMessage || item.error) blocks.push({ type: "error", text: printable(item.errorMessage ?? item.error) });
  if (!blocks.length) blocks.push({ type: "data", text: printable(item) });
  return { role, name: string(item.name), blocks };
}

function messagesFrom(value: unknown): Message[] {
  const root = record(value);
  const values = Array.isArray(root?.messages) ? root.messages : Array.isArray(root?.input) ? root.input : Array.isArray(value) ? value : null;
  if (values) return values.map((entry) => message(entry));
  if (typeof root?.input === "string") return [message({ role: "user", content: root.input })];
  if (root?.role || root?.parts) return [message(value)];
  return [];
}

const STREAM_DELTAS: Record<string, true> = {
  "response.output_text.delta": true,
  "response.reasoning_summary_text.delta": true,
  "response.reasoning_text.delta": true,
  "response.function_call_arguments.delta": true,
};

function streamedResponse(value: Record<string, unknown>): Message[] {
  const messages = new Map<string, Message>();
  const slots = new Map<string, Block>();
  const ordered: Message[] = [];
  const getMessage = (key: string) => {
    let entry = messages.get(key);
    if (!entry) {
      entry = { role: "assistant", blocks: [] };
      messages.set(key, entry);
      ordered.push(entry);
    }
    return entry;
  };
  const slot = (key: string, owner: string, type: BlockType) => {
    let block = slots.get(key);
    if (!block) {
      block = { type, text: "" };
      slots.set(key, block);
      getMessage(owner).blocks.push(block);
    }
    return block;
  };
  for (const raw of Array.isArray(value.events) ? value.events : []) {
    let decoded = raw;
    if (typeof raw === "string") {
      const data = raw.replace(/^data:\s*/, "").trim();
      if (data === "[DONE]") continue;
      try {
        decoded = JSON.parse(data);
      } catch {
        continue;
      }
    }
    const wrapper = record(decoded);
    let event = record(wrapper?.data) ?? wrapper;
    if (typeof wrapper?.data === "string") {
      if (wrapper.data.trim() === "[DONE]") continue;
      try {
        event = record(JSON.parse(wrapper.data));
      } catch {
        continue;
      }
    }
    if (!event) continue;
    if (Array.isArray(event.choices)) {
      for (const [index, rawChoice] of event.choices.entries()) {
        const choice = record(rawChoice);
        const delta = record(choice?.delta ?? choice?.message);
        if (!delta) continue;
        const owner = `choice:${choice?.index ?? index}`;
        const entry = getMessage(owner);
        if (typeof delta.role === "string") entry.role = delta.role;
        if (typeof delta.reasoning_content === "string") slot(`${owner}:reasoning`, owner, "reasoning").text += delta.reasoning_content;
        if (typeof delta.reasoning === "string") slot(`${owner}:reasoning`, owner, "reasoning").text += delta.reasoning;
        if (typeof delta.content === "string") slot(`${owner}:text`, owner, "text").text += delta.content;
        if (Array.isArray(delta.content)) entry.blocks.push(...contentBlocks(delta.content));
        for (const rawCall of Array.isArray(delta.tool_calls) ? delta.tool_calls : []) {
          const call = record(rawCall);
          if (!call) continue;
          const fn = record(call.function);
          const block = slot(`${owner}:call:${call.index ?? call.id ?? 0}`, owner, "tool-call");
          block.callId = string(call.id) ?? block.callId;
          if (typeof fn?.name === "string") block.title = (block.title ?? "") + fn.name;
          if (typeof fn?.arguments === "string") block.text += fn.arguments;
        }
      }
      continue;
    }
    const kind = String(event.type ?? wrapper?.event ?? "");
    const key = `anthropic:${event.index ?? 0}`;
    if (kind === "content_block_start") {
      const parsed = part(event.content_block)[0];
      if (parsed) {
        const block = slot(key, "anthropic", parsed.type);
        Object.assign(block, parsed);
        if (parsed.type === "tool-call" && block.text === "{}") block.text = "";
      }
      continue;
    }
    if (kind === "content_block_delta") {
      const delta = record(event.delta);
      const type: BlockType = delta?.type === "input_json_delta" ? "tool-call" : delta?.type === "thinking_delta" ? "reasoning" : "text";
      if (delta?.text !== undefined || delta?.thinking !== undefined || delta?.partial_json !== undefined)
        slot(key, "anthropic", type).text += printable(delta.text ?? delta.thinking ?? delta.partial_json);
      continue;
    }
    if (kind === "response.output_item.added") {
      const item = record(event.item);
      const owner = `output:${event.output_index ?? item?.id ?? 0}`;
      if (item?.type === "message") getMessage(owner).role = string(item.role) ?? "assistant";
      else {
        const parsed = part(item)[0];
        if (parsed) Object.assign(slot(owner, owner, parsed.type), parsed);
      }
      continue;
    }
    if (kind === "response.content_part.added") {
      const owner = `output:${event.output_index ?? 0}`;
      const parsed = part(event.part)[0];
      if (parsed) Object.assign(slot(`${owner}:content:${event.content_index ?? 0}`, owner, parsed.type), parsed);
      continue;
    }
    if (STREAM_DELTAS[kind] === true) {
      const owner = `output:${event.output_index ?? 0}`;
      const type: BlockType = kind.includes("arguments") ? "tool-call" : kind.includes("reasoning") ? "reasoning" : "text";
      slot(type === "text" ? `${owner}:content:${event.content_index ?? 0}` : owner, owner, type).text += printable(event.delta);
      continue;
    }
    if (kind === "error" || kind === "response.failed") {
      getMessage("error").blocks.push({ type: "error", text: printable(event.error ?? event.response ?? event) });
    }
  }
  const result = ordered.filter((entry) => entry.blocks.length);
  if (!result.some((entry) => entry.blocks.some((block) => block.type === "text")) && typeof value.content === "string" && value.content)
    result.push({ role: "assistant", blocks: [{ type: "text", text: value.content }] });
  if (!result.length && value.content && typeof value.content !== "string") result.push(...responsePayload(value.content));
  return result;
}

function responsePayload(payload: unknown): Message[] {
  const root = record(payload);
  if (root?.message)
    return [
      message(root.message, "assistant"),
      ...(Array.isArray(root.tool_results)
        ? root.tool_results.map((result) => message({ role: "tool", ...record(result), content: record(result)?.content ?? record(result)?.output ?? result }))
        : []),
    ];
  if (Array.isArray(root?.choices)) return root.choices.map((choice) => message(record(choice)?.message ?? record(choice)?.delta ?? choice, "assistant"));
  if (Array.isArray(root?.output)) return root.output.map((entry) => message(entry, "assistant"));
  const direct = messagesFrom(payload);
  if (direct.length) return direct;
  if (typeof payload === "string") return [{ role: "assistant", blocks: [{ type: "text", text: payload }] }];
  if (root?.content !== undefined || root?.parts !== undefined || root?.text !== undefined || root?.error || root?.type) return [message(root, "assistant")];
  return [];
}

export function requestMessages(envelope: Envelope): Message[] {
  return messagesFrom(envelope.request);
}

export function responseMessages(envelope: Envelope): Message[] {
  const response = record(envelope.response);
  const messages = response?.format === "reconstructed_sse" ? streamedResponse(response) : responsePayload(response?.body ?? response?.content ?? envelope.response);
  if (Array.isArray(envelope.tool_activity)) {
    for (const [index, raw] of envelope.tool_activity.entries()) {
      const activity = record(raw);
      if (!activity || typeof activity.name !== "string") continue;
      const callId = `activity:${envelope.exchange_id}:${index}`;
      const blocks: Block[] = [{ type: "tool-call", callId, title: activity.name, text: printable(activity.input) }];
      if (activity.output !== undefined) blocks.push({ type: "tool-result", callId, title: activity.name, failed: activity.status === "failed", text: printable(activity.output) });
      else if (activity.status === "failed") blocks.push({ type: "tool-result", callId, title: activity.name, failed: true, text: "" });
      messages.push({ role: "tool", blocks });
    }
  }
  return messages;
}

function sameMessage(left: Message, right: Message): boolean {
  if (left.role !== right.role || left.name !== right.name || left.blocks.length !== right.blocks.length) return false;
  return left.blocks.every((block, index) => {
    const other = right.blocks[index]!;
    return block.type === other.type && block.title === other.title && block.callId === other.callId && block.text === other.text && block.failed === other.failed;
  });
}

/** Length of the exact, adjacent context replay at the start of `incoming`. */
export function adjacentReplay(previous: Message[], incoming: Message[]): number {
  const maximum = Math.min(previous.length, incoming.length);
  for (let count = maximum; count >= 2; count--) {
    let matches = true;
    for (let index = 0; index < count; index++) {
      if (!sameMessage(previous[previous.length - count + index]!, incoming[index]!)) {
        matches = false;
        break;
      }
    }
    if (matches) return count;
  }
  // A linked tool result is unambiguous; a repeated single user turn is not.
  const last = previous.at(-1);
  const first = incoming[0];
  if (last && first && last.blocks.length && last.blocks.every((block) => block.type === "tool-result" && block.callId) && sameMessage(last, first)) return 1;
  return 0;
}

/** Messages a request adds after the latest assistant message it carries. */
function newRequestMessages(messages: Message[]): Message[] {
  let index = messages.length;
  while (index > 0 && messages[index - 1]!.role !== "assistant") index--;
  return messages.slice(index).filter((entry) => entry.role !== "system" && entry.role !== "developer");
}

function joinText(messages: Message[], type: BlockType): string {
  return messages
    .flatMap((entry) => entry.blocks)
    .filter((block) => block.type === type && block.text.trim())
    .map((block) => block.text.trim())
    .join("\n\n");
}

export const MAX_TURN_TEXT = 64 * 1024;
export const MAX_TOOL_TEXT = 4 * 1024;

export type ToolCall = { name: string; input: string | null; output: string | null; error: boolean };
export type TurnContent = {
  user: string | null;
  assistant: string | null;
  reasoning: string | null;
  tools: ToolCall[];
  truncated: boolean;
};
export type TranscriptCapture = { exchange_id: string; session_id: string; primary: boolean; envelope: Envelope | null };

// buildTurnContents projects chronologically ordered captures into turns.
// Replay of the prior primary capture on the same branch is removed, and tool
// results arriving in a later request are paired with their earlier call.
export function buildTurnContents(captures: TranscriptCapture[]): Map<string, TurnContent> {
  const turns = new Map<string, TurnContent>();
  const prior = new Map<string, Message[]>();
  const calls = new Map<string, ToolCall>();
  for (const capture of captures) {
    const branch = capture.session_id;
    if (!capture.envelope) {
      if (capture.primary) prior.delete(branch);
      continue;
    }
    let truncated = false;
    const clip = (text: string, limit: number) => {
      if (text.length <= limit) return text;
      truncated = true;
      return text.slice(0, limit);
    };
    const request = requestMessages(capture.envelope);
    const response = responseMessages(capture.envelope);
    const replayed = capture.primary ? adjacentReplay(prior.get(branch) ?? [], request) : 0;
    if (capture.primary) prior.set(branch, [...request, ...response]);
    const added = newRequestMessages(request.slice(replayed));
    const tools: ToolCall[] = [];
    const result = (block: Block) => {
      const output = clip(block.text, MAX_TOOL_TEXT);
      const call = block.callId ? calls.get(`${branch}\u0000${block.callId}`) : undefined;
      if (call && call.output === null) {
        call.output = output;
        call.error = block.failed === true;
      } else tools.push({ name: block.title ?? "tool", input: null, output, error: block.failed === true });
    };
    for (const block of added.flatMap((entry) => entry.blocks)) if (block.type === "tool-result") result(block);
    for (const block of response.flatMap((entry) => entry.blocks)) {
      if (block.type === "tool-call") {
        const call: ToolCall = { name: block.title ?? "tool", input: block.text ? clip(block.text, MAX_TOOL_TEXT) : null, output: null, error: false };
        tools.push(call);
        if (block.callId) calls.set(`${branch}\u0000${block.callId}`, call);
      } else if (block.type === "tool-result") result(block);
    }
    const user = joinText(added.filter((entry) => entry.role === "user"), "text");
    const assistant = [joinText(response, "text"), joinText(response, "error")].filter(Boolean).join("\n\n");
    const reasoning = joinText(response, "reasoning");
    turns.set(capture.exchange_id, {
      user: user ? clip(user, MAX_TURN_TEXT) : null,
      assistant: assistant ? clip(assistant, MAX_TURN_TEXT) : null,
      reasoning: reasoning ? clip(reasoning, MAX_TURN_TEXT) : null,
      tools,
      truncated,
    });
  }
  return turns;
}

export const MAX_SEARCH_TEXT = 256 * 1024;

/** Text one exchange adds to its session: new user input, tool activity and the response. */
export function searchText(envelope: Envelope): string {
  const added = newRequestMessages(requestMessages(envelope));
  const response = responseMessages(envelope);
  const parts: string[] = [];
  for (const entry of [...added, ...response]) {
    for (const block of entry.blocks) {
      if (block.type === "text" || block.type === "error" || block.type === "tool-result") parts.push(block.text);
      else if (block.type === "tool-call") parts.push(`${block.title ?? "tool"} ${block.text}`);
    }
  }
  return parts.join("\n").slice(0, MAX_SEARCH_TEXT);
}

/** The final assistant text of a response, for summaries. */
export function finalAssistantText(envelope: Envelope): string | null {
  const response = responseMessages(envelope);
  for (let index = response.length - 1; index >= 0; index--) {
    const text = joinText([response[index]!], "text");
    if (text && response[index]!.role === "assistant") return text;
  }
  return null;
}
