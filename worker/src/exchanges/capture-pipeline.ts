import { readConfig, stringArray } from "../config/config-store";
import type { Bindings } from "../env";
import { reportSessionEvent } from "../sessions/events";
import { resolveSession } from "../sessions/lifecycle";
import { ulid } from "../shared/ulid";
import {
  extractGeneratedTitle,
  finalizedExchangeTitleStatements,
} from "../sessions/titles";
import {
  classifyRequestKind,
  deriveIntent,
  deriveSessionFields,
  excerpt,
  extractFinishReason,
  extractProvider,
} from "./evidence";
import type { RequestKind } from "./exchange-types";
import {
  extractUsage,
  parseCapturedResponse,
  readBoundedText,
} from "./response-codec";
import { redact } from "./redaction";

export const MAX_RESPONSE_BYTES = 20 * 1024 * 1024;

type CaptureInput = {
  request: Record<string, unknown>;
  archiveBody: ReadableStream<Uint8Array>;
  endpoint: string;
  model: string;
  repo: string | null;
  harness: string | null;
  accessTokenLabel: string;
  installationID: string | null;
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

export async function capture(
  env: Bindings,
  input: CaptureInput,
): Promise<void> {
  const id = ulid();
  const activityAt = new Date(input.started).toISOString();
  const responseResultPromise = readBoundedText(
    input.archiveBody,
    MAX_RESPONSE_BYTES,
  )
    .then((text) => ({ text, error: null as unknown }))
    .catch((error: unknown) => ({ text: "", error }));
  const [config, session] = await Promise.all([
    readConfig(env.DB),
    resolveSession(
      env.DB,
      input.declaredSession,
      input.repo,
      input.harness,
      input.sourceRef,
      input.model,
      activityAt,
      input.installationID,
    ),
  ]);
  const patterns = stringArray(config["redact.patterns"]);
  const redactedRequest = redact(input.request, patterns);
  const requestKind = classifyRequestKind(input.requestKind, redactedRequest);
  const intentCandidate =
    requestKind === "primary" ? deriveIntent(redactedRequest) : null;
  const requestExcerpt = excerpt(JSON.stringify(redactedRequest));
  const acceptedAt = new Date().toISOString();
  const r2Key = `log/${acceptedAt.slice(0, 10).replaceAll("-", "/")}/${id}.json`;
  const accepted = await acceptExchange(
    env.DB,
    input,
    id,
    session.id,
    activityAt,
    acceptedAt,
    r2Key,
    requestExcerpt,
    requestKind,
    intentCandidate,
  );
  if (!accepted) {
    await responseResultPromise;
    return;
  }

  const responseResult = await responseResultPromise;
  if (responseResult.error) {
    const tooLarge =
      responseResult.error instanceof Error &&
      responseResult.error.message === "capture limit exceeded";
    const failureCode = tooLarge
      ? "response_too_large"
      : "response_read_failed";
    await failExchange(
      env.DB,
      id,
      failureCode,
      tooLarge ? "response_size_limit" : "response_read",
    );
    logCaptureError(
      tooLarge
        ? "capture response exceeded size limit"
        : "capture response read failed",
      responseResult.error,
      id,
      session.id,
      failureCode,
    );
    return;
  }

  const parsedResponse = parseCapturedResponse(
    responseResult.text,
    input.responseType,
  );
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
  const titleCandidate =
    requestKind === "title" ? extractGeneratedTitle(redactedResponse) : null;
  const latency = Date.now() - input.started;
  if (
    !(await prepareAcceptedExchange(
      env.DB,
      id,
      session.id,
      prepared,
      latency,
      titleCandidate,
    ))
  )
    return;

  const reconstructed = prepared.redactedResponse as {
    content?: unknown;
    events?: unknown;
  };
  const response = input.responseType.includes("text/event-stream")
    ? {
        format: "reconstructed_sse",
        content: reconstructed.content ?? "",
        events: reconstructed.events ?? [],
      }
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
    metadata: {
      repo: input.repo,
      harness: input.harness,
      git_ref: input.sourceRef,
      installation_id: input.installationID,
      model: input.model,
      provider: prepared.provider,
      finish_reason: prepared.finishReason,
      request_kind: requestKind,
    },
    usage: {
      input_tokens: prepared.usage.prompt_tokens,
      output_tokens: prepared.usage.completion_tokens,
      cache_read_tokens: prepared.usage.cache_read_tokens,
      cache_write_tokens: prepared.usage.cache_write_tokens,
    },
    latency_ms: latency,
    redaction: { version: 1 },
  };
  const objectBody = JSON.stringify(envelope);
  const r2Bytes = new TextEncoder().encode(objectBody).byteLength;
  try {
    await env.LOGS.put(r2Key, objectBody, {
      httpMetadata: { contentType: "application/json" },
    });
  } catch (error) {
    await failExchange(env.DB, id, "r2_write_failed", "r2_write");
    logCaptureError(
      "capture R2 write failed",
      error,
      id,
      session.id,
      "r2_write_failed",
    );
    return;
  }

  try {
    await finalizeAcceptedExchange(
      env.DB,
      id,
      session.id,
      activityAt,
      new Date().toISOString(),
      input.harness,
      input.model,
      prepared.usage.prompt_tokens,
      prepared.usage.completion_tokens,
      prepared.usage.cache_read_tokens,
      prepared.usage.cache_write_tokens,
      r2Bytes,
      true,
      titleCandidate,
    );
  } catch (error) {
    logCaptureError(
      "capture D1 finalization failed",
      error,
      id,
      session.id,
      "d1_finalize_failed",
    );
    return;
  }

  const provider = prepared.provider ? ` · ${prepared.provider}` : "";
  console.log(
    `saved exchange ${id} to session ${session.id} · ${input.model}${provider} · ${prepared.usage.prompt_tokens} in / ${prepared.usage.completion_tokens} out · ${latency}ms`,
  );
  await reportSessionEvent(env, {
    version: 1,
    kind: "turn",
    session_id: session.id,
    installation_id: input.installationID,
    harness: input.harness,
    repo: input.repo,
    ts: new Date().toISOString(),
    turn: {
      exchange_id: id,
      model: input.model,
      provider: prepared.provider,
      request_kind: requestKind,
      usage: {
        input_tokens: prepared.usage.prompt_tokens,
        output_tokens: prepared.usage.completion_tokens,
        cache_read_tokens: prepared.usage.cache_read_tokens,
        cache_write_tokens: prepared.usage.cache_write_tokens,
      },
      latency_ms: latency,
      excerpt: requestExcerpt.slice(0, 500),
    },
  });
}

