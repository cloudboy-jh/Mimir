// Mimir capture plugin for OpenCode.
//
// Reports completed turns, heartbeats, and session ends to the Mimir session
// object. Direct-provider exchanges come from OpenCode's session store;
// OpenRouter exchanges remain canonical at the Mimir proxy.
//
// Install: copy this file to ~/.config/opencode/plugins/ (global) or
// .opencode/plugins/ (project). Uninstall: delete the file.
//
// No credentials live in this file. Connection resolves from, in order:
//   1. MIMIR_URL + MIMIR_TOKEN environment variables
//   2. $MIMIR_HOME/config + $MIMIR_HOME/token
//   3. ~/.mimir/config + ~/.mimir/token (written by `mimir setup`/`mimir login`)
//
// Session deletion is reported immediately. Process loss is covered by the
// server-side silence timer (~10 minutes without a heartbeat).

import { tool, type Plugin } from "@opencode-ai/plugin";
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const HEARTBEAT_MS = 60_000;
const ACTIVITY_WINDOW_MS = 5 * 60_000;
const MAX_PARTS = 256;
const MAX_STRING_BYTES = 64 * 1024;
const MAX_EXCHANGE_BYTES = 512 * 1024;
const MAX_JSON_DEPTH = 8;
const MAX_JSON_ENTRIES = 256;
const EXCHANGE_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;

type Connection = { url: string; token: string };

type HarnessLoad = {
  version: 1;
  harness: "opencode";
  source_sha256: string;
  bundle_version?: string;
  cli_version?: string;
  cli_commit?: string;
  installation_id?: string;
};

type SessionEvent = {
  version: 1;
  kind: "turn" | "heartbeat" | "end";
  session_id: string;
  parent_session_id?: string | null;
  harness: string | null;
  repo?: string | null;
  ts: string;
  turn?: Record<string, unknown>;
  reason?: string;
};

type DirectExchange = {
  exchange_id: string;
  ts: string;
  provider: string;
  model: string;
  request_kind: "primary";
  request: { message_id: string; created_at: string; messages: Array<{ role: "user"; content: Record<string, unknown>[] }> };
  response: { message_id: string; parent_message_id: string; role: "assistant"; created_at: string; completed_at: string; parts: Record<string, unknown>[]; stop_reason?: string; error?: unknown };
  usage: { input_tokens: number; output_tokens: number };
  latency_ms: number;
};

type MessageRecord = { info: Record<string, unknown>; parts: unknown[] };
type OpenCodeClient = { session: { messages(input: { path: { id: string } }): Promise<unknown> } };

function parseMimirConfig(text: string): { url?: string } {
  const out: { url?: string } = {};
  for (const line of text.split("\n")) {
    const match = line.match(/^\s*([A-Za-z_]+)\s*=\s*"?([^"\n]*)"?\s*$/);
    if (match && match[1] === "url" && match[2]) out.url = match[2].replace(/\/+$/, "");
  }
  return out;
}

function resolveConnection(
  env: Record<string, string | undefined>,
  readFile: (path: string) => string | null,
  home: string | undefined,
): Connection | null {
  const envUrl = env.MIMIR_URL?.trim();
  const envToken = env.MIMIR_TOKEN?.trim();
  if (envUrl && envToken) return { url: envUrl.replace(/\/+$/, ""), token: envToken };
  const dir = env.MIMIR_HOME?.trim() || (home ? join(home, ".mimir") : null);
  if (!dir) return null;
  const config = readFile(join(dir, "config"));
  const token = readFile(join(dir, "token"))?.trim();
  const url = config ? parseMimirConfig(config).url : undefined;
  return url && token ? { url, token } : null;
}

function loadConnection(): Connection | null {
  return resolveConnection(
    process.env,
    (path) => {
      try {
        return existsSync(path) ? readFileSync(path, "utf8") : null;
      } catch {
        return null;
      }
    },
    (() => {
      try {
        return homedir();
      } catch {
        return undefined;
      }
    })(),
  );
}

