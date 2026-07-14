import { Hono } from "hono";
import type { Context } from "hono";

type Bindings = Env & {
  OPENROUTER_API_KEY: string;
};

type AppEnv = { Bindings: Bindings };

type SaveConfig = {
  enabled: boolean;
  excludeRepos: string[];
  excludeModels: string[];
  gapMinutes: number;
};

const MAX_REQUEST_BYTES = 10 * 1024 * 1024;
const MAX_RESPONSE_BYTES = 20 * 1024 * 1024;
const SESSION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
const SESSION_COLUMNS = "id, started_at, ended_at, boundary, outcome, outcome_src, repo, source_ref, model_primary, request_count, tokens_in, tokens_out, intent";

const app = new Hono<AppEnv>();

app.onError((error, c) => {
  console.error(JSON.stringify({ message: "request failed", error: error.message, method: c.req.method, path: c.req.path }));
  return c.json({ error: "internal server error" }, 500);
});

app.use("*", async (c, next) => {
  const token = requestToken(c.req.raw.headers);
  if (!token || !(await validToken(c.env.DB, token))) {
    return c.json({ error: "unauthorized" }, 401);
  }
  await next();
});

app.get("/whoami", async (c) => {
  const [sessions, exchanges] = await Promise.all([
    c.env.DB.prepare("SELECT COUNT(*) AS count FROM sessions").first<{ count: number }>(),
    c.env.DB.prepare("SELECT COUNT(*) AS count FROM exchanges").first<{ count: number }>(),
  ]);
  return c.json({ url: new URL(c.req.url).origin, sessions: sessions?.count ?? 0, log: exchanges?.count ?? 0 });
});

app.get("/sessions", async (c) => {
  const where: string[] = [];
  const values: string[] = [];
  for (const [field, column] of [["repo", "repo"], ["model", "model_primary"], ["outcome", "outcome"]] as const) {
    const value = c.req.query(field);
    if (value) {
      where.push(`${column} = ?`);
      values.push(value);
    }
  }
  const from = c.req.query("from");
  if (from) {
    where.push("started_at >= ?");
    values.push(from);
  }
  const to = c.req.query("to");
  if (to) {
    where.push("started_at <= ?");
    values.push(to);
  }
  const sql = `SELECT ${SESSION_COLUMNS} FROM sessions ${where.length ? `WHERE ${where.join(" AND ")}` : ""} ORDER BY started_at DESC LIMIT 100`;
  const results = await c.env.DB.prepare(sql).bind(...values).all();
  return c.json({ sessions: results.results });
});

app.get("/sessions/:id", async (c) => {
  const session = await c.env.DB.prepare(`SELECT ${SESSION_COLUMNS} FROM sessions WHERE id = ?`).bind(c.req.param("id")).first();
  if (!session) return c.json({ error: "session not found" }, 404);
  const [exchanges, files, errors] = await Promise.all([
    c.env.DB.prepare("SELECT * FROM exchanges WHERE session_id = ? ORDER BY ts").bind(c.req.param("id")).all(),
    c.env.DB.prepare("SELECT file FROM session_files WHERE session_id = ? ORDER BY file").bind(c.req.param("id")).all<{ file: string }>(),
    c.env.DB.prepare("SELECT signature FROM session_errors WHERE session_id = ? ORDER BY signature").bind(c.req.param("id")).all<{ signature: string }>(),
  ]);
  return c.json({ session, files: files.results.map((row) => row.file), errors: errors.results.map((row) => row.signature), exchanges: exchanges.results });
});

app.post("/sessions/:id/mark", async (c) => {
	const body = await c.req.json<{ outcome?: string }>();
	return updateOutcome(c, body.outcome, "explicit");
});

app.post("/sessions/:id/outcome", async (c) => {
	const body = await c.req.json<{ outcome?: string; source?: string }>();
	if (body.source !== "git" && body.source !== "explicit") return c.json({ error: "invalid outcome source" }, 400);
	return updateOutcome(c, body.outcome, body.source);
});

