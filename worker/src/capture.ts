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

type PreparedCapture = {
  response: unknown;
  redactedResponse: unknown;
  usage: ReturnType<typeof extractUsage>;
  provider: string | null;
  finishReason: string | null;
  responseExcerpt: string;
  files: string[];
  errors: string[];
};

export async function capture(env: Bindings, input: CaptureInput): Promise<void> {
  const id = ulid();
  const activityAt = new Date(input.started).toISOString();
  const responseResultPromise = readBoundedText(input.archiveBody, MAX_RESPONSE_BYTES)
    .then((text) => ({ text, error: null as unknown }))
    .catch((error: unknown) => ({ text: "", error }));
  const [config, session] = await Promise.all([
    readConfig(env.DB),
    resolveSession(env.DB, input.declaredSession, input.repo, input.harness, input.sourceRef, input.model, activityAt),
  ]);
  const patterns = stringArray(config["redact.patterns"]);
  const redactedRequest = redact(input.request, patterns);
  const requestExcerpt = excerpt(JSON.stringify(redactedRequest));
  const acceptedAt = new Date().toISOString();
  const r2Key = `log/${acceptedAt.slice(0, 10).replaceAll("-", "/")}/${id}.json`;
  const accepted = await acceptExchange(env.DB, input, id, session.id, activityAt, acceptedAt, r2Key, requestExcerpt);
  if (!accepted) {
    await responseResultPromise;
    return;
  }

  const responseResult = await responseResultPromise;
  if (responseResult.error) {
    const tooLarge = responseResult.error instanceof Error && responseResult.error.message === "capture limit exceeded";
    const failureCode = tooLarge ? "response_too_large" : "response_read_failed";
    await failExchange(env.DB, id, failureCode, tooLarge ? "response_size_limit" : "response_read");
    logCaptureError(tooLarge ? "capture response exceeded size limit" : "capture response read failed", responseResult.error, id, session.id, failureCode);
    return;
  }

  const parsedResponse = parseCapturedResponse(responseResult.text, input.responseType);
  const redactedResponse = redact(parsedResponse, patterns);
  const derived = deriveSessionFields(redactedRequest, redactedResponse);
  const prepared: PreparedCapture = {
    response: parsedResponse,
    redactedResponse,
    usage: extractUsage(parsedResponse),
    provider: extractProvider(parsedResponse),
    finishReason: extractFinishReason(parsedResponse),
    responseExcerpt: excerpt(JSON.stringify(redactedResponse)),
    files: derived.files,
    errors: derived.errors,
  };
  const latency = Date.now() - input.started;
  if (!await prepareAcceptedExchange(env.DB, id, session.id, prepared, latency)) return;

  const reconstructed = prepared.redactedResponse as { content?: unknown; events?: unknown };
  const response = input.responseType.includes("text/event-stream")
    ? { format: "reconstructed_sse", content: reconstructed.content ?? "", events: reconstructed.events ?? [] }
    : { format: "json", body: prepared.redactedResponse };
  const envelope = {
    schema_version: 1,
    exchange_id: id,
    session_id: session.id,
    declared_session_id: input.declaredSession,
    captured_at: acceptedAt,
    endpoint: input.endpoint,
    request: redactedRequest,
    response,
    metadata: { repo: input.repo, harness: input.harness, git_ref: input.sourceRef, model: input.model, provider: prepared.provider, finish_reason: prepared.finishReason },
    usage: { input_tokens: prepared.usage.prompt_tokens, output_tokens: prepared.usage.completion_tokens },
    latency_ms: latency,
    redaction: { version: 1 },
  };
  const objectBody = JSON.stringify(envelope);
  const r2Bytes = new TextEncoder().encode(objectBody).byteLength;
  try {
    await env.LOGS.put(r2Key, objectBody, { httpMetadata: { contentType: "application/json" } });
  } catch (error) {
    await failExchange(env.DB, id, "r2_write_failed", "r2_write");
    logCaptureError("capture R2 write failed", error, id, session.id, "r2_write_failed");
    return;
  }

  try {
    await finalizeAcceptedExchange(env.DB, id, session.id, activityAt, new Date().toISOString(), input.harness, input.model, prepared.usage.prompt_tokens, prepared.usage.completion_tokens, r2Bytes, true);
  } catch (error) {
    logCaptureError("capture D1 finalization failed", error, id, session.id, "d1_finalize_failed");
  }
}

async function acceptExchange(db: D1Database, input: CaptureInput, id: string, sessionId: string, activityAt: string, acceptedAt: string, r2Key: string, requestExcerpt: string) {
  try {
    await db.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, response_excerpt, usage_json, latency_ms, repo, harness, r2_key, access_token_label, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, schema_version) VALUES (?, ?, ?, ?, ?, ?, '', '{}', 0, ?, ?, ?, ?, 0, 0, 'accepted', 'enabled', ?, 1)")
      .bind(id, sessionId, activityAt, input.endpoint, input.model, requestExcerpt, input.repo, input.harness, r2Key, input.accessTokenLabel, acceptedAt).run();
    return true;
  } catch (error) {
    logCaptureError("capture D1 acceptance failed", error, id, sessionId, "d1_accept_failed");
    return false;
  }
}