function buildHarnessLoad(source: string, receiptText: string | null): HarnessLoad {
  const load: HarnessLoad = {
    version: 1,
    harness: "opencode",
    source_sha256: createHash("sha256").update(source).digest("hex"),
  };
  if (!receiptText) return load;
  try {
    const receipt = JSON.parse(receiptText) as Record<string, unknown>;
    const cli = typeof receipt.cli === "object" && receipt.cli ? receipt.cli as Record<string, unknown> : {};
    if (typeof receipt.bundle_version === "string" && receipt.bundle_version) load.bundle_version = receipt.bundle_version;
    if (typeof cli.version === "string" && cli.version) load.cli_version = cli.version;
    if (typeof cli.commit === "string" && cli.commit) load.cli_commit = cli.commit;
    if (typeof receipt.installation_id === "string" && receipt.installation_id) load.installation_id = receipt.installation_id;
  } catch {
    // A missing or malformed receipt must not suppress source identity.
  }
  return load;
}

function loadHarnessLoad(
  env: Record<string, string | undefined>,
  readFile: (path: string) => string | null,
  home: string | undefined,
  sourcePath = fileURLToPath(import.meta.url),
): HarnessLoad | null {
  const source = readFile(sourcePath);
  if (source === null) return null;
  const dir = env.MIMIR_HOME?.trim() || (home ? join(home, ".mimir") : null);
  const receipt = dir ? readFile(join(dir, "install-receipt.json")) : null;
  return buildHarnessLoad(source, receipt);
}

async function postHarnessLoad(conn: Connection, load: HarnessLoad): Promise<boolean> {
  try {
    const response = await fetch(`${conn.url}/integrations/harness-loads`, {
      method: "POST",
      headers: { authorization: `Bearer ${conn.token}`, "content-type": "application/json" },
      body: JSON.stringify(load),
    });
    return response.ok;
  } catch {
    return false;
  }
}

function reportHarnessLoad(
  conn: Connection,
  load: HarnessLoad,
  post: (conn: Connection, load: HarnessLoad) => Promise<boolean> = postHarnessLoad,
  schedule: (callback: () => void, delay: number) => unknown = setTimeout,
): void {
  const attempt = async (number: number) => {
    if (await post(conn, load) || number >= 4) return;
    const timer = schedule(() => { void attempt(number + 1); }, 250 * (2 ** (number - 1)));
    (timer as { unref?: () => void }).unref?.();
  };
  void attempt(1);
}

// buildTurnEvent converts a completed OpenCode assistant message into a Mimir
// turn event. In-progress and non-assistant messages return null.
function buildTurnEvent(info: unknown, repo: string | null): SessionEvent | null {
  if (typeof info !== "object" || !info) return null;
  const message = info as Record<string, unknown>;
  if (message.role !== "assistant") return null;
  const time = message.time as Record<string, unknown> | undefined;
  const created = typeof time?.created === "number" ? time.created : null;
  const completed = typeof time?.completed === "number" ? time.completed : null;
  if (!completed || typeof message.sessionID !== "string" || !message.sessionID) return null;
  const tokens = (message.tokens ?? {}) as Record<string, unknown>;
  const input = typeof tokens.input === "number" ? tokens.input : 0;
  const cache = (tokens.cache ?? {}) as Record<string, unknown>;
  const cacheRead = typeof cache.read === "number" ? cache.read : 0;
  const output = typeof tokens.output === "number" ? tokens.output : 0;
  return {
    version: 1,
    kind: "turn",
    session_id: message.sessionID,
    harness: "opencode",
    repo,
    ts: new Date(completed).toISOString(),
    turn: {
      exchange_id: typeof message.id === "string" ? message.id : undefined,
      model: typeof message.modelID === "string" ? message.modelID : undefined,
      provider: typeof message.providerID === "string" ? message.providerID : undefined,
      request_kind: "primary",
      usage: { input_tokens: input + cacheRead, output_tokens: output },
      latency_ms: created ? Math.max(0, completed - created) : undefined,
    },
  };
}

