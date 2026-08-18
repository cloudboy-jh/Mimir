import { finiteNumber, parseJSON } from "../config/config-store";
export async function readBoundedText(
  stream: ReadableStream<Uint8Array> | null,
  limit: number,
) {
  if (!stream) return "";
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let size = 0;
  let text = "";
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.byteLength;
      if (size > limit) throw new Error("capture limit exceeded");
      text += decoder.decode(value, { stream: true });
    }
    return text + decoder.decode();
  } catch (error) {
    await reader.cancel(error).catch(() => undefined);
    throw error;
  } finally {
    reader.releaseLock();
  }
}

export function parseCapturedResponse(
  text: string,
  contentType: string,
): unknown {
  if (!contentType.includes("text/event-stream")) return parseJSON(text);
  const events: unknown[] = [];
  let content = "";
  for (const line of text.split("\n")) {
    if (!line.startsWith("data:")) continue;
    const data = line.slice(5).trim();
    if (!data || data === "[DONE]") continue;
    const event = parseJSON(data);
    events.push(event);
    if (typeof event !== "object" || !event) continue;
    const record = event as Record<string, unknown>;
    const choices = Array.isArray(record.choices) ? record.choices : [];
    for (const choice of choices) {
      const delta =
        typeof choice === "object" && choice
          ? (choice as Record<string, unknown>).delta
          : null;
      if (
        typeof delta === "object" &&
        delta &&
        typeof (delta as Record<string, unknown>).content === "string"
      )
        content += (delta as Record<string, unknown>).content;
    }
    const delta =
      typeof record.delta === "object" && record.delta
        ? (record.delta as Record<string, unknown>)
        : {};
    if (typeof delta.text === "string") content += delta.text;
  }
  return { stream: true, content, events };
}

export function extractUsage(response: unknown) {
  const records =
    typeof response === "object" && response
      ? (response as Record<string, unknown>)
      : {};
  const events = Array.isArray(records.events) ? records.events : [response];
  let promptTokens = 0;
  let completionTokens = 0;
  for (const event of events) {
    const record =
      typeof event === "object" && event
        ? (event as Record<string, unknown>)
        : {};
    const message =
      typeof record.message === "object" && record.message
        ? (record.message as Record<string, unknown>)
        : {};
    const usage =
      typeof record.usage === "object" && record.usage
        ? (record.usage as Record<string, unknown>)
        : typeof message.usage === "object" && message.usage
          ? (message.usage as Record<string, unknown>)
          : {};
    promptTokens = Math.max(
      promptTokens,
      finiteNumber(usage.prompt_tokens ?? usage.input_tokens, 0),
    );
    completionTokens = Math.max(
      completionTokens,
      finiteNumber(usage.completion_tokens ?? usage.output_tokens, 0),
    );
  }
  return { prompt_tokens: promptTokens, completion_tokens: completionTokens };
}
