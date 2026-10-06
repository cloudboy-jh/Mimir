import type { LogEnvelope } from "./api";

export type StructuredBlock = {
  id: string;
  exchangeId: string;
  side: "request" | "response";
  type: "text" | "reasoning" | "tool-call" | "tool-result" | "context" | "data" | "error";
  title?: string;
  callId?: string;
  failed?: boolean;
  outputCaptured?: boolean;
  text: string;
};
export type StructuredMessage = { id: string; role: string; name?: string; blocks: StructuredBlock[] };
export type StructuredEvidence = { recognized: boolean; messages: StructuredMessage[] };
type Block = Omit<StructuredBlock, "id" | "exchangeId" | "side">;
type Message = Omit<StructuredMessage, "id" | "blocks"> & { blocks: Block[]; nativeId?: string };

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}
function printable(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === undefined) return "";
  try { return JSON.stringify(value, null, 2); } catch { return String(value); }
}
function string(value: unknown): string | undefined { return typeof value === "string" && value ? value : undefined; }
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
    return [{ type: "reasoning", title: "Saved reasoning", text: printable(item.thinking ?? item.text ?? item.reasoning ?? summary ?? item.content ?? item) }];
  }
  if (["tool_use", "toolCall", "tool_call", "function_call"].includes(kind)) return [{ type: "tool-call", title, callId, text: printable(item.arguments ?? item.input ?? {}) }];
  if (["tool_result", "toolResult", "function_call_output"].includes(kind)) return [{ type: "tool-result", title, callId, failed: item.is_error === true || item.isError === true, text: printable(item.content ?? item.output ?? item.result ?? item) }];
  if (kind === "tool") {
    const state = record(item.state);
    const blocks: Block[] = [{ type: "tool-call", title, callId, text: printable(state?.input ?? item.input ?? {}) }];
    if (state?.output !== undefined || state?.error !== undefined) blocks.push({ type: "tool-result", title, callId, failed: state.status === "error" || state.error !== undefined, text: printable(state.error ?? state.output) });
    else blocks.push({ type: "data", title: "Tool state", text: printable(state ?? item) });
    return blocks;
  }
  if (["compaction", "context", "step-start", "step-finish"].includes(kind)) return [{ type: "context", title: kind, text: printable(item) }];
  if (kind === "error" || item.error !== undefined) return [{ type: "error", title, text: printable(item.error ?? item) }];
  return [{ type: "data", title: kind || "Unsupported content", text: printable(item) }];
}
function contentBlocks(content: unknown): Block[] {
  if (content === undefined || content === null) return [];
  return Array.isArray(content) ? content.flatMap(part) : part(content);
}
function message(value: unknown, defaultRole = "message"): Message {
  const item = record(value);
  if (!item) return { role: defaultRole, blocks: [{ type: "data", title: "Unsupported message", text: printable(value) }] };
  const info = record(item.info);
  const role = string(item.role ?? info?.role) ?? defaultRole;
  let blocks: Block[];
  if (["function_call", "function_call_output", "reasoning", "toolCall", "toolResult"].includes(String(item.type))) blocks = part(item);
  else blocks = contentBlocks(item.parts ?? item.content ?? item.text ?? item.output);
  if (role === "tool" || role === "toolResult") blocks = [{ type: "tool-result", title: string(item.name ?? item.toolName), callId: string(item.tool_call_id ?? item.toolCallId ?? item.call_id), failed: item.isError === true || item.is_error === true, text: printable(item.content ?? item.output) }];
  if (item.reasoning_content || item.thinking) blocks.unshift({ type: "reasoning", title: "Saved reasoning", text: printable(item.reasoning_content ?? item.thinking) });
  for (const raw of Array.isArray(item.tool_calls) ? item.tool_calls : []) {
    const tool = record(raw); const fn = record(tool?.function);
    blocks.push({ type: "tool-call", title: string(fn?.name ?? tool?.name), callId: string(tool?.id), text: printable(fn?.arguments ?? tool?.arguments ?? raw) });
  }
  if (item.errorMessage || item.error) blocks.push({ type: "error", text: printable(item.errorMessage ?? item.error) });
  if (!blocks.length) blocks.push({ type: "data", text: printable(item) });
  return { role, name: string(item.name), nativeId: string(item.id ?? info?.id), blocks };
}
function messagesFrom(value: unknown): Message[] {
  const root = record(value);
  const values = Array.isArray(root?.messages) ? root.messages : Array.isArray(root?.input) ? root.input : Array.isArray(value) ? value : null;
  if (values) return values.map((value) => message(value));
  if (typeof root?.input === "string") return [message({ role: "user", content: root.input })];
  if (root?.role || root?.parts) return [message(value)];
  return [];
}

