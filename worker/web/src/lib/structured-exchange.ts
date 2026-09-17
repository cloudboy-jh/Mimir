import type { LogEnvelope } from "./api";

export type StructuredBlock = {
  type: "text" | "tool-call" | "tool-result" | "data" | "error";
  title?: string;
  text: string;
};

export type StructuredMessage = {
  role: string;
  name?: string;
  blocks: StructuredBlock[];
};

export type StructuredEvidence = {
  recognized: boolean;
  messages: StructuredMessage[];
};

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function printable(value: unknown): string {
  if (typeof value === "string") return value;
  if (value === undefined) return "";
  try { return JSON.stringify(value, null, 2); } catch { return String(value); }
}

function contentBlocks(content: unknown): StructuredBlock[] {
  if (typeof content === "string") return content ? [{ type: "text", text: content }] : [];
  if (!Array.isArray(content)) return content === undefined || content === null ? [] : [{ type: "data", text: printable(content) }];
  return content.flatMap((part): StructuredBlock[] => {
    if (typeof part === "string") return [{ type: "text", text: part }];
    const item = record(part);
    if (!item) return [{ type: "data", text: printable(part) }];
    const kind = String(item.type ?? "");
    if (kind === "text" || kind === "output_text" || kind === "input_text") return [{ type: "text", text: printable(item.text ?? item.content) }];
    if (kind.includes("tool") || kind === "function_call") {
      return [{ type: kind.includes("result") ? "tool-result" : "tool-call", title: printable(item.name || item.tool_name || item.id), text: printable(item.arguments ?? item.input ?? item.content ?? item.output ?? item) }];
    }
    return [{ type: "data", title: kind || undefined, text: printable(item) }];
  });
}

function message(value: unknown): StructuredMessage | null {
  const item = record(value);
  if (!item) return null;
  const role = typeof item.role === "string" ? item.role : typeof item.type === "string" ? item.type : "message";
  const blocks = contentBlocks(item.content ?? item.text ?? item.output);
  const toolCalls = Array.isArray(item.tool_calls) ? item.tool_calls : [];
  for (const call of toolCalls) {
    const tool = record(call);
    const fn = record(tool?.function);
    blocks.push({ type: "tool-call", title: printable(fn?.name ?? tool?.name ?? tool?.id), text: printable(fn?.arguments ?? tool?.arguments ?? tool) });
  }
  if (typeof item.errorMessage === "string" && item.errorMessage) blocks.push({ type: "error", text: item.errorMessage });
  if (!blocks.length) blocks.push({ type: "data", text: printable(item) });
  return { role, name: typeof item.name === "string" ? item.name : undefined, blocks };
}

function messagesFrom(value: unknown): StructuredMessage[] {
  const root = record(value);
  const values = Array.isArray(root?.messages) ? root.messages
    : Array.isArray(root?.input) ? root.input
      : Array.isArray(value) ? value
        : [];
  return values.flatMap((item) => {
    const parsed = message(item);
    return parsed ? [parsed] : [];
  });
}

function streamedResponse(value: Extract<LogEnvelope["response"], { format: "reconstructed_sse" }>): StructuredMessage[] {
  const events = Array.isArray(value.events) ? value.events : [];
  const tools = new Map<string, { name: string; id: string; arguments: string; input?: unknown }>();
  let streamedText = "";
  let role = "assistant";
  for (const rawEvent of events) {
    const event = record(rawEvent);
    if (!event) continue;
    const choices = Array.isArray(event.choices) ? event.choices : [];
    for (const rawChoice of choices) {
      const choice = record(rawChoice);
      const delta = record(choice?.delta);
      if (!delta) continue;
      if (typeof delta.role === "string") role = delta.role;
      if (typeof delta.content === "string") streamedText += delta.content;
      const calls = Array.isArray(delta.tool_calls) ? delta.tool_calls : [];
      for (const rawCall of calls) {
        const call = record(rawCall);
        if (!call) continue;
        const fn = record(call.function);
        const key = typeof call.index === "number" ? `openai:${call.index}` : `openai:${printable(call.id)}`;
        const current = tools.get(key) ?? { name: "", id: "", arguments: "" };
        if (typeof call.id === "string") current.id = call.id;
        if (typeof fn?.name === "string") current.name += fn.name;
        if (typeof fn?.arguments === "string") current.arguments += fn.arguments;
        tools.set(key, current);
      }
    }
    const index = typeof event.index === "number" ? event.index : 0;
    const contentBlock = record(event.content_block);
    if (event.type === "content_block_start" && contentBlock?.type === "tool_use") {
      tools.set(`anthropic:${index}`, {
        name: typeof contentBlock.name === "string" ? contentBlock.name : "",
        id: typeof contentBlock.id === "string" ? contentBlock.id : "",
        arguments: "",
        input: contentBlock.input,
      });
    }
    const delta = record(event.delta);
    if (typeof delta?.text === "string") streamedText += delta.text;
    if (delta?.type === "input_json_delta" && typeof delta.partial_json === "string") {
      const key = `anthropic:${index}`;
      const current = tools.get(key) ?? { name: "", id: "", arguments: "" };
      current.arguments += delta.partial_json;
      tools.set(key, current);
    }
  }
  const text = typeof value.content === "string" && value.content ? value.content : streamedText;
  const blocks: StructuredBlock[] = text ? [{ type: "text", text }] : [];
  for (const tool of tools.values()) {
    blocks.push({
      type: "tool-call",
      title: tool.name || tool.id || undefined,
      text: tool.arguments || printable(tool.input ?? {}),
    });
  }
  return blocks.length ? [{ role, blocks }] : [];
}

function responseMessages(value: LogEnvelope["response"]): StructuredMessage[] {
  if (value.format === "reconstructed_sse") {
    const streamed = streamedResponse(value);
    if (streamed.length) return streamed;
  }
  const payload = value.format === "json" ? value.body : value.content;
  const root = record(payload);
  const direct = message(root?.message);
  if (direct) {
    const results = Array.isArray(root?.tool_results) ? root.tool_results.flatMap((item) => {
      const parsed = message({ role: "tool", content: item });
      return parsed ? [parsed] : [];
    }) : [];
    return [direct, ...results];
  }
  const rootMessage = message(payload);
  if (rootMessage && (root?.role || root?.content || root?.message)) return [rootMessage];
  const choice = Array.isArray(root?.choices) ? record(root.choices[0]) : null;
  const selected = message(choice?.message ?? choice?.delta);
  if (selected) return [selected];
  const output = Array.isArray(root?.output) ? messagesFrom(root.output) : [];
  if (output.length) return output;
  if (typeof payload === "string" && payload) return [{ role: "assistant", blocks: [{ type: "text", text: payload }] }];
  if (value.format === "reconstructed_sse" && typeof value.content === "string" && value.content) return [{ role: "assistant", blocks: [{ type: "text", text: value.content }] }];
  const reconstructed = record(value.format === "reconstructed_sse" ? value.content : null);
  const text = printable(reconstructed?.text ?? reconstructed?.content);
  return text ? [{ role: "assistant", blocks: [{ type: "text", text }] }] : [];
}

export function structuredEvidence(envelope: LogEnvelope, side: "request" | "response"): StructuredEvidence {
  const messages = side === "request" ? messagesFrom(envelope.request) : responseMessages(envelope.response);
  return { recognized: messages.length > 0, messages };
}