async function acceptExchange(
  db: D1Database,
  input: CaptureInput,
  id: string,
  sessionId: string,
  activityAt: string,
  acceptedAt: string,
  r2Key: string,
  requestExcerpt: string,
  requestKind: RequestKind,
  intentCandidate: string | null,
) {
  try {
    await db
      .prepare(
        "INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, response_excerpt, usage_json, latency_ms, repo, harness, r2_key, access_token_label, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, schema_version, request_kind, intent_candidate) VALUES (?, ?, ?, ?, ?, ?, '', '{}', 0, ?, ?, ?, ?, 0, 0, 'accepted', 'enabled', ?, 1, ?, ?)",
      )
      .bind(
        id,
        sessionId,
        activityAt,
        input.endpoint,
        input.model,
        requestExcerpt,
        input.repo,
        input.harness,
        r2Key,
        input.accessTokenLabel,
        acceptedAt,
        requestKind,
        intentCandidate,
      )
      .run();
    return true;
  } catch (error) {
    logCaptureError(
      "capture D1 acceptance failed",
      error,
      id,
      sessionId,
      "d1_accept_failed",
    );
    return false;
  }
}

async function prepareAcceptedExchange(
  db: D1Database,
  exchangeId: string,
  sessionId: string,
  prepared: PreparedCapture,
  latency: number,
  titleCandidate: string | null,
) {
  try {
    await db.batch([
      db
        .prepare(
          "UPDATE exchanges SET response_excerpt = ?, usage_json = ?, latency_ms = ?, provider = ?, finish_reason = ?, input_tokens = ?, output_tokens = ?, cache_read_tokens = ?, cache_write_tokens = ?, title_candidate = ? WHERE id = ? AND capture_status = 'accepted'",
        )
        .bind(
          prepared.responseExcerpt,
          JSON.stringify(prepared.usage),
          latency,
          prepared.provider,
          prepared.finishReason,
          prepared.usage.prompt_tokens,
          prepared.usage.completion_tokens,
          prepared.usage.cache_read_tokens,
          prepared.usage.cache_write_tokens,
          titleCandidate,
          exchangeId,
        ),
      ...prepared.files.map((file) =>
        db
          .prepare(
            "INSERT INTO exchange_files(exchange_id, session_id, file) VALUES (?, ?, ?)",
          )
          .bind(exchangeId, sessionId, file),
      ),
      ...prepared.errors.map((signature) =>
        db
          .prepare(
            "INSERT INTO exchange_errors(exchange_id, session_id, signature) VALUES (?, ?, ?)",
          )
          .bind(exchangeId, sessionId, signature),
      ),
    ]);
    return true;
  } catch (error) {
    await failExchange(db, exchangeId, "d1_prepare_failed", "d1_prepare");
    logCaptureError(
      "capture D1 preparation failed",
      error,
      exchangeId,
      sessionId,
      "d1_prepare_failed",
    );
    return false;
  }
}

