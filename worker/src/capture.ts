import { finiteNumber, parseJSON, readConfig, stringArray } from "./config";
import { resolveSession, ulid } from "./sessions";
import type { Bindings } from "./types";

export const MAX_RESPONSE_BYTES = 20 * 1024 * 1024;

type CaptureInput = {
  request: Record<string, unknown>;
  archiveBody: ReadableStream<Uint8Array>;
  endpoint: string;
  model: string;
  repo: string | null;
  harness: string | null;
  accessTokenLabel: string;
  declaredSession: string | null;
  sourceRef: string | null;
  responseType: string;
  started: number;
};

export async function capture(env: Bindings, input: CaptureInput) {
  const responseText = await readBoundedText(input.archiveBody, MAX_RESPONSE_BYTES);
  const response = parseCapturedResponse(responseText, input.responseType);
  const config = await readConfig(env.DB);
  const patterns = stringArray(config["redact.patterns"]);
  const id = ulid();
  const now = new Date().toISOString();
  const r2Key = `log/${now.slice(0, 10).replaceAll("-", "/")}/${id}.json`;
  const redactedRequest = redact(input.request, patterns);
  const redactedResponse = redact(response, patterns);
  const session = await resolveSession(env.DB, input.declaredSession, input.repo, input.harness, input.sourceRef, input.model, now);
  const usage = extractUsage(response);
  const provider = extractProvider(response);
  const finishReason = extractFinishReason(response);
  const derived = deriveSessionFields(redactedRequest, redactedResponse);
  const latency = Date.now() - input.started;
  const log = { id, ts: now, session: input.declaredSession, model: input.model, provider, finish_reason: finishReason, endpoint: input.endpoint, request: redactedRequest, response: redactedResponse, usage, latency_ms: latency, meta: { repo: input.repo, harness: input.harness } };
  await env.LOGS.put(r2Key, JSON.stringify(log), { httpMetadata: { contentType: "application/json" } });
  const requestExcerpt = excerpt(JSON.stringify(redactedRequest));
  const responseExcerpt = excerpt(JSON.stringify(redactedResponse));
  await env.DB.batch([
    env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, response_excerpt, usage_json, latency_ms, repo, harness, r2_key, provider, finish_reason, access_token_label, input_tokens, output_tokens) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)").bind(id, session.id, now, input.endpoint, input.model, requestExcerpt, responseExcerpt, JSON.stringify(usage), latency, input.repo, input.harness, r2Key, provider, finishReason, input.accessTokenLabel, usage.prompt_tokens, usage.completion_tokens),
    env.DB.prepare("UPDATE sessions SET ended_at = CASE WHEN ended_at IS NULL OR ended_at < ? THEN ? ELSE ended_at END, last_active_at = CASE WHEN last_active_at IS NULL OR last_active_at < ? THEN ? ELSE last_active_at END, harness = COALESCE(harness, ?), state = 'active', inactive_at = NULL, model_primary = COALESCE(model_primary, ?), request_count = request_count + 1, tokens_in = tokens_in + ?, tokens_out = tokens_out + ? WHERE id = ?").bind(now, now, now, now, input.harness, input.model, usage.prompt_tokens, usage.completion_tokens, session.id),
    ...derived.files.map((file) => env.DB.prepare("INSERT OR IGNORE INTO session_files(session_id, file) VALUES (?, ?)").bind(session.id, file)),
    ...derived.errors.map((signature) => env.DB.prepare("INSERT OR IGNORE INTO session_errors(session_id, signature) VALUES (?, ?)").bind(session.id, signature)),
  ]);
}

export async function readBoundedText(stream: ReadableStream<Uint8Array> | null, limit: number) {
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

export function parseCapturedResponse(text: string, contentType: string): unknown {
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
      const delta = typeof choice === "object" && choice ? (choice as Record<string, unknown>).delta : null;
      if (typeof delta === "object" && delta && typeof (delta as Record<string, unknown>).content === "string") content += (delta as Record<string, unknown>).content;
    }
    const delta = typeof record.delta === "object" && record.delta ? record.delta as Record<string, unknown> : {};
    if (typeof delta.text === "string") content += delta.text;
  }
  return { stream: true, content, events };
}

