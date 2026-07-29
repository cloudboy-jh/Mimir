import { finiteNumber, parseJSON, readConfig, stringArray } from "./config";
import { reportSessionEvent } from "./session-events";
import { resolveSession, ulid } from "./sessions";
import type { Bindings } from "./types";

export const MAX_RESPONSE_BYTES = 20 * 1024 * 1024;

export type RequestKind = "primary" | "title" | "summary" | "compaction";

type CaptureInput = {
  request: Record<string, unknown>;
  archiveBody: ReadableStream<Uint8Array>;
  endpoint: string;
  model: string;
  repo: string | null;
  harness: string | null;
  accessTokenLabel: string;
  declaredSession: string | null;
  requestKind: RequestKind;
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
  const requestKind = classifyRequestKind(input.requestKind, redactedRequest);
  const intentCandidate = requestKind === "primary" ? deriveIntent(redactedRequest) : null;
  const requestExcerpt = excerpt(JSON.stringify(redactedRequest));
  const acceptedAt = new Date().toISOString();
  const r2Key = `log/${acceptedAt.slice(0, 10).replaceAll("-", "/")}/${id}.json`;
  const accepted = await acceptExchange(env.DB, input, id, session.id, activityAt, acceptedAt, r2Key, requestExcerpt, requestKind, intentCandidate);
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
    metadata: { repo: input.repo, harness: input.harness, git_ref: input.sourceRef, model: input.model, provider: prepared.provider, finish_reason: prepared.finishReason, request_kind: requestKind },
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
    return;
  }

  const provider = prepared.provider ? ` · ${prepared.provider}` : "";
  console.log(`saved exchange ${id} to session ${session.id} · ${input.model}${provider} · ${prepared.usage.prompt_tokens} in / ${prepared.usage.completion_tokens} out · ${latency}ms`);
  await reportSessionEvent(env, {
    version: 1,
    kind: "turn",
    session_id: session.id,
    harness: input.harness,
    repo: input.repo,
    ts: new Date().toISOString(),
    turn: {
      exchange_id: id,
      model: input.model,
      provider: prepared.provider,
      request_kind: requestKind,
      usage: { input_tokens: prepared.usage.prompt_tokens, output_tokens: prepared.usage.completion_tokens },
      latency_ms: latency,
      excerpt: requestExcerpt.slice(0, 500),
    },
  });
}