function boundedString(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const encoder = new TextEncoder();
  if (encoder.encode(value).byteLength <= MAX_STRING_BYTES) return value;
  let low = 0;
  let high = value.length;
  while (low < high) {
    const middle = Math.ceil((low + high) / 2);
    if (encoder.encode(value.slice(0, middle)).byteLength <= MAX_STRING_BYTES) low = middle;
    else high = middle - 1;
  }
  return value.slice(0, low);
}

function jsonSafe(value: unknown, depth = 0, seen = new WeakSet<object>(), budget = { remaining: MAX_JSON_ENTRIES }): unknown {
  if (budget.remaining-- <= 0) return undefined;
  if (value === null || typeof value === "boolean") return value;
  if (typeof value === "string") return boundedString(value);
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (typeof value === "bigint") return value.toString();
  if (typeof value !== "object" || depth >= MAX_JSON_DEPTH || seen.has(value)) return undefined;
  seen.add(value);
  if (Array.isArray(value)) {
    const result: unknown[] = [];
    for (const item of value.slice(0, MAX_JSON_ENTRIES)) {
      const safe = jsonSafe(item, depth + 1, seen, budget);
      if (safe !== undefined) result.push(safe);
      if (budget.remaining <= 0) break;
    }
    seen.delete(value);
    return result;
  }
  const result: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value).slice(0, MAX_JSON_ENTRIES)) {
    const safe = jsonSafe(item, depth + 1, seen, budget);
    if (safe !== undefined) result[boundedString(key) ?? ""] = safe;
    if (budget.remaining <= 0) break;
  }
  seen.delete(value);
  return result;
}

function normalizeParts(parts: unknown): Record<string, unknown>[] {
  if (!Array.isArray(parts)) return [];
  const normalized: Record<string, unknown>[] = [];
  for (const raw of parts.slice(0, MAX_PARTS)) {
    if (!raw || typeof raw !== "object") continue;
    const part = raw as Record<string, unknown>;
    if (part.type === "text" || part.type === "reasoning") {
      const text = boundedString(part.text);
      if (text !== undefined) normalized.push({ type: part.type, text });
      continue;
    }
    if (part.type === "file") {
      const file: Record<string, unknown> = { type: "file" };
      for (const key of ["mime", "filename", "url"] as const) {
        const value = boundedString(part[key]);
        if (value !== undefined) file[key] = value;
      }
      const source = jsonSafe(part.source);
      if (source !== undefined) file.source = source;
      normalized.push(file);
      continue;
    }
    if (part.type === "tool") {
      const state = part.state && typeof part.state === "object" ? part.state as Record<string, unknown> : {};
      const tool: Record<string, unknown> = { type: "tool" };
      const callID = boundedString(part.callID);
      const toolName = boundedString(part.tool);
      const status = boundedString(state.status);
      if (callID !== undefined) tool.call_id = callID;
      if (toolName !== undefined) tool.tool = toolName;
      if (status !== undefined) tool.status = status;
      const input = jsonSafe(state.input);
      if (input !== undefined) tool.input = input;
      for (const key of ["output", "error", "raw", "title"] as const) {
        const value = jsonSafe(state[key]);
        if (value !== undefined) tool[key] = value;
      }
      const attachments = normalizeParts(state.attachments);
      if (attachments.length) tool.attachments = attachments;
      normalized.push(tool);
    }
  }
  return normalized;
}

function messageRecords(result: unknown): MessageRecord[] {
  const value = result && typeof result === "object" && "data" in result ? (result as { data?: unknown }).data : result;
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    if (!item || typeof item !== "object") return [];
    const record = item as Record<string, unknown>;
    if (!record.info || typeof record.info !== "object") return [];
    return [{ info: record.info as Record<string, unknown>, parts: Array.isArray(record.parts) ? record.parts : [] }];
  });
}

function tokenCount(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
}