async function updateOutcome(c: Context<AppEnv>, outcome: string | undefined, source: "explicit" | "git") {
	const outcomes = new Set(["promoted", "discarded", "abandoned", "unknown"]);
	if (!outcome || !outcomes.has(outcome)) return c.json({ error: "invalid outcome" }, 400);
	const result = await c.env.DB.prepare("UPDATE sessions SET outcome = ?, outcome_src = ? WHERE id = ?").bind(outcome, source, c.req.param("id")).run();
	if (!result.meta.changes) return c.json({ error: "session not found" }, 404);
	return c.json({ id: c.req.param("id"), outcome, outcome_src: source });
}

app.get("/log/*", async (c) => {
  const key = c.req.path.replace(/^\/log\//, "");
  if (!key.startsWith("log/")) return c.json({ error: "invalid log key" }, 400);
  const object = await c.env.LOGS.get(key);
  if (!object) return c.json({ error: "log not found" }, 404);
  return new Response(object.body, { headers: { "content-type": "application/json" } });
});

app.post("/search", async (c) => {
  const body = await c.req.json<{ query?: string; types?: string[]; budget?: number; filters?: { repo?: string; outcome?: string } }>();
  const query = body.query?.trim() ?? "";
  const budget = Math.max(1, Math.min(body.budget ?? 4000, 16000));
  const filters = body.filters ?? {};
  const where = ["(s.intent LIKE ? OR e.request_excerpt LIKE ? OR e.response_excerpt LIKE ? OR EXISTS (SELECT 1 FROM session_files sf WHERE sf.session_id = s.id AND sf.file LIKE ?) OR EXISTS (SELECT 1 FROM session_errors se WHERE se.session_id = s.id AND se.signature LIKE ?))"];
  const needle = `%${query}%`;
  const values: string[] = [needle, needle, needle, needle, needle];
  if (filters.repo) { where.push("s.repo = ?"); values.push(filters.repo); }
  if (filters.outcome) { where.push("s.outcome = ?"); values.push(filters.outcome); }
  const sql = `SELECT s.id AS session_id, s.started_at, s.outcome, s.repo, s.model_primary, e.id AS exchange_id, e.request_excerpt, e.response_excerpt, e.r2_key FROM sessions s JOIN exchanges e ON e.session_id = s.id WHERE ${where.join(" AND ")} ORDER BY s.started_at DESC LIMIT 50`;
  const result = await c.env.DB.prepare(sql).bind(...values).all<Record<string, unknown>>();
  const matches: Record<string, unknown>[] = [];
  let used = 0;
  for (const row of result.results) {
    const cost = JSON.stringify(row).length;
    if (matches.length && used + cost > budget * 4) break;
    matches.push(row);
    used += cost;
  }
  return c.json({ query, budget, matches });
});

app.get("/config", async (c) => c.json(await readConfig(c.env.DB)));

app.put("/config", async (c) => {
  const values = await c.req.json<Record<string, unknown>>();
  const validation = validateConfigValues(values);
  if (validation) return c.json({ error: validation }, 400);
  const statements = Object.entries(values)
    .map(([key, value]) => c.env.DB.prepare("INSERT INTO config(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value").bind(key, JSON.stringify(value)));
  if (statements.length) await c.env.DB.batch(statements);
  return c.json(await readConfig(c.env.DB));
});

app.post("/v1/chat/completions", (c) => proxy(c, "chat"));
app.post("/v1/messages", (c) => proxy(c, "messages"));
app.get("/v1/models", async (c) => {
  const response = await fetch("https://openrouter.ai/api/v1/models", { headers: buildUpstreamHeaders(c.req.raw.headers, c.env.OPENROUTER_API_KEY) });
  return new Response(response.body, response);
});

async function proxy(c: Context<AppEnv>, endpoint: "chat" | "messages") {
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
  try { request = JSON.parse(requestBody) as Record<string, unknown>; } catch { return c.json({ error: "request body must be JSON" }, 400); }
  const model = typeof request.model === "string" ? request.model : "";
  const declaredSession = c.req.header("x-mimir-session") ?? null;
  if (declaredSession && !SESSION_ID.test(declaredSession)) return c.json({ error: "invalid x-mimir-session" }, 400);
  const repo = metadata(c.req.header("x-mimir-repo"));
  const harness = metadata(c.req.header("x-mimir-harness"));
  const config = await readSaveConfig(c.env.DB);
  const save = shouldSave(config, repo, model);
  const headers = buildUpstreamHeaders(c.req.raw.headers, c.env.OPENROUTER_API_KEY);
  const upstream = await fetch(`https://openrouter.ai/api/v1${endpoint === "chat" ? "/chat/completions" : "/messages"}`, { method: "POST", headers, body: requestBody });
  if (!save || !upstream.body) return new Response(upstream.body, upstream);
  const [clientBody, archiveBody] = upstream.body.tee();
  const responseHeaders = new Headers(upstream.headers);
  c.executionCtx.waitUntil(capture(c.env, {
    request, archiveBody, endpoint, model, repo, harness,
    declaredSession,
    sourceRef: metadata(c.req.header("x-mimir-git-ref")),
    responseType: upstream.headers.get("content-type") ?? "application/json",
    started,
  }).catch((error) => console.error(JSON.stringify({ message: "exchange persistence failed", error: error instanceof Error ? error.message : String(error) }))));
  return new Response(clientBody, { status: upstream.status, statusText: upstream.statusText, headers: responseHeaders });
}

async function capture(env: Bindings, input: { request: Record<string, unknown>; archiveBody: ReadableStream<Uint8Array>; endpoint: string; model: string; repo: string | null; harness: string | null; declaredSession: string | null; sourceRef: string | null; responseType: string; started: number }) {
	const responseText = await readBoundedText(input.archiveBody, MAX_RESPONSE_BYTES);
	const response = parseCapturedResponse(responseText, input.responseType);
	const config = await readConfig(env.DB);
	const patterns = array(config["redact.patterns"]);
	const id = ulid();
  const now = new Date().toISOString();
  const r2Key = `log/${now.slice(0, 10).replaceAll("-", "/")}/${id}.json`;
	const redactedRequest = redact(input.request, patterns);
	const redactedResponse = redact(response, patterns);
	const session = await resolveSession(env.DB, input.declaredSession, input.repo, input.sourceRef, input.model, now);
	const usage = extractUsage(response);
	const derived = deriveSessionFields(redactedRequest, redactedResponse);
	const log = { id, ts: now, session: input.declaredSession, model: input.model, endpoint: input.endpoint, request: redactedRequest, response: redactedResponse, usage, latency_ms: Date.now() - input.started, meta: { repo: input.repo, harness: input.harness } };
  await env.LOGS.put(r2Key, JSON.stringify(log), { httpMetadata: { contentType: "application/json" } });
  const requestExcerpt = excerpt(JSON.stringify(redactedRequest));
  const responseExcerpt = excerpt(JSON.stringify(redactedResponse));
  await env.DB.batch([
    env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, response_excerpt, usage_json, latency_ms, repo, harness, r2_key) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)").bind(id, session.id, now, input.endpoint, input.model, requestExcerpt, responseExcerpt, JSON.stringify(usage), Date.now() - input.started, input.repo, input.harness, r2Key),
		env.DB.prepare("UPDATE sessions SET ended_at = ?, model_primary = COALESCE(model_primary, ?), request_count = request_count + 1, tokens_in = tokens_in + ?, tokens_out = tokens_out + ? WHERE id = ?").bind(now, input.model, usage.prompt_tokens, usage.completion_tokens, session.id),
    ...derived.files.map((file) => env.DB.prepare("INSERT OR IGNORE INTO session_files(session_id, file) VALUES (?, ?)").bind(session.id, file)),
    ...derived.errors.map((signature) => env.DB.prepare("INSERT OR IGNORE INTO session_errors(session_id, signature) VALUES (?, ?)").bind(session.id, signature)),
  ]);
}