async function prepareAcceptedExchange(db: D1Database, exchangeId: string, sessionId: string, prepared: PreparedCapture, latency: number) {
  try {
    await db.batch([
      db.prepare("UPDATE exchanges SET response_excerpt = ?, usage_json = ?, latency_ms = ?, provider = ?, finish_reason = ?, input_tokens = ?, output_tokens = ? WHERE id = ? AND capture_status = 'accepted'")
        .bind(prepared.responseExcerpt, JSON.stringify(prepared.usage), latency, prepared.provider, prepared.finishReason, prepared.usage.prompt_tokens, prepared.usage.completion_tokens, exchangeId),
      ...prepared.files.map((file) => db.prepare("INSERT INTO exchange_files(exchange_id, session_id, file) VALUES (?, ?, ?)").bind(exchangeId, sessionId, file)),
      ...prepared.errors.map((signature) => db.prepare("INSERT INTO exchange_errors(exchange_id, session_id, signature) VALUES (?, ?, ?)").bind(exchangeId, sessionId, signature)),
    ]);
    return true;
  } catch (error) {
    await failExchange(db, exchangeId, "d1_prepare_failed", "d1_prepare");
    logCaptureError("capture D1 preparation failed", error, exchangeId, sessionId, "d1_prepare_failed");
    return false;
  }
}

export async function finalizeAcceptedExchange(db: D1Database, exchangeId: string, sessionId: string, activityAt: string, savedAt: string, harness: string | null, model: string, inputTokens: number, outputTokens: number, r2Bytes: number | null, reactivate: boolean) {
  await db.batch([
    db.prepare("UPDATE sessions SET ended_at = CASE WHEN ended_at IS NULL OR ended_at < ? THEN ? ELSE ended_at END, last_active_at = CASE WHEN last_active_at IS NULL OR last_active_at < ? THEN ? ELSE last_active_at END, harness = COALESCE(harness, ?), state = CASE WHEN ? AND (boundary = 'header' OR NOT EXISTS (SELECT 1 FROM sessions active WHERE active.id <> sessions.id AND active.boundary = 'heuristic' AND active.state = 'active' AND active.repo IS sessions.repo AND active.harness IS sessions.harness)) THEN 'active' ELSE state END, inactive_at = CASE WHEN ? AND (boundary = 'header' OR NOT EXISTS (SELECT 1 FROM sessions active WHERE active.id <> sessions.id AND active.boundary = 'heuristic' AND active.state = 'active' AND active.repo IS sessions.repo AND active.harness IS sessions.harness)) THEN NULL ELSE inactive_at END, model_primary = COALESCE(model_primary, ?), request_count = request_count + 1, tokens_in = tokens_in + ?, tokens_out = tokens_out + ? WHERE id = ? AND EXISTS (SELECT 1 FROM exchanges WHERE id = ? AND capture_status = 'accepted')").bind(activityAt, activityAt, activityAt, activityAt, harness, reactivate ? 1 : 0, reactivate ? 1 : 0, model, inputTokens, outputTokens, sessionId, exchangeId),
    db.prepare("INSERT OR IGNORE INTO session_files(session_id, file) SELECT session_id, file FROM exchange_files WHERE exchange_id = ? AND EXISTS (SELECT 1 FROM exchanges WHERE id = ? AND capture_status = 'accepted')").bind(exchangeId, exchangeId),
    db.prepare("INSERT OR IGNORE INTO session_errors(session_id, signature) SELECT session_id, signature FROM exchange_errors WHERE exchange_id = ? AND EXISTS (SELECT 1 FROM exchanges WHERE id = ? AND capture_status = 'accepted')").bind(exchangeId, exchangeId),
    db.prepare("UPDATE exchanges SET capture_status = 'saved', capture_reason = 'enabled', saved_at = ?, failed_at = NULL, failure_code = NULL, r2_bytes = ? WHERE id = ? AND capture_status = 'accepted'").bind(savedAt, r2Bytes, exchangeId),
  ]);
}

async function failExchange(db: D1Database, exchangeId: string, failureCode: string, reason: string) {
  try {
    await db.prepare("UPDATE exchanges SET capture_status = 'failed', capture_reason = ?, failed_at = ?, failure_code = ? WHERE id = ? AND capture_status = 'accepted'").bind(reason, new Date().toISOString(), failureCode, exchangeId).run();
  } catch (error) {
    logCaptureError("capture failure status update failed", error, exchangeId, null, "d1_failure_update_failed");
  }
}

function logCaptureError(message: string, error: unknown, exchangeId: string, sessionId: string | null, failureCode: string) {
  console.error(JSON.stringify({ message, error: error instanceof Error ? error.message : String(error), exchange_id: exchangeId, session_id: sessionId, failure_code: failureCode }));
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