function buildDirectExchange(info: unknown, result: unknown): DirectExchange | null {
  if (!info || typeof info !== "object") return null;
  const completedInfo = info as Record<string, unknown>;
  if (completedInfo.role !== "assistant" || completedInfo.providerID === "openrouter") return null;
  const id = boundedString(completedInfo.id);
  const sessionID = boundedString(completedInfo.sessionID);
  const parentID = boundedString(completedInfo.parentID);
  const provider = boundedString(completedInfo.providerID)?.slice(0, 256);
  const model = boundedString(completedInfo.modelID)?.slice(0, 256);
  const time = completedInfo.time && typeof completedInfo.time === "object" ? completedInfo.time as Record<string, unknown> : {};
  const created = typeof time.created === "number" && Number.isFinite(time.created) ? time.created : null;
  const completed = typeof time.completed === "number" && Number.isFinite(time.completed) ? time.completed : null;
  if (!id || !EXCHANGE_ID.test(id) || !sessionID || !parentID || !provider || !model || created === null || completed === null) return null;

  const records = messageRecords(result);
  const assistant = records.find((message) => message.info.id === id);
  const user = records.find((message) => message.info.id === parentID && message.info.role === "user");
  if (!assistant || !user) return null;
  const userTime = user.info.time && typeof user.info.time === "object" ? user.info.time as Record<string, unknown> : {};
  const userCreated = typeof userTime.created === "number" && Number.isFinite(userTime.created) ? userTime.created : created;
  const tokens = completedInfo.tokens && typeof completedInfo.tokens === "object" ? completedInfo.tokens as Record<string, unknown> : {};
  const cache = tokens.cache && typeof tokens.cache === "object" ? tokens.cache as Record<string, unknown> : {};
  const payload: DirectExchange = {
    exchange_id: id,
    ts: new Date(completed).toISOString(),
    provider,
    model,
    request_kind: "primary",
    request: { message_id: parentID, created_at: new Date(userCreated).toISOString(), messages: [{ role: "user", content: normalizeParts(user.parts) }] },
    response: { message_id: id, parent_message_id: parentID, role: "assistant", created_at: new Date(created).toISOString(), completed_at: new Date(completed).toISOString(), parts: normalizeParts(assistant.parts) },
    usage: {
      input_tokens: tokenCount(tokens.input) + tokenCount(cache.read),
      output_tokens: tokenCount(tokens.output),
    },
    latency_ms: Math.max(0, Math.floor(completed - created)),
  };
  const finish = boundedString(completedInfo.finish)?.slice(0, 256);
  if (finish) payload.response.stop_reason = finish;
  const error = jsonSafe(completedInfo.error);
  if (error !== undefined) payload.response.error = error;
  while (new TextEncoder().encode(JSON.stringify(payload)).byteLength > MAX_EXCHANGE_BYTES) {
    const requestParts = payload.request.messages[0].content;
    if (payload.response.parts.length >= requestParts.length && payload.response.parts.length) payload.response.parts.pop();
    else if (requestParts.length) requestParts.pop();
    else return null;
  }
  return payload;
}

function repoName(directory: string | undefined): string | null {
  if (!directory) return null;
  const parts = directory.replace(/[\\/]+$/, "").split(/[\\/]/);
  return parts[parts.length - 1] || null;
}

function createDeliveryQueue(
  send: (event: SessionEvent) => Promise<boolean>,
  schedule: (callback: () => void, delay: number) => unknown = setTimeout,
  maxAttempts = 4,
) {
  const pending = new Map<string, { event: SessionEvent; attempts: number }>();
  const keyFor = (event: SessionEvent) => {
    const exchange = event.turn?.exchange_id;
    return typeof exchange === "string" && exchange ? `turn:${exchange}` : `${event.kind}:${event.session_id}:${event.ts}`;
  };
  const attempt = async (key: string) => {
    const item = pending.get(key);
    if (!item) return;
    item.attempts += 1;
    if (await send(item.event)) {
      pending.delete(key);
      return;
    }
    if (item.attempts >= maxAttempts) {
      pending.delete(key);
      return;
    }
    schedule(() => { void attempt(key); }, 250 * (2 ** (item.attempts - 1)));
  };
  return {
    deliver(event: SessionEvent): void {
      const key = keyFor(event);
      if (pending.has(key)) return;
      pending.set(key, { event, attempts: 0 });
      void attempt(key);
    },
    pending: () => pending.size,
  };
}