export function extractUsage(response: unknown) {
  const records = typeof response === "object" && response ? response as Record<string, unknown> : {};
  const events = Array.isArray(records.events) ? records.events : [response];
  let promptTokens = 0;
  let completionTokens = 0;
  for (const event of events) {
    const record = typeof event === "object" && event ? event as Record<string, unknown> : {};
    const message = typeof record.message === "object" && record.message ? record.message as Record<string, unknown> : {};
    const usage = typeof record.usage === "object" && record.usage ? record.usage as Record<string, unknown> : typeof message.usage === "object" && message.usage ? message.usage as Record<string, unknown> : {};
    promptTokens = Math.max(promptTokens, finiteNumber(usage.prompt_tokens ?? usage.input_tokens, 0));
    completionTokens = Math.max(completionTokens, finiteNumber(usage.completion_tokens ?? usage.output_tokens, 0));
  }
  return { prompt_tokens: promptTokens, completion_tokens: completionTokens };
}

export function redact(value: unknown, patterns: string[]): unknown {
  let text = JSON.stringify(value)
    .replace(/(?:sk|pk|rk)_[A-Za-z0-9_-]{16,}/g, "[REDACTED]")
    .replace(/(?:Bearer\s+)[A-Za-z0-9._-]+/gi, "$1[REDACTED]")
    .replace(/((?:api[_-]?key|token|secret|password)["']?\s*[:=]\s*["']?)[^\s,"'}]+/gi, "$1[REDACTED]");
  for (const pattern of patterns) {
    if (pattern === "builtin") continue;
    try {
      text = text.replace(new RegExp(pattern, "g"), "[REDACTED]");
    } catch {
      // Invalid patterns are inert rather than blocking the proxy.
    }
  }
  return parseJSON(text);
}

export function deriveSessionFields(...values: unknown[]) {
  const text = values.map((value) => JSON.stringify(value)).join("\n");
  const files = text.match(/(?:[A-Za-z0-9_.-]+\/)*[A-Za-z0-9_.-]+\.(?:tsx|ts|jsx|js|cpp|hpp|json|yaml|yml|sql|java|go|py|rs|cs|md|c|h)(?![A-Za-z0-9_.-])/g) ?? [];
  const errors = text.match(/(?:error|exception|panic|failed)[:\s][^\n"}]{1,160}/gi) ?? [];
  return { files: unique(files, 100), errors: unique(errors, 20) };
}

function extractProvider(response: unknown) {
  const records = typeof response === "object" && response ? response as Record<string, unknown> : {};
  const events = Array.isArray(records.events) ? records.events : [response];
  for (const event of events) {
    const record = typeof event === "object" && event ? event as Record<string, unknown> : {};
    const provider = record.provider;
    if (typeof provider === "string") return provider;
    if (typeof provider === "object" && provider && typeof (provider as Record<string, unknown>).name === "string") return (provider as Record<string, unknown>).name as string;
  }
  return null;
}

function extractFinishReason(response: unknown) {
  const records = typeof response === "object" && response ? response as Record<string, unknown> : {};
  const events = Array.isArray(records.events) ? records.events : [response];
  for (const event of [...events].reverse()) {
    const record = typeof event === "object" && event ? event as Record<string, unknown> : {};
    const choices = Array.isArray(record.choices) ? record.choices : [];
    for (const choice of choices) if (typeof choice === "object" && choice && typeof (choice as Record<string, unknown>).finish_reason === "string") return (choice as Record<string, unknown>).finish_reason as string;
    if (typeof record.stop_reason === "string") return record.stop_reason;
  }
  return null;
}

function excerpt(value: string) {
  return value.slice(0, 8_000);
}

function unique(values: string[], limit: number) {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))].slice(0, limit);
}
