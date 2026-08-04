import type { Context } from "hono";
import { classifyRequestKind, deriveIntent, deriveSessionFields, excerpt, extractFinishReason, extractProvider, finalizeAcceptedExchange, readBoundedText, redact, type RequestKind } from "./capture";
import { decideCapture, readConfig, saveConfig, shouldSave, stringArray } from "./config";
import { reportSessionEvent, SESSION_ID } from "./session-events";
import { resolveSession } from "./sessions";
import { extractGeneratedTitle, normalizeSessionTitle } from "./session-titles";
import type { AppEnv } from "./types";

const MAX_BODY_BYTES = 20 * 1024 * 1024;
const MAX_REQUEST_BYTES = 10 * 1024 * 1024;
const MAX_JSON_DEPTH = 64;
const MAX_JSON_VALUES = 100_000;
const MAX_STRING_CHARS = 2 * 1024 * 1024;
const EXCHANGE_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
const REQUEST_KINDS = new Set<RequestKind>(["primary", "title", "summary", "compaction"]);
const PAYLOAD_FIELDS = new Set(["exchange_id", "ts", "model", "provider", "request", "response", "usage", "latency_ms", "request_kind", "title"]);
const USAGE_FIELDS = new Set(["input_tokens", "output_tokens"]);

type ReportedExchange = {
  exchange_id: string;
  ts: string;
  model: string;
  provider: string | null;
  request: unknown;
  response: unknown;
  usage: { input_tokens: number; output_tokens: number };
  latency_ms: number;
  request_kind: RequestKind;
  title: string | null;
};