function createActivityTracker(now: () => number = Date.now) {
  let last: { sessionID: string; at: number } | null = null;
  return {
    touch(sessionID: string): void { last = { sessionID, at: now() }; },
    clear(sessionID: string): void { if (last?.sessionID === sessionID) last = null; },
    active(): string | null { return last && now() - last.at < ACTIVITY_WINDOW_MS ? last.sessionID : null; },
  };
}

async function postEvent(conn: Connection, event: SessionEvent): Promise<boolean> {
  try {
    const response = await fetch(`${conn.url}/sessions/${encodeURIComponent(event.session_id)}/events`, {
      method: "POST",
      headers: { authorization: `Bearer ${conn.token}`, "content-type": "application/json" },
      body: JSON.stringify(event),
    });
    return response.ok;
  } catch {
    // Best-effort: capture must never interrupt the harness.
    return false;
  }
}

async function postDirectExchange(conn: Connection, sessionID: string, exchange: DirectExchange, repo: string | null): Promise<boolean> {
  try {
    const headers: Record<string, string> = { authorization: `Bearer ${conn.token}`, "content-type": "application/json", "x-mimir-harness": "opencode" };
    if (repo) headers["x-mimir-repo"] = repo;
    const response = await fetch(`${conn.url}/sessions/${encodeURIComponent(sessionID)}/exchanges`, {
      method: "POST",
      headers,
      body: JSON.stringify(exchange),
    });
    return response.ok;
  } catch {
    return false;
  }
}