async function resolveSession(db: D1Database, declared: string | null, repo: string | null, sourceRef: string | null, model: string, now: string) {
	if (declared) {
		await db.prepare("INSERT OR IGNORE INTO sessions(id, started_at, boundary, repo, source_ref, model_primary) VALUES (?, ?, 'header', ?, ?, ?)").bind(declared, now, repo, sourceRef, model).run();
    return { id: declared };
  }
  const config = await readSaveConfig(db);
  const cutoff = new Date(Date.parse(now) - config.gapMinutes * 60_000).toISOString();
  const prior = await db.prepare("SELECT id FROM sessions WHERE boundary = 'heuristic' AND repo IS ? AND ended_at >= ? ORDER BY ended_at DESC LIMIT 1").bind(repo, cutoff).first<{ id: string }>();
  if (prior) return prior;
  const id = ulid();
  await db.prepare("INSERT INTO sessions(id, started_at, boundary, repo, model_primary) VALUES (?, ?, 'heuristic', ?, ?)").bind(id, now, repo, model).run();
  return { id };
}

async function readConfig(db: D1Database) {
  const result = await db.prepare("SELECT key, value FROM config").all<{ key: string; value: string }>();
  const config: Record<string, unknown> = {
    "save.enabled": true, "save.exclude_repos": [], "save.exclude_models": [], "redact.patterns": ["builtin"], "session.gap_minutes": 15, "session.abandon_days": 7,
  };
  for (const row of result.results) config[row.key] = parseJSON(row.value);
  return config;
}