function streamedResponse(value: Extract<LogEnvelope["response"], { format: "reconstructed_sse" }>): Message[] {
  const messages = new Map<string, Message>();
  const slots = new Map<string, Block>();
  const unknown: Message[] = [];
  const getMessage = (key: string) => {
    let msg = messages.get(key);
    if (!msg) { msg = { role: "assistant", blocks: [] }; messages.set(key, msg); unknown.push(msg); }
    return msg;
  };
  const slot = (key: string, owner: string, type: Block["type"]) => {
    let block = slots.get(key);
    if (!block) { block = { type, text: "" }; slots.set(key, block); getMessage(owner).blocks.push(block); }
    return block;
  };
  if (value.events !== undefined && !Array.isArray(value.events)) unknown.push({ role: "capture", blocks: [{ type: "data", title: "Malformed stream events", text: printable(value.events) }] });
  for (const raw of Array.isArray(value.events) ? value.events : []) {
    let decoded = raw;
    if (typeof raw === "string") {
      const data = raw.replace(/^data:\s*/, "").trim();
      if (data === "[DONE]") continue;
      try { decoded = JSON.parse(data); } catch { unknown.push(message(raw, "capture")); continue; }
    }
    const wrapper = record(decoded);
    let event = record(wrapper?.data) ?? wrapper;
    if (typeof wrapper?.data === "string") {
      if (wrapper.data.trim() === "[DONE]") continue;
      try { event = record(JSON.parse(wrapper.data)); } catch { unknown.push(message(decoded, "capture")); continue; }
    }
    if (!event) { unknown.push(message(decoded, "capture")); continue; }
    if (Array.isArray(event.choices)) {
      for (const [index, rawChoice] of event.choices.entries()) {
        const choice = record(rawChoice); const delta = record(choice?.delta ?? choice?.message);
        if (!delta) { if (choice?.finish_reason === undefined) unknown.push(message(rawChoice, "capture")); continue; }
        const owner = `choice:${choice?.index ?? index}`;
        const msg = getMessage(owner);
        if (typeof delta.role === "string") msg.role = delta.role;
        if (typeof delta.reasoning_content === "string") slot(`${owner}:reasoning`, owner, "reasoning").text += delta.reasoning_content;
        if (typeof delta.content === "string") slot(`${owner}:text`, owner, "text").text += delta.content;
        if (Array.isArray(delta.content)) msg.blocks.push(...contentBlocks(delta.content));
        for (const rawCall of Array.isArray(delta.tool_calls) ? delta.tool_calls : []) {
          const call = record(rawCall);
          if (!call) { msg.blocks.push({ type: "data", title: "Malformed tool call", text: printable(rawCall) }); continue; }
          const fn = record(call.function);
          const block = slot(`${owner}:call:${call?.index ?? call?.id ?? 0}`, owner, "tool-call");
          block.callId = string(call?.id) ?? block.callId;
          if (typeof fn?.name === "string") block.title = (block.title ?? "") + fn.name;
          if (typeof fn?.arguments === "string") block.text += fn.arguments;
        }
      }
      continue;
    }
    const kind = String(event.type ?? wrapper?.event ?? "");
    const owner = "anthropic";
    const key = `${owner}:${event.index ?? 0}`;
    if (kind === "content_block_start") {
      const parsed = part(event.content_block)[0];
      if (parsed) { const block = slot(key, owner, parsed.type); Object.assign(block, parsed); if (parsed.type === "tool-call" && block.text === "{}") block.text = ""; }
      continue;
    }
    if (kind === "content_block_delta") {
      const delta = record(event.delta);
      const type = delta?.type === "input_json_delta" ? "tool-call" : delta?.type === "thinking_delta" ? "reasoning" : "text";
      if (delta?.text !== undefined || delta?.thinking !== undefined || delta?.partial_json !== undefined) slot(key, owner, type).text += printable(delta.text ?? delta.thinking ?? delta.partial_json);
      else if (delta?.type !== "signature_delta") unknown.push(message(event, "capture"));
      continue;
    }
    if (kind === "response.output_item.added") {
      const item = record(event.item); const outputOwner = `output:${event.output_index ?? item?.id ?? 0}`;
      if (item?.type === "message") getMessage(outputOwner).role = string(item.role) ?? "assistant";
      else { const parsed = part(item)[0]; if (parsed) Object.assign(slot(outputOwner, outputOwner, parsed.type), parsed); }
      continue;
    }
    if (kind === "response.content_part.added") {
      const outputOwner = `output:${event.output_index ?? 0}`; const parsed = part(event.part)[0];
      if (parsed) Object.assign(slot(`${outputOwner}:content:${event.content_index ?? 0}`, outputOwner, parsed.type), parsed);
      continue;
    }
    if (["response.output_text.delta", "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.function_call_arguments.delta"].includes(kind)) {
      const outputOwner = `output:${event.output_index ?? 0}`;
      const type = kind.includes("arguments") ? "tool-call" : kind.includes("reasoning") ? "reasoning" : "text";
      slot(type === "text" ? `${outputOwner}:content:${event.content_index ?? 0}` : outputOwner, outputOwner, type).text += printable(event.delta);
      continue;
    }
    if (kind === "error" || kind === "response.failed") { getMessage("error").blocks.push({ type: "error", text: printable(event.error ?? event.response ?? event) }); continue; }
    if (["message_start", "message_delta", "message_stop", "content_block_stop", "ping", "response.created", "response.in_progress", "response.completed", "response.output_item.done", "response.content_part.done", "response.output_text.done", "response.function_call_arguments.done", "response.reasoning_summary_text.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done"].includes(kind) || event.usage) continue;
    unknown.push(message(event, "capture"));
  }
  const result = unknown.filter((msg) => msg.blocks.length);
  if (!result.some((msg) => msg.blocks.some((block) => block.type === "text")) && typeof value.content === "string" && value.content) result.push({ role: "assistant", blocks: [{ type: "text", text: value.content }] });
  if (!result.length && value.content && typeof value.content !== "string") result.push(...responsePayload(value.content));
  return result;
}
function responsePayload(payload: unknown): Message[] {
  const root = record(payload);
  if (root?.message) return [message(root.message, "assistant"), ...(Array.isArray(root.tool_results) ? root.tool_results.map((result) => message({ role: "tool", ...record(result), content: record(result)?.content ?? record(result)?.output ?? result })) : [])];
  if (Array.isArray(root?.choices)) return root.choices.map((choice) => message(record(choice)?.message ?? record(choice)?.delta ?? choice, "assistant"));
  if (Array.isArray(root?.output)) return root.output.map((entry) => message(entry, "assistant"));
  const direct = messagesFrom(payload);
  if (direct.length) return direct;
  if (typeof payload === "string") return [{ role: "assistant", blocks: [{ type: "text", text: payload }] }];
  if (root?.content !== undefined || root?.parts !== undefined || root?.text !== undefined || root?.error || root?.type) return [message(root, "assistant")];
  return [{ role: "capture", blocks: [{ type: "data", title: "Unsupported response", text: printable(payload) }] }];
}
const evidenceCache = new WeakMap<LogEnvelope, Partial<Record<"request" | "response", StructuredEvidence>>>();
export function structuredEvidence(envelope: LogEnvelope, side: "request" | "response"): StructuredEvidence {
  const cached = evidenceCache.get(envelope)?.[side];
  if (cached) return cached;
  const response = record(envelope.response);
  let messages = side === "request" ? messagesFrom(envelope.request) : response?.format === "reconstructed_sse" ? streamedResponse(envelope.response as Extract<LogEnvelope["response"], { format: "reconstructed_sse" }>) : responsePayload(response?.body ?? response?.content ?? envelope.response);
  if (!messages.length) messages = [{ role: "capture", blocks: [{ type: "data", title: `Unsupported ${side}`, text: printable(side === "request" ? envelope.request : envelope.response) }] }];
  if (side === "response" && Array.isArray(envelope.tool_activity)) for (const [index, rawActivity] of envelope.tool_activity.entries()) {
    const activity = record(rawActivity);
    if (!activity || typeof activity.name !== "string") { messages.push({ role: "capture", blocks: [{ type: "data", title: "Malformed tool activity", text: printable(rawActivity) }] }); continue; }
    const callId = `activity:${envelope.exchange_id}:${index}`;
    messages.push({ role: "tool", blocks: [{ type: "tool-call", callId, title: activity.name, text: printable(activity.input) }, { type: "tool-result", callId, title: activity.name, failed: activity.status === "failed", outputCaptured: activity.output !== undefined, text: activity.output === undefined ? "No tool output was captured." : printable(activity.output) }] });
  }
  const recognized = messages.some((msg) => msg.blocks.some((block) => block.type !== "data"));
  const evidence = { recognized, messages: messages.map((msg, index) => {
    const id = `${envelope.exchange_id}:${side}:${index}${msg.nativeId ? `:${msg.nativeId}` : ""}`;
    return { id, role: msg.role, name: msg.name, blocks: msg.blocks.map((block, blockIndex) => ({ ...block, exchangeId: envelope.exchange_id, side, id: `${id}:${blockIndex}` })) };
  }) };
  const sides = evidenceCache.get(envelope) ?? {};
  sides[side] = evidence;
  evidenceCache.set(envelope, sides);
  return evidence;
}
