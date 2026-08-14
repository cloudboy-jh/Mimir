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

function responseMessages(value: LogEnvelope["response"]): StructuredMessage[] {
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
