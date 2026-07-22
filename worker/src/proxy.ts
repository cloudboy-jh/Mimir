import type { Context } from "hono";
import { capture, readBoundedText, type RequestKind } from "./capture";
import { decideCapture, readSaveConfig } from "./config";
import { expireSessions } from "./sessions";
import type { AppEnv } from "./types";

const MAX_REQUEST_BYTES = 10 * 1024 * 1024;
const SESSION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
const REQUEST_KINDS = new Set<RequestKind>(["primary", "title", "summary", "compaction"]);

export async function proxy(c: Context<AppEnv>, endpoint: "chat" | "messages") {
  const started = Date.now();
  const declaredLength = Number(c.req.header("content-length") ?? 0);
  if (declaredLength > MAX_REQUEST_BYTES) return c.json({ error: "request body too large" }, 413);
  let requestBody: string;
  try {
    requestBody = await readBoundedText(c.req.raw.body, MAX_REQUEST_BYTES);
  } catch {
    return c.json({ error: "request body too large" }, 413);
  }
  let request: Record<string, unknown> = {};
  try {
    request = JSON.parse(requestBody) as Record<string, unknown>;
  } catch {
    return c.json({ error: "request body must be JSON" }, 400);
  }
  const model = typeof request.model === "string" ? request.model : "";
  const declaredSession = c.req.header("x-mimir-session") ?? null;
  if (declaredSession && !SESSION_ID.test(declaredSession)) return c.json({ error: "invalid x-mimir-session" }, 400);
  const requestKindHeader = c.req.header("x-mimir-request-kind");
  if (requestKindHeader && !REQUEST_KINDS.has(requestKindHeader as RequestKind)) return c.json({ error: "invalid x-mimir-request-kind" }, 400);
  const requestKind = (requestKindHeader ?? "primary") as RequestKind;
  const repo = metadata(c.req.header("x-mimir-repo"));
  const harness = metadata(c.req.header("x-mimir-harness"));
  const config = await readSaveConfig(c.env.DB);
  await expireSessions(c.env.DB, config.gapMinutes);
  const headers = buildUpstreamHeaders(c.req.raw.headers, c.env.OPENROUTER_API_KEY);
  const upstream = await fetch(`https://openrouter.ai/api/v1${endpoint === "chat" ? "/chat/completions" : "/messages"}`, { method: "POST", headers, body: requestBody });
  const decision = decideCapture(config, repo, model, upstream.body !== null);
  const responseHeaders = new Headers(upstream.headers);
  responseHeaders.set("x-mimir-capture", decision.capture);
  responseHeaders.set("x-mimir-capture-reason", decision.reason);
  if (decision.capture === "skipped" || !upstream.body) return new Response(upstream.body, { status: upstream.status, statusText: upstream.statusText, headers: responseHeaders });
  const [clientBody, archiveBody] = upstream.body.tee();
  c.executionCtx.waitUntil(capture(c.env, {
    request,
    archiveBody,
    endpoint,
    model,
    repo,
    harness,
    accessTokenLabel: c.get("tokenLabel"),
    declaredSession,
    requestKind,
    sourceRef: metadata(c.req.header("x-mimir-git-ref")),
    responseType: upstream.headers.get("content-type") ?? "application/json",
    started,
  }).catch((error) => console.error(JSON.stringify({ message: "exchange capture failed", error: error instanceof Error ? error.message : String(error) }))));
  return new Response(clientBody, { status: upstream.status, statusText: upstream.statusText, headers: responseHeaders });
}

export function buildUpstreamHeaders(source: Headers, openRouterKey: string) {
  const headers = new Headers(source);
  headers.set("authorization", `Bearer ${openRouterKey}`);
  for (const name of ["x-api-key", "x-mimir-session", "x-mimir-repo", "x-mimir-harness", "x-mimir-git-ref", "x-mimir-request-kind", "host"]) headers.delete(name);
  return headers;
}

function metadata(value: string | undefined) {
  const trimmed = value?.trim();
  return trimmed ? trimmed.slice(0, 512) : null;
}