export async function finalizeAcceptedExchange(
  db: D1Database,
  exchangeId: string,
  sessionId: string,
  activityAt: string,
  savedAt: string,
  harness: string | null,
  model: string,
  inputTokens: number,
  outputTokens: number,
  cacheReadTokens: number,
  cacheWriteTokens: number,
  r2Bytes: number | null,
  reactivate: boolean,
  generatedTitle: string | null = null,
) {
  await db.batch([
    db
      .prepare(
        "UPDATE sessions SET ended_at = CASE WHEN ended_at IS NULL OR ended_at < ? THEN ? ELSE ended_at END, last_active_at = CASE WHEN last_active_at IS NULL OR last_active_at < ? THEN ? ELSE last_active_at END, harness = COALESCE(harness, ?), state = CASE WHEN ? AND (inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at OR inactive_at < ?) AND (boundary = 'header' OR NOT EXISTS (SELECT 1 FROM sessions active WHERE active.id <> sessions.id AND active.boundary = 'heuristic' AND active.state = 'active' AND active.repo IS sessions.repo AND active.harness IS sessions.harness AND active.installation_id IS sessions.installation_id)) THEN 'active' ELSE state END, inactive_at = CASE WHEN ? AND (inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at OR inactive_at < ?) AND (boundary = 'header' OR NOT EXISTS (SELECT 1 FROM sessions active WHERE active.id <> sessions.id AND active.boundary = 'heuristic' AND active.state = 'active' AND active.repo IS sessions.repo AND active.harness IS sessions.harness AND active.installation_id IS sessions.installation_id)) THEN NULL ELSE inactive_at END, model_primary = COALESCE(model_primary, ?), request_count = request_count + 1, tokens_in = tokens_in + ?, tokens_out = tokens_out + ?, cache_read_tokens = cache_read_tokens + ?, cache_write_tokens = cache_write_tokens + ?, summary_text = NULL, summary_status = 'pending', summary_source = NULL, summary_updated_at = NULL WHERE id = ? AND EXISTS (SELECT 1 FROM exchanges WHERE id = ? AND capture_status = 'accepted')",
      )
      .bind(
        activityAt,
        activityAt,
        activityAt,
        activityAt,
        harness,
        reactivate ? 1 : 0,
        activityAt,
        reactivate ? 1 : 0,
        activityAt,
        model,
        inputTokens,
        outputTokens,
        cacheReadTokens,
        cacheWriteTokens,
        sessionId,
        exchangeId,
      ),
    db
      .prepare(
        "INSERT OR IGNORE INTO session_files(session_id, file) SELECT session_id, file FROM exchange_files WHERE exchange_id = ? AND EXISTS (SELECT 1 FROM exchanges WHERE id = ? AND capture_status = 'accepted')",
      )
      .bind(exchangeId, exchangeId),
    db
      .prepare(
        "INSERT OR IGNORE INTO session_errors(session_id, signature) SELECT session_id, signature FROM exchange_errors WHERE exchange_id = ? AND EXISTS (SELECT 1 FROM exchanges WHERE id = ? AND capture_status = 'accepted')",
      )
      .bind(exchangeId, exchangeId),
    db
      .prepare(
        "UPDATE exchanges SET capture_status = 'saved', capture_reason = 'enabled', saved_at = ?, failed_at = NULL, failure_code = NULL, r2_bytes = ? WHERE id = ? AND capture_status = 'accepted'",
      )
      .bind(savedAt, r2Bytes, exchangeId),
    db
      .prepare(
        "UPDATE sessions SET intent = CASE WHEN intent IS NULL OR EXISTS (SELECT 1 FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'primary' AND intent_candidate = sessions.intent) THEN COALESCE((SELECT intent_candidate FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'primary' AND intent_candidate IS NOT NULL ORDER BY ts ASC, id ASC LIMIT 1), intent) ELSE intent END WHERE id = ?",
      )
      .bind(sessionId, sessionId, sessionId),
    ...finalizedExchangeTitleStatements(
      db,
      sessionId,
      generatedTitle,
      activityAt,
    ),
  ]);
}

async function failExchange(
  db: D1Database,
  exchangeId: string,
  failureCode: string,
  reason: string,
) {
  try {
    await db
      .prepare(
        "UPDATE exchanges SET capture_status = 'failed', capture_reason = ?, failed_at = ?, failure_code = ? WHERE id = ? AND capture_status = 'accepted'",
      )
      .bind(reason, new Date().toISOString(), failureCode, exchangeId)
      .run();
  } catch (error) {
    logCaptureError(
      "capture failure status update failed",
      error,
      exchangeId,
      null,
      "d1_failure_update_failed",
    );
  }
}

function logCaptureError(
  message: string,
  error: unknown,
  exchangeId: string,
  sessionId: string | null,
  failureCode: string,
) {
  console.error(
    JSON.stringify({
      message,
      error: error instanceof Error ? error.message : String(error),
      exchange_id: exchangeId,
      session_id: sessionId,
      failure_code: failureCode,
    }),
  );
}