async function acceptExchange(db: D1Database, input: CaptureInput, id: string, sessionId: string, activityAt: string, acceptedAt: string, r2Key: string, requestExcerpt: string, requestKind: RequestKind, intentCandidate: string | null) {
  try {
    await db.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, response_excerpt, usage_json, latency_ms, repo, harness, r2_key, access_token_label, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, schema_version, request_kind, intent_candidate) VALUES (?, ?, ?, ?, ?, ?, '', '{}', 0, ?, ?, ?, ?, 0, 0, 'accepted', 'enabled', ?, 1, ?, ?)")
      .bind(id, sessionId, activityAt, input.endpoint, input.model, requestExcerpt, input.repo, input.harness, r2Key, input.accessTokenLabel, acceptedAt, requestKind, intentCandidate).run();
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
    db.prepare("UPDATE sessions SET ended_at = CASE WHEN ended_at IS NULL OR ended_at < ? THEN ? ELSE ended_at END, last_active_at = CASE WHEN last_active_at IS NULL OR last_active_at < ? THEN ? ELSE last_active_at END, harness = COALESCE(harness, ?), state = CASE WHEN ? AND (inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at OR inactive_at < ?) AND (boundary = 'header' OR NOT EXISTS (SELECT 1 FROM sessions active WHERE active.id <> sessions.id AND active.boundary = 'heuristic' AND active.state = 'active' AND active.repo IS sessions.repo AND active.harness IS sessions.harness)) THEN 'active' ELSE state END, inactive_at = CASE WHEN ? AND (inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at OR inactive_at < ?) AND (boundary = 'header' OR NOT EXISTS (SELECT 1 FROM sessions active WHERE active.id <> sessions.id AND active.boundary = 'heuristic' AND active.state = 'active' AND active.repo IS sessions.repo AND active.harness IS sessions.harness)) THEN NULL ELSE inactive_at END, model_primary = COALESCE(model_primary, ?), request_count = request_count + 1, tokens_in = tokens_in + ?, tokens_out = tokens_out + ? WHERE id = ? AND EXISTS (SELECT 1 FROM exchanges WHERE id = ? AND capture_status = 'accepted')").bind(activityAt, activityAt, activityAt, activityAt, harness, reactivate ? 1 : 0, activityAt, reactivate ? 1 : 0, activityAt, model, inputTokens, outputTokens, sessionId, exchangeId),
    db.prepare("INSERT OR IGNORE INTO session_files(session_id, file) SELECT session_id, file FROM exchange_files WHERE exchange_id = ? AND EXISTS (SELECT 1 FROM exchanges WHERE id = ? AND capture_status = 'accepted')").bind(exchangeId, exchangeId),
    db.prepare("INSERT OR IGNORE INTO session_errors(session_id, signature) SELECT session_id, signature FROM exchange_errors WHERE exchange_id = ? AND EXISTS (SELECT 1 FROM exchanges WHERE id = ? AND capture_status = 'accepted')").bind(exchangeId, exchangeId),
    db.prepare("UPDATE exchanges SET capture_status = 'saved', capture_reason = 'enabled', saved_at = ?, failed_at = NULL, failure_code = NULL, r2_bytes = ? WHERE id = ? AND capture_status = 'accepted'").bind(savedAt, r2Bytes, exchangeId),
    db.prepare("UPDATE sessions SET intent = CASE WHEN intent IS NULL OR EXISTS (SELECT 1 FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'primary' AND intent_candidate = sessions.intent) THEN COALESCE((SELECT intent_candidate FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'primary' AND intent_candidate IS NOT NULL ORDER BY ts ASC, id ASC LIMIT 1), intent) ELSE intent END WHERE id = ?").bind(sessionId, sessionId, sessionId),
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

const MAX_FILES = 40;
const MAX_ERRORS = 10;
const MAX_WALK_DEPTH = 12;
const TRAILING_MESSAGES = 3;
const FILE_KEYS = new Set(["path", "file", "filepath", "file_path", "filename", "file_name", "notebook_path", "target_file", "abs_path", "absolute_path", "new_path", "old_path"]);
const DEPENDENCY_PATH = /(?:^|\/)(?:node_modules|\.git|dist|build|out|vendor|\.venv|venv|__pycache__|\.next|\.nuxt|coverage|target)\//i;
// Diagnostics are line-anchored and require punctuation a compiler or runtime
// emits, so source code that merely contains the word "error" cannot match.
const DIAGNOSTICS = [
  /^[A-Z][A-Za-z.]*(?:Error|Exception)\b:[^\n]{1,180}/m,
  /^Traceback \(most recent call last\)/m,
  /^panic: [^\n]{1,180}/m,
  /^error(?:\[[A-Z0-9]+\])?: [^\n]{1,180}/m,
  /^[^\n]{0,90}error TS\d+: [^\n]{1,180}/m,
  /^\s*at [^\n]{1,120}:\d+:\d+/m,
];

// deriveSessionFields projects searchable facets from one exchange. Files come
// only from tool activity and errors only from explicit failure signals, so the
// facets describe what the agent did rather than what its prompt contained.
export function deriveSessionFields(request: unknown, response?: unknown) {
  return { files: unique(deriveFiles(request, response), MAX_FILES), errors: unique(deriveErrors(request, response), MAX_ERRORS) };
}

function deriveFiles(request: unknown, response: unknown): string[] {
  const files: string[] = [];
  for (const input of toolInputs(request).concat(toolInputs(response))) {
    for (const candidate of filePaths(input, 0)) {
      const normalized = normalizeFilePath(candidate);
      if (normalized) files.push(normalized);
    }
  }
  return files;
}

// toolInputs collects the argument objects of tool calls in either the OpenAI
// (function.arguments JSON string) or Anthropic (tool_use.input object) shape.
function toolInputs(value: unknown, depth = 0): unknown[] {
  if (depth > MAX_WALK_DEPTH || !value || typeof value !== "object") return [];
  if (Array.isArray(value)) return value.flatMap((item) => toolInputs(item, depth + 1));
  const record = value as Record<string, unknown>;
  const found: unknown[] = [];
  if (record.type === "tool_use" && record.input && typeof record.input === "object") found.push(record.input);
  const fn = record.function && typeof record.function === "object" ? record.function as Record<string, unknown> : null;
  if (fn && typeof fn.arguments === "string") {
    const parsed = parseJSON(fn.arguments);
    if (parsed && typeof parsed === "object") found.push(parsed);
  }
  for (const nested of Object.values(record)) found.push(...toolInputs(nested, depth + 1));
  return found;
}

function filePaths(value: unknown, depth: number): string[] {
  if (depth > MAX_WALK_DEPTH || !value || typeof value !== "object") return [];
  if (Array.isArray(value)) return value.flatMap((item) => filePaths(item, depth + 1));
  const paths: string[] = [];
  for (const [key, nested] of Object.entries(value as Record<string, unknown>)) {
    if (typeof nested === "string") {
      if (FILE_KEYS.has(key.toLowerCase())) paths.push(nested);
      continue;
    }
    paths.push(...filePaths(nested, depth + 1));
  }
  return paths;
}

function normalizeFilePath(raw: string): string | null {
  const value = raw.trim().replace(/\\/g, "/").replace(/^\.\//, "");
  if (!value || value.length > 240) return null;
  if ((value.match(/ /g)?.length ?? 0) > 3) return null;
  if (!value.includes("/") && !/\.[A-Za-z0-9]{1,10}$/.test(value)) return null;
  if (DEPENDENCY_PATH.test(value)) return null;
  return value;
}

function deriveErrors(request: unknown, response: unknown): string[] {
  const errors: string[] = [];
  pushEnvelopeError(response, errors);
  const record = response && typeof response === "object" ? response as Record<string, unknown> : {};
  if (Array.isArray(record.events)) {
    for (const event of record.events) pushEnvelopeError(event, errors);
  }
  for (const message of trailingMessages(request)) pushToolFailure(message, errors);
  return errors;
}

// pushEnvelopeError reads provider failure envelopes, which are the errors the
// proxy is actually positioned to observe.
function pushEnvelopeError(value: unknown, errors: string[]) {
  if (!value || typeof value !== "object") return;
  const record = value as Record<string, unknown>;
  const error = record.error;
  if (typeof error === "string") {
    pushSignature(error, errors);
    return;
  }
  if (!error || typeof error !== "object") return;
  const detail = error as Record<string, unknown>;
  const message = [detail.message, detail.type, detail.code].find((part) => typeof part === "string" && part) as string | undefined;
  if (message) pushSignature(typeof detail.code === "string" && detail.code && detail.code !== message ? `${detail.code}: ${message}` : message, errors);
}

// pushToolFailure only trusts explicitly flagged tool failures. An unflagged
// tool result is usually file content, and scanning it produced false errors.
function pushToolFailure(message: unknown, errors: string[]) {
  if (!message || typeof message !== "object") return;
  const record = message as Record<string, unknown>;
  const blocks = Array.isArray(record.content) ? record.content : [];
  for (const block of blocks) {
    if (!block || typeof block !== "object") continue;
    const detail = block as Record<string, unknown>;
    if (detail.type !== "tool_result" || detail.is_error !== true) continue;
    pushSignature(firstDiagnostic(messageText(detail.content) || (typeof detail.content === "string" ? detail.content : "")), errors);
  }
  if (record.role !== "tool") return;
  const flagged = record.is_error === true || (typeof record.exit_code === "number" && record.exit_code !== 0);
  if (flagged) pushSignature(firstDiagnostic(messageText(record.content)), errors);
}

function firstDiagnostic(text: string): string {
  if (!text) return "";
  for (const pattern of DIAGNOSTICS) {
    const match = pattern.exec(text);
    if (match) return match[0];
  }
  return text.split("\n").find((line) => line.trim()) ?? "";
}

function pushSignature(raw: string, errors: string[]) {
  const signature = raw.replace(/\s+/g, " ").trim().slice(0, 200);
  if (signature && /[A-Za-z]/.test(signature)) errors.push(signature);
}

// trailingMessages limits error detection to the newest turn so a failure is
// not re-counted against every later exchange that replays the transcript.
function trailingMessages(request: unknown): unknown[] {
  const messages = request && typeof request === "object" ? (request as Record<string, unknown>).messages : null;
  return Array.isArray(messages) ? messages.slice(-TRAILING_MESSAGES) : [];
}

// deriveIntent summarizes the session's purpose from the first user message
// of the redacted request. Only the first captured exchange wins; later
// exchanges cannot overwrite the session intent.
export function deriveIntent(request: unknown): string | null {
  const messages = typeof request === "object" && request ? (request as Record<string, unknown>).messages : null;
  if (!Array.isArray(messages)) return null;
  for (const message of messages) {
    const record = typeof message === "object" && message ? message as Record<string, unknown> : {};
    if (record.role !== "user") continue;
    const collapsed = messageText(record.content).replace(/\s+/g, " ").trim();
    if (collapsed) return collapsed.slice(0, 200);
  }
  return null;
}

export function classifyRequestKind(declared: RequestKind, request: unknown): RequestKind {
  if (declared !== "primary") return declared;
  const messages = typeof request === "object" && request ? (request as Record<string, unknown>).messages : null;
  if (!Array.isArray(messages)) return declared;
  for (const message of messages) {
    const record = typeof message === "object" && message ? message as Record<string, unknown> : {};
    if (record.role !== "system" && record.role !== "developer") continue;
    const content = messageText(record.content).toLowerCase();
    if (content.includes("you are a title generator") || (content.includes("generate a brief title") && content.includes("output only"))) return "title";
  }
  return declared;
}

function messageText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content.map((part) => {
    const record = typeof part === "object" && part ? part as Record<string, unknown> : {};
    return typeof record.text === "string" ? record.text : "";
  }).join(" ");
}

export function extractProvider(response: unknown) {
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

export function extractFinishReason(response: unknown) {
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

export function excerpt(value: string) {
  return value.slice(0, 8_000);
}

function unique(values: string[], limit: number) {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))].slice(0, limit);
}