export async function ingestReportedExchange(c: Context<AppEnv>) {
  const sessionId = c.req.param("id") ?? "";
  if (!SESSION_ID.test(sessionId)) return c.json({ error: "invalid session id" }, 400);

  const declaredLength = Number(c.req.header("content-length") ?? 0);
  if (Number.isFinite(declaredLength) && declaredLength > MAX_BODY_BYTES) return c.json({ error: "exchange body too large" }, 413);
  let bodyText: string;
  try {
    bodyText = await readBoundedText(c.req.raw.body, MAX_BODY_BYTES);
  } catch {
    return c.json({ error: "exchange body too large" }, 413);
  }

  let body: unknown;
  try {
    body = JSON.parse(bodyText);
  } catch {
    return c.json({ error: "invalid JSON body" }, 400);
  }
  const parsed = parseReportedExchange(body);
  if ("error" in parsed) return c.json({ error: parsed.error }, 400);

  const prior = await existingExchange(c.env.DB, parsed.exchange_id);
  if (prior?.capture_status === "saved") return duplicateResponse(c, prior, sessionId);
  if (prior && prior.session_id !== sessionId) return duplicateResponse(c, prior, sessionId);
  if (prior?.capture_status === "accepted") return c.json({ error: "exchange save in progress" }, 503);

  const repo = metadata(c.req.header("x-mimir-repo"));
  const harness = metadata(c.req.header("x-mimir-harness"));
  const sourceRef = metadata(c.req.header("x-mimir-git-ref"));
  const acceptedAt = new Date().toISOString();
  const config = await readConfig(c.env.DB);
  const policy = saveConfig(config);
  if (!shouldSave(policy, repo, parsed.model)) {
    const decision = decideCapture(policy, repo, parsed.model, true);
    if (decision.capture !== "skipped") throw new Error("inconsistent reported exchange capture decision");
    return c.json({ exchange_id: parsed.exchange_id, session_id: sessionId, capture_status: "skipped", capture_reason: decision.reason, duplicate: false });
  }
  if (prior) {
    await c.env.DB.batch([
      c.env.DB.prepare("DELETE FROM exchange_files WHERE exchange_id = ?").bind(parsed.exchange_id),
      c.env.DB.prepare("DELETE FROM exchange_errors WHERE exchange_id = ?").bind(parsed.exchange_id),
      c.env.DB.prepare("DELETE FROM exchanges WHERE id = ? AND session_id = ? AND capture_status = 'failed'").bind(parsed.exchange_id, sessionId),
    ]);
  }
  const session = await resolveSession(c.env.DB, sessionId, repo, harness, sourceRef, parsed.model, parsed.ts);
  const patterns = stringArray(config["redact.patterns"]);
  const request = redact(parsed.request, patterns);
  const response = redact(parsed.response, patterns);
  const requestKind = classifyRequestKind(parsed.request_kind, request);
  const intent = requestKind === "primary" ? deriveIntent(request) : null;
  const facets = deriveSessionFields(request, response);
  const provider = parsed.provider ?? extractProvider(response);
  const finishReason = extractFinishReason(response);
  const requestExcerpt = excerpt(JSON.stringify(request));
  const responseExcerpt = excerpt(JSON.stringify(response));
  const titleCandidate = requestKind === "title" ? extractGeneratedTitle(response) : null;
  const r2Key = `log/${acceptedAt.slice(0, 10).replaceAll("-", "/")}/${parsed.exchange_id}.json`;

  const inserted = await c.env.DB.prepare("INSERT OR IGNORE INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, response_excerpt, usage_json, latency_ms, repo, harness, r2_key, provider, finish_reason, access_token_label, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, schema_version, request_kind, intent_candidate, title_candidate) VALUES (?, ?, ?, 'harness', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'accepted', 'reported', ?, 1, ?, ?, ?)")
    .bind(parsed.exchange_id, session.id, parsed.ts, parsed.model, requestExcerpt, responseExcerpt, JSON.stringify(parsed.usage), parsed.latency_ms, repo, harness, r2Key, provider, finishReason, c.get("tokenLabel"), parsed.usage.input_tokens, parsed.usage.output_tokens, acceptedAt, requestKind, intent, titleCandidate).run();
  if (inserted.meta.changes === 0) {
    const raced = await existingExchange(c.env.DB, parsed.exchange_id);
    if (raced) return duplicateResponse(c, raced, sessionId);
    throw new Error("reported exchange insert was ignored without an existing row");
  }

  try {
    const facetStatements = [
      ...facets.files.map((file) => c.env.DB.prepare("INSERT INTO exchange_files(exchange_id, session_id, file) VALUES (?, ?, ?)").bind(parsed.exchange_id, sessionId, file)),
      ...facets.errors.map((signature) => c.env.DB.prepare("INSERT INTO exchange_errors(exchange_id, session_id, signature) VALUES (?, ?, ?)").bind(parsed.exchange_id, sessionId, signature)),
    ];
    if (facetStatements.length) await c.env.DB.batch(facetStatements);

    const envelope = {
      schema_version: 1,
      exchange_id: parsed.exchange_id,
      session_id: sessionId,
      declared_session_id: sessionId,
      captured_at: acceptedAt,
      endpoint: "harness",
      request,
      response: { format: "json", body: response },
      metadata: { repo, harness, git_ref: sourceRef, model: parsed.model, provider, finish_reason: finishReason, request_kind: requestKind },
      usage: parsed.usage,
      latency_ms: parsed.latency_ms,
      redaction: { version: 1 },
    };
    const objectBody = JSON.stringify(envelope);
    const r2Bytes = new TextEncoder().encode(objectBody).byteLength;
    await c.env.LOGS.put(r2Key, objectBody, { httpMetadata: { contentType: "application/json" } });
    await finalizeAcceptedExchange(c.env.DB, parsed.exchange_id, sessionId, parsed.ts, new Date().toISOString(), harness, parsed.model, parsed.usage.input_tokens, parsed.usage.output_tokens, r2Bytes, true, titleCandidate);
  } catch (error) {
    await c.env.DB.prepare("UPDATE exchanges SET capture_status = 'failed', capture_reason = 'reported', failed_at = ?, failure_code = ? WHERE id = ? AND capture_status = 'accepted'")
      .bind(new Date().toISOString(), "reported_save_failed", parsed.exchange_id).run();
    console.error(JSON.stringify({ message: "reported exchange save failed", exchange_id: parsed.exchange_id, session_id: sessionId, error: error instanceof Error ? error.message : String(error) }));
    return c.json({ error: "exchange save failed" }, 500);
  }

  await reportSessionEvent(c.env, {
    version: 1,
    kind: "turn",
    session_id: sessionId,
    harness,
    repo,
    ...(parsed.title ? { title: parsed.title } : {}),
    ts: parsed.ts,
    turn: {
      exchange_id: parsed.exchange_id,
      model: parsed.model,
      provider,
      request_kind: requestKind,
      usage: parsed.usage,
      latency_ms: parsed.latency_ms,
      excerpt: requestExcerpt.slice(0, 500),
    },
  });
  return c.json({ exchange_id: parsed.exchange_id, session_id: sessionId, capture_status: "saved", duplicate: false }, 201);
}