async function sessionRequest(conn: Connection, sessionID: string, path: "status" | "outcome", body?: Record<string, unknown>): Promise<string> {
  const response = await fetch(`${conn.url}/sessions/${encodeURIComponent(sessionID)}/${path}`, {
    method: body ? "POST" : "GET",
    headers: { authorization: `Bearer ${conn.token}`, accept: "application/json", ...(body ? { "content-type": "application/json" } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await response.text();
  if (!response.ok) throw new Error(`Mimir ${path} failed (${response.status}): ${text}`);
  return text;
}

function createDirectExchangeReporter(
  load: (sessionID: string) => Promise<unknown>,
  send: (sessionID: string, exchange: DirectExchange) => Promise<boolean>,
  schedule: (callback: () => void, delay: number) => unknown = setTimeout,
  maxAttempts = 4,
) {
  const pending = new Map<string, { info: Record<string, unknown>; payload?: DirectExchange; attempts: number }>();
  const attempt = async (id: string) => {
    const item = pending.get(id);
    if (!item) return;
    item.attempts += 1;
    try {
      const sessionID = String(item.info.sessionID);
      if (!item.payload) item.payload = buildDirectExchange(item.info, await load(sessionID)) ?? undefined;
      if (item.payload && await send(sessionID, item.payload)) {
        pending.delete(id);
        return;
      }
    } catch {
      // OpenCode message reads and Mimir delivery are both strictly best-effort.
    }
    if (item.attempts >= maxAttempts) {
      pending.delete(id);
      return;
    }
    schedule(() => { void attempt(id); }, 250 * (2 ** (item.attempts - 1)));
  };
  return {
    deliver(info: unknown): void {
      if (!info || typeof info !== "object") return;
      const message = info as Record<string, unknown>;
      const time = message.time as Record<string, unknown> | undefined;
      if (message.role !== "assistant" || message.providerID === "openrouter" || typeof time?.completed !== "number") return;
      const id = typeof message.id === "string" ? message.id : "";
      if (!id || pending.has(id)) return;
      pending.set(id, { info: message, attempts: 0 });
      void attempt(id);
    },
    pending: () => pending.size,
  };
}

const server: Plugin = async ({ client, directory, worktree }) => {
  const conn = loadConnection();
  if (!conn) return {};
  const readLocalFile = (path: string) => {
    try { return existsSync(path) ? readFileSync(path, "utf8") : null; } catch { return null; }
  };
  const home = (() => { try { return homedir(); } catch { return undefined; } })();
  const load = loadHarnessLoad(process.env, readLocalFile, home);
  if (load) reportHarnessLoad(conn, load);
  const repo = repoName(worktree ?? directory);
  const delivery = createDeliveryQueue((event) => postEvent(conn, event));
  const exchangeReporter = createDirectExchangeReporter(
    (sessionID) => (client as unknown as OpenCodeClient).session.messages({ path: { id: sessionID } }),
    (sessionID, exchange) => postDirectExchange(conn, sessionID, exchange, repo),
  );
  const activity = createActivityTracker();

  const timer = setInterval(() => {
    const sessionID = activity.active();
    if (sessionID) delivery.deliver({ version: 1, kind: "heartbeat", session_id: sessionID, harness: "opencode", repo, ts: new Date().toISOString() });
  }, HEARTBEAT_MS);
  (timer as { unref?: () => void }).unref?.();

  return {
    tool: {
      mimir_session_status: tool({
        description: "Verify the current OpenCode session's durable Mimir capture status.",
        args: {},
        async execute(_args, context) {
          return sessionRequest(conn, context.sessionID, "status");
        },
      }),
      mimir_session_outcome: tool({
        description: "Record the evidenced work outcome for the current OpenCode session. Child-session outcomes are applied to the root work session.",
        args: {
          outcome: tool.schema.enum(["landed", "discarded", "abandoned", "unresolved"]),
          reason: tool.schema.string().min(1).max(2000),
          evidence: tool.schema.string().max(32000).optional(),
        },
        async execute(args, context) {
          await sessionRequest(conn, context.sessionID, "outcome", { outcome: args.outcome, reason: args.reason, ...(args.evidence ? { evidence: args.evidence } : {}) });
          return sessionRequest(conn, context.sessionID, "status");
        },
      }),
    },
    "chat.headers": async (input: { sessionID: string; model?: { providerID?: string } }, output: { headers: Record<string, string> }) => {
      if (input.model?.providerID !== "openrouter") return;
      output.headers["x-mimir-session"] = input.sessionID;
      output.headers["x-mimir-harness"] = "opencode";
      if (repo) output.headers["x-mimir-repo"] = repo;
    },
    event: async ({ event }: { event: { type: string; properties?: Record<string, unknown> } }) => {
      const properties = event.properties ?? {};
      if (event.type === "message.updated") {
        const turn = buildTurnEvent(properties.info, repo);
        const info = properties.info as Record<string, unknown> | undefined;
        if (typeof info?.sessionID === "string") activity.touch(info.sessionID);
        if (turn && typeof info?.id === "string") {
          delivery.deliver(turn);
          exchangeReporter.deliver(info);
        }
        return;
      }
      if (event.type === "session.created" || event.type === "session.updated") {
        const info = properties.info as Record<string, unknown> | undefined;
        if (typeof info?.id === "string") {
          activity.touch(info.id);
          const parentSessionID = typeof info.parentID === "string" && info.parentID !== info.id ? info.parentID : null;
          delivery.deliver({ version: 1, kind: "heartbeat", session_id: info.id, parent_session_id: parentSessionID, harness: "opencode", repo, ts: new Date().toISOString() });
        }
        return;
      }
      if (event.type === "session.deleted") {
        const info = properties.info as Record<string, unknown> | undefined;
        if (typeof info?.id === "string") {
          activity.clear(info.id);
          delivery.deliver({ version: 1, kind: "end", session_id: info.id, harness: "opencode", repo, ts: new Date().toISOString(), reason: "session deleted" });
        }
      }
    },
  };
};

export const MimirPlugin = server;
export default { id: "mimir", server };

// Test surface. The OpenCode plugin loader only invokes function exports, so
// this object is inert in production.
export const __testing = { parseMimirConfig, resolveConnection, buildTurnEvent, buildDirectExchange, normalizeParts, jsonSafe, repoName, createActivityTracker, createDeliveryQueue, createDirectExchangeReporter, postEvent, postDirectExchange, sessionRequest, buildHarnessLoad, loadHarnessLoad, postHarnessLoad, reportHarnessLoad };