async function readSaveConfig(db: D1Database): Promise<SaveConfig> {
  const config = await readConfig(db);
  return { enabled: config["save.enabled"] !== false, excludeRepos: array(config["save.exclude_repos"]), excludeModels: array(config["save.exclude_models"]), gapMinutes: number(config["session.gap_minutes"], 15) };
}

function shouldSave(config: SaveConfig, repo: string | null, model: string) { return config.enabled && !config.excludeRepos.some((value) => matches(value, repo ?? "")) && !config.excludeModels.some((value) => matches(value, model)); }
function matches(pattern: string, value: string) { return new RegExp(`^${pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&").replaceAll("*", ".*")}$`).test(value); }
function array(value: unknown) { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []; }
function number(value: unknown, fallback: number) { return typeof value === "number" && Number.isFinite(value) ? value : fallback; }
function parseJSON(value: string) { try { return JSON.parse(value) as unknown; } catch { return value; } }
function excerpt(value: string) { return value.slice(0, 8_000); }
function extractUsage(response: unknown) {
  const records = typeof response === "object" && response ? response as Record<string, unknown> : {};
  const events = Array.isArray(records.events) ? records.events : [response];
  let promptTokens = 0;
  let completionTokens = 0;
  for (const event of events) {
    const record = typeof event === "object" && event ? event as Record<string, unknown> : {};
    const message = typeof record.message === "object" && record.message ? record.message as Record<string, unknown> : {};
    const usage = typeof record.usage === "object" && record.usage ? record.usage as Record<string, unknown> : typeof message.usage === "object" && message.usage ? message.usage as Record<string, unknown> : {};
    promptTokens = Math.max(promptTokens, number(usage.prompt_tokens ?? usage.input_tokens, 0));
    completionTokens = Math.max(completionTokens, number(usage.completion_tokens ?? usage.output_tokens, 0));
  }
  return { prompt_tokens: promptTokens, completion_tokens: completionTokens };
}
function redact(value: unknown, patterns: string[]): unknown {
  let text = JSON.stringify(value)
    .replace(/(?:sk|pk|rk)_[A-Za-z0-9_-]{16,}/g, "[REDACTED]")
    .replace(/(?:Bearer\s+)[A-Za-z0-9._-]+/gi, "$1[REDACTED]")
    .replace(/((?:api[_-]?key|token|secret|password)["']?\s*[:=]\s*["']?)[^\s,"'}]+/gi, "$1[REDACTED]");
  for (const pattern of patterns) {
    if (pattern === "builtin") continue;
    try { text = text.replace(new RegExp(pattern, "g"), "[REDACTED]"); } catch { /* Invalid patterns are inert rather than blocking the proxy. */ }
  }
  return parseJSON(text);
}
function deriveSessionFields(...values: unknown[]) {
  const text = values.map((value) => JSON.stringify(value)).join("\n");
  const files = text.match(/(?:[A-Za-z0-9_.-]+\/)*[A-Za-z0-9_.-]+\.(?:tsx|ts|jsx|js|cpp|hpp|json|yaml|yml|sql|java|go|py|rs|cs|md|c|h)(?![A-Za-z0-9_.-])/g) ?? [];
  const errors = text.match(/(?:error|exception|panic|failed)[:\s][^\n"}]{1,160}/gi) ?? [];
  return { files: unique(files, 100), errors: unique(errors, 20) };
}
function unique(values: string[], limit: number) { return [...new Set(values.map((value) => value.trim()).filter(Boolean))].slice(0, limit); }
function metadata(value: string | undefined) { const trimmed = value?.trim(); return trimmed ? trimmed.slice(0, 512) : null; }
function validateConfigValues(values: Record<string, unknown>) {
  const allowed = new Set(["save.enabled", "save.exclude_repos", "save.exclude_models", "redact.patterns", "session.gap_minutes", "session.abandon_days"]);
  for (const [key, value] of Object.entries(values)) {
    if (!allowed.has(key)) return `unknown config key: ${key}`;
    if (key === "save.enabled" && typeof value !== "boolean") return `${key} must be boolean`;
    if (["save.exclude_repos", "save.exclude_models", "redact.patterns"].includes(key)) {
      if (!Array.isArray(value) || value.length > 100 || value.some((item) => typeof item !== "string" || item.length > 256)) return `${key} must be an array of strings up to 256 characters`;
    }
    if (key === "session.gap_minutes" && (typeof value !== "number" || !Number.isInteger(value) || value < 1 || value > 1440)) return `${key} must be an integer from 1 to 1440`;
    if (key === "session.abandon_days" && (typeof value !== "number" || !Number.isInteger(value) || value < 1 || value > 365)) return `${key} must be an integer from 1 to 365`;
  }
  return "";
}
function ulid() { const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"; let time = Date.now(); let prefix = ""; for (let i = 0; i < 10; i++) { prefix = alphabet[time % 32] + prefix; time = Math.floor(time / 32); } const bytes = crypto.getRandomValues(new Uint8Array(16)); return prefix + Array.from(bytes, (byte) => alphabet[byte % 32]).join(""); }

async function validToken(db: D1Database, token: string) {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(token));
  const hash = Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
  return Boolean(await db.prepare("SELECT 1 FROM access_tokens WHERE token_hash = ? AND revoked_at IS NULL").bind(hash).first());
}

function requestToken(headers: Headers) {
  const auth = headers.get("authorization");
  return auth?.startsWith("Bearer ") ? auth.slice(7) : headers.get("x-api-key") ?? undefined;
}

function buildUpstreamHeaders(source: Headers, openRouterKey: string) {
  const headers = new Headers(source);
  headers.set("authorization", `Bearer ${openRouterKey}`);
  for (const name of ["x-api-key", "x-mimir-session", "x-mimir-repo", "x-mimir-harness", "x-mimir-git-ref", "host"]) headers.delete(name);
  return headers;
}

async function readBoundedText(stream: ReadableStream<Uint8Array> | null, limit: number) {
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

function parseCapturedResponse(text: string, contentType: string): unknown {
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

export { app, buildUpstreamHeaders, deriveSessionFields, extractUsage, parseCapturedResponse, readBoundedText, redact, requestToken, shouldSave, validateConfigValues };
export default app;