function parseReportedExchange(input: unknown): ReportedExchange | { error: string } {
  if (!input || typeof input !== "object" || Array.isArray(input)) return { error: "exchange must be an object" };
  const body = input as Record<string, unknown>;
  if (Object.keys(body).some((field) => !PAYLOAD_FIELDS.has(field))) return { error: "exchange contains unknown fields" };
  if (typeof body.exchange_id !== "string" || !EXCHANGE_ID.test(body.exchange_id)) return { error: "invalid exchange_id" };
  if (typeof body.ts !== "string" || Number.isNaN(Date.parse(body.ts))) return { error: "invalid ts" };
  if (typeof body.model !== "string" || body.model.length === 0 || body.model.length > 256) return { error: "invalid model" };
  if (body.provider !== undefined && body.provider !== null && (typeof body.provider !== "string" || body.provider.length === 0 || body.provider.length > 256)) return { error: "invalid provider" };
  if (!("request" in body) || !("response" in body)) return { error: "request and response are required" };
  const requestError = validateJSONValue(body.request, MAX_REQUEST_BYTES);
  if (requestError) return { error: `invalid request: ${requestError}` };
  const responseError = validateJSONValue(body.response, MAX_BODY_BYTES);
  if (responseError) return { error: `invalid response: ${responseError}` };
  if (!body.usage || typeof body.usage !== "object" || Array.isArray(body.usage)) return { error: "invalid usage" };
  const usage = body.usage as Record<string, unknown>;
  if (Object.keys(usage).some((field) => !USAGE_FIELDS.has(field)) || Object.keys(usage).length !== USAGE_FIELDS.size) return { error: "usage must contain input_tokens and output_tokens" };
  if (!boundedInteger(usage.input_tokens) || !boundedInteger(usage.output_tokens)) return { error: "invalid usage token counts" };
  if (!boundedInteger(body.latency_ms)) return { error: "invalid latency_ms" };
  if (!REQUEST_KINDS.has(body.request_kind as RequestKind)) return { error: "invalid request_kind" };
  if (body.title !== undefined && normalizeSessionTitle(body.title) === null) return { error: "invalid title" };
  return {
    exchange_id: body.exchange_id,
    ts: new Date(body.ts).toISOString(),
    model: body.model,
    provider: typeof body.provider === "string" ? body.provider : null,
    request: body.request,
    response: body.response,
    usage: { input_tokens: usage.input_tokens as number, output_tokens: usage.output_tokens as number },
    latency_ms: body.latency_ms as number,
    request_kind: body.request_kind as RequestKind,
    title: body.title === undefined ? null : normalizeSessionTitle(body.title),
  };
}

function validateJSONValue(root: unknown, byteLimit: number): string | null {
  let encoded: string;
  try {
    encoded = JSON.stringify(root);
  } catch {
    return "not JSON-safe";
  }
  if (encoded === undefined) return "not JSON-safe";
  if (new TextEncoder().encode(encoded).byteLength > byteLimit) return "too large";
  const stack: Array<{ value: unknown; depth: number }> = [{ value: root, depth: 0 }];
  let count = 0;
  while (stack.length) {
    const { value, depth } = stack.pop()!;
    if (++count > MAX_JSON_VALUES) return "too many values";
    if (depth > MAX_JSON_DEPTH) return "too deeply nested";
    if (typeof value === "number" && !Number.isFinite(value)) return "numbers must be finite";
    if (typeof value === "string" && value.length > MAX_STRING_CHARS) return "string too large";
    if (Array.isArray(value)) {
      for (const child of value) stack.push({ value: child, depth: depth + 1 });
    } else if (value && typeof value === "object") {
      for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
        if (key.length > 1_000) return "object key too large";
        stack.push({ value: child, depth: depth + 1 });
      }
    } else if (value !== null && !["string", "number", "boolean"].includes(typeof value)) {
      return "not JSON-safe";
    }
  }
  return null;
}

function boundedInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function metadata(value: string | undefined) {
  const trimmed = value?.trim();
  return trimmed ? trimmed.slice(0, 512) : null;
}

async function existingExchange(db: D1Database, exchangeId: string) {
  return db.prepare("SELECT id, session_id, capture_status FROM exchanges WHERE id = ?").bind(exchangeId).first<{ id: string; session_id: string; capture_status: string }>();
}

function duplicateResponse(c: Context<AppEnv>, existing: { id: string; session_id: string; capture_status: string }, sessionId: string) {
  if (existing.session_id !== sessionId) return c.json({ error: "exchange_id belongs to another session" }, 409);
  return c.json({ exchange_id: existing.id, session_id: sessionId, capture_status: existing.capture_status, duplicate: true });
}
