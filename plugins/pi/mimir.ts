// Mimir capture extension for Pi.
//
// The managed extension routes Pi's OpenRouter provider through Mimir and adds
// exact session metadata to every proxied request. Other Pi providers bypass
// the proxy, so their completed turns are uploaded as bounded reconstructed
// exchanges. Querying memory remains the responsibility of the installed
// mimir-use skill and the Mimir CLI.
//
// No credentials live in this file. Connection resolves from, in order:
//   1. MIMIR_URL + MIMIR_TOKEN environment variables
//   2. $MIMIR_HOME/config + $MIMIR_HOME/token
//   3. ~/.mimir/config + ~/.mimir/token

type ExtensionAPI = any;

import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { basename, join } from "node:path";
import { fileURLToPath } from "node:url";

const HEARTBEAT_MS = 60_000;
const MAX_STRING_BYTES = 64 * 1024;
const MAX_EXCHANGE_BYTES = 512 * 1024;
const MAX_JSON_DEPTH = 8;
const MAX_JSON_ENTRIES = 512;
const MAX_MESSAGES = 128;
const SESSION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;

type Connection = { url: string; token: string };
type RequestKind = "primary" | "summary" | "compaction";
type SessionState = { id: string; cwd: string; repo: string | null; gitRef: string | null; active: boolean };
type HarnessLoad = {
  version: 1;
  harness: "pi";
  source_sha256: string;
  bundle_version?: string;
  cli_version?: string;
  cli_commit?: string;
  installation_id?: string;
};
type SessionEvent = {
  version: 1;
  kind: "heartbeat" | "end";
  session_id: string;
  harness: "pi";
  repo?: string;
  title?: string;
  ts: string;
  reason?: string;
};
type Usage = { input?: number; output?: number; cacheRead?: number; cacheWrite?: number };
type AssistantMessage = {
  role?: string;
  content?: unknown;
  provider?: string;
  model?: string;
  usage?: Usage;
  stopReason?: string;
  errorMessage?: string;
  timestamp?: number;
};
type TurnSnapshot = { startedAt: number; request: { messages: unknown[] } };
type NormalizedToolActivity = { name: string; input: Record<string, unknown>; status: "succeeded" | "failed"; output?: string };

function parseMimirConfig(text: string): { url?: string } {
  const match = text.match(/^\s*url\s*=\s*"?([^"\n]+?)"?\s*$/m);
  return match ? { url: match[1]!.replace(/\/+$/, "") } : {};
}

function resolveConnection(
  env: Record<string, string | undefined>,
  readFile: (path: string) => string | null,
  home: string | undefined,
): Connection | null {
  const envUrl = env.MIMIR_URL?.trim();
  const envToken = env.MIMIR_TOKEN?.trim();
  if (envUrl && envToken) return { url: envUrl.replace(/\/+$/, ""), token: envToken };
  const directory = env.MIMIR_HOME?.trim() || (home ? join(home, ".mimir") : null);
  if (!directory) return null;
  const config = readFile(join(directory, "config"));
  const token = readFile(join(directory, "token"))?.trim();
  const url = config ? parseMimirConfig(config).url : undefined;
  return url && token ? { url, token } : null;
}

function readLocalFile(path: string): string | null {
  try {
    return existsSync(path) ? readFileSync(path, "utf8") : null;
  } catch {
    return null;
  }
}

function loadConnection(): Connection | null {
  let home: string | undefined;
  try { home = homedir(); } catch { home = undefined; }
  return resolveConnection(process.env, readLocalFile, home);
}

function buildHarnessLoad(source: string, receiptText: string | null): HarnessLoad {
  const load: HarnessLoad = {
    version: 1,
    harness: "pi",
    source_sha256: createHash("sha256").update(source).digest("hex"),
  };
  if (!receiptText) return load;
  try {
    const receipt = JSON.parse(receiptText) as Record<string, unknown>;
    const cli = receipt.cli && typeof receipt.cli === "object" ? receipt.cli as Record<string, unknown> : {};
    if (typeof receipt.bundle_version === "string" && receipt.bundle_version) load.bundle_version = receipt.bundle_version;
    if (typeof cli.version === "string" && cli.version) load.cli_version = cli.version;
    if (typeof cli.commit === "string" && cli.commit) load.cli_commit = cli.commit;
    if (typeof receipt.installation_id === "string" && receipt.installation_id) load.installation_id = receipt.installation_id;
  } catch {
    // Source identity remains useful when an old or partial receipt is present.
  }
  return load;
}

function loadHarnessLoad(sourcePath = fileURLToPath(import.meta.url)): HarnessLoad | null {
  const source = readLocalFile(sourcePath);
  if (source === null) return null;
  let home: string | undefined;
  try { home = homedir(); } catch { home = undefined; }
  const directory = process.env.MIMIR_HOME?.trim() || (home ? join(home, ".mimir") : null);
  const receipt = directory ? readLocalFile(join(directory, "install-receipt.json")) : null;
  return buildHarnessLoad(source, receipt);
}

function canonicalSessionID(value: string): string {
  if (SESSION_ID.test(value)) return value;
  return `pi-${createHash("sha256").update(value).digest("hex").slice(0, 32)}`;
}

function boundedString(value: string): string {
  const encoder = new TextEncoder();
  if (encoder.encode(value).byteLength <= MAX_STRING_BYTES) return value;
  let low = 0, high = value.length;
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
    if (safe !== undefined) result[boundedString(key)] = safe;
    if (budget.remaining <= 0) break;
  }
  seen.delete(value);
  return result;
}

function normalizeMessages(messages: unknown): unknown[] {
  if (!Array.isArray(messages)) return [];
  return messages.slice(-MAX_MESSAGES).flatMap((message) => {
    const safe = jsonSafe(message);
    return safe && typeof safe === "object" ? [safe] : [];
  });
}

function normalizeToolActivity(rawMessage: unknown, rawToolResults: unknown): NormalizedToolActivity[] {
  const message = rawMessage && typeof rawMessage === "object" ? rawMessage as Record<string, unknown> : {};
  const blocks = Array.isArray(message.content) ? message.content : [];
  const calls: Array<{ id: string | null; name: string; input: Record<string, unknown> }> = [];
  for (const block of blocks) {
    if (!block || typeof block !== "object") continue;
    const value = block as Record<string, unknown>;
    if (value.type !== "toolCall" && value.type !== "tool_use") continue;
    const name = typeof value.name === "string" ? value.name : typeof value.toolName === "string" ? value.toolName : "";
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(name)) continue;
    let input: unknown = value.arguments ?? value.input ?? {};
    if (typeof input === "string") {
      try {
        input = JSON.parse(input);
      } catch {
        input = {};
      }
    }
    const safeInput = jsonSafe(input);
    calls.push({
      id: typeof value.id === "string" ? value.id : typeof value.toolCallId === "string" ? value.toolCallId : null,
      name,
      input: safeInput && typeof safeInput === "object" && !Array.isArray(safeInput) ? safeInput as Record<string, unknown> : {},
    });
  }
  const results = normalizeMessages(rawToolResults).filter((value): value is Record<string, unknown> => !!value && typeof value === "object" && !Array.isArray(value));
  const consumed = new Set<number>();
  const activities = calls.map((call) => {
    const resultIndex = results.findIndex((result, index) => !consumed.has(index) && (
      call.id && (result.toolCallId === call.id || result.tool_call_id === call.id)
      || result.toolName === call.name
      || result.name === call.name
    ));
    const result = resultIndex >= 0 ? results[resultIndex] : null;
    if (resultIndex >= 0) consumed.add(resultIndex);
    const failed = result?.isError === true || result?.is_error === true || result?.status === "error" || typeof result?.exitCode === "number" && result.exitCode !== 0;
    const content = result?.content;
    const output = content === undefined ? undefined : boundedString(typeof content === "string" ? content : JSON.stringify(content));
    return { name: call.name, input: call.input, status: failed ? "failed" as const : "succeeded" as const, ...(output ? { output } : {}) };
  });
  for (const [index, result] of results.entries()) {
    if (consumed.has(index)) continue;
    const name = typeof result.toolName === "string" ? result.toolName : typeof result.name === "string" ? result.name : "";
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(name)) continue;
    const failed = result.isError === true || result.is_error === true || result.status === "error" || typeof result.exitCode === "number" && result.exitCode !== 0;
    const content = result.content;
    const output = content === undefined ? undefined : boundedString(typeof content === "string" ? content : JSON.stringify(content));
    activities.push({ name, input: {}, status: failed ? "failed" : "succeeded", ...(output ? { output } : {}) });
  }
  return activities;
}

function byteLength(value: unknown): number {
  return new TextEncoder().encode(JSON.stringify(value)).byteLength;
}

function buildExchange(sessionID: string, turnIndex: number, snapshot: TurnSnapshot | undefined, rawMessage: unknown, rawToolResults: unknown = [], title?: string) {
  const message = rawMessage && typeof rawMessage === "object" ? rawMessage as AssistantMessage : {};
  if (message.role !== "assistant" || !message.provider || message.provider === "openrouter" || !message.model) return null;
  const timestamp = typeof message.timestamp === "number" && Number.isFinite(message.timestamp) ? message.timestamp : Date.now();
  const safeMessage = jsonSafe(message);
  if (!safeMessage) return null;
  const response = {
    message: safeMessage,
    tool_results: normalizeMessages(rawToolResults),
  };
  const request = { messages: [...(snapshot?.request.messages ?? [])] };
  const usage = message.usage ?? {};
  const payload: Record<string, unknown> = {
    exchange_id: `pi:${createHash("sha256").update(`${sessionID}\0${timestamp}\0${turnIndex}`).digest("hex").slice(0, 40)}`,
    ts: new Date(timestamp).toISOString(),
    provider: message.provider.slice(0, 256),
    model: message.model.slice(0, 256),
    request_kind: "primary",
    request,
    response,
    tool_activity: normalizeToolActivity(rawMessage, rawToolResults),
    usage: {
      input_tokens: Math.max(0, Math.floor(usage.input ?? 0)),
      output_tokens: Math.max(0, Math.floor(usage.output ?? 0)),
      cache_read_tokens: Math.max(0, Math.floor(usage.cacheRead ?? 0)),
      cache_write_tokens: Math.max(0, Math.floor(usage.cacheWrite ?? 0)),
    },
    latency_ms: Math.max(0, Math.floor(Date.now() - (snapshot?.startedAt ?? timestamp))),
  };
  if (title?.trim()) payload.title = title.trim().slice(0, 200);
  while (byteLength(payload) > MAX_EXCHANGE_BYTES && request.messages.length > 1) request.messages.shift();
  if (byteLength(payload) > MAX_EXCHANGE_BYTES) return null;
  return payload;
}

async function request(connection: Connection, path: string, body: unknown, metadata: Record<string, string> = {}): Promise<boolean> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 10_000);
  try {
    const response = await fetch(`${connection.url}${path}`, {
      method: "POST",
      headers: { authorization: `Bearer ${connection.token}`, "content-type": "application/json", ...metadata },
      body: JSON.stringify(body),
      signal: controller.signal,
    });
    return response.ok;
  } catch {
    return false;
  } finally {
    clearTimeout(timeout);
  }
}

function createDeliveryQueue(
  send: (path: string, body: unknown, metadata: Record<string, string>) => Promise<boolean>,
  schedule: (callback: () => void, delay: number) => unknown = setTimeout,
  maxAttempts = 4,
) {
  const pending = new Map<string, { path: string; body: unknown; metadata: Record<string, string>; attempts: number }>();
  const attempt = async (key: string) => {
    const item = pending.get(key);
    if (!item) return;
    item.attempts++;
    if (await send(item.path, item.body, item.metadata)) {
      pending.delete(key);
      return;
    }
    if (item.attempts >= maxAttempts) {
      pending.delete(key);
      return;
    }
    const timer = schedule(() => { void attempt(key); }, 250 * (2 ** (item.attempts - 1)));
    (timer as { unref?: () => void }).unref?.();
  };
  return {
    deliver(key: string, path: string, body: unknown, metadata: Record<string, string> = {}): void {
      if (pending.has(key)) return;
      pending.set(key, { path, body, metadata, attempts: 0 });
      void attempt(key);
    },
    pending: () => pending.size,
  };
}

async function gitMetadata(pi: ExtensionAPI, cwd: string): Promise<{ repo: string | null; gitRef: string | null }> {
  let repo = basename(cwd) || null;
  let gitRef: string | null = null;
  try {
    const root = await pi.exec("git", ["-C", cwd, "rev-parse", "--show-toplevel"], { timeout: 5_000 });
    if (root.code === 0 && root.stdout.trim()) repo = basename(root.stdout.trim());
    const branch = await pi.exec("git", ["-C", cwd, "rev-parse", "--abbrev-ref", "HEAD"], { timeout: 5_000 });
    if (branch.code === 0 && branch.stdout.trim() && branch.stdout.trim() !== "HEAD") gitRef = branch.stdout.trim().slice(0, 512);
  } catch {
    // Repository metadata is optional.
  }
  return { repo, gitRef };
}

export default function (pi: ExtensionAPI) {
  const connection = loadConnection();
  if (!connection) return;

  pi.registerProvider("openrouter", {
    baseUrl: `${connection.url}/v1`,
    apiKey: connection.token,
  });

  const delivery = createDeliveryQueue((path, body, metadata) => request(connection, path, body, metadata));
  const snapshots = new Map<number, TurnSnapshot>();
  let session: SessionState | null = null;
  let heartbeat: ReturnType<typeof setInterval> | undefined;
  let nextRequestKind: RequestKind = "primary";
  let initialization = 0;

  const captureHeaders = (current: SessionState): Record<string, string> => ({
    "x-mimir-harness": "pi",
    ...(current.repo ? { "x-mimir-repo": current.repo } : {}),
    ...(current.gitRef ? { "x-mimir-git-ref": current.gitRef } : {}),
  });

  const eventBody = (current: SessionState, kind: "heartbeat" | "end", reason?: string): SessionEvent => {
    const body: SessionEvent = {
      version: 1,
      kind,
      session_id: current.id,
      harness: "pi",
      ts: new Date().toISOString(),
    };
    if (current.repo) body.repo = current.repo;
    const title = reason === "switch" ? undefined : pi.getSessionName()?.trim();
    if (title) body.title = title.slice(0, 200);
    if (reason) body.reason = reason.slice(0, 2_000);
    return body;
  };

  const sendHeartbeat = () => {
    const current = session;
    if (!current?.active) return;
    const body = eventBody(current, "heartbeat");
    delivery.deliver(`heartbeat:${current.id}:${body.ts}`, `/sessions/${encodeURIComponent(current.id)}/events`, body);
  };

  const scheduleHeartbeat = () => {
    clearInterval(heartbeat);
    heartbeat = setInterval(sendHeartbeat, HEARTBEAT_MS);
    (heartbeat as { unref?: () => void }).unref?.();
  };

  const activate = () => {
    if (!session || session.active) return;
    session.active = true;
    sendHeartbeat();
    scheduleHeartbeat();
  };

  const initialize = async (_event: unknown, ctx: { cwd?: string; sessionManager?: { getSessionId?: () => string } }) => {
    const generation = ++initialization;
    clearInterval(heartbeat);
    heartbeat = undefined;
    snapshots.clear();
    const rawID = ctx?.sessionManager?.getSessionId?.();
    if (!rawID) return;
    const cwd = ctx.cwd || process.cwd();
    const previous = session;
    const id = canonicalSessionID(String(rawID));
    const candidate: SessionState = {
      id,
      cwd,
      repo: basename(cwd) || null,
      gitRef: null,
      active: previous?.id === id && previous.active,
    };
    Object.assign(candidate, await gitMetadata(pi, cwd));
    if (generation !== initialization) return;
    if (previous?.active && previous.id !== candidate.id) {
      await request(
        connection,
        `/sessions/${encodeURIComponent(previous.id)}/events`,
        eventBody(previous, "end", "switch"),
        captureHeaders(previous),
      );
      if (generation !== initialization) return;
    }
    session = candidate;
    nextRequestKind = "primary";
    const load = loadHarnessLoad();
    if (load) delivery.deliver(`load:${load.source_sha256}`, "/integrations/harness-loads", load);
    if (candidate.active) {
      sendHeartbeat();
      scheduleHeartbeat();
    }
  };

  pi.on("session_start", initialize);

  pi.on("before_provider_headers", (event, ctx) => {
    if (!session || ctx.model?.provider !== "openrouter") return;
    event.headers["x-mimir-session"] = session.id;
    event.headers["x-mimir-harness"] = "pi";
    event.headers["x-mimir-request-kind"] = nextRequestKind;
    if (session.repo) event.headers["x-mimir-repo"] = session.repo;
    if (session.gitRef) event.headers["x-mimir-git-ref"] = session.gitRef;
    if (nextRequestKind !== "primary") nextRequestKind = "primary";
  });

  pi.on("session_before_compact", () => { nextRequestKind = "compaction"; });
  pi.on("session_compact", () => { nextRequestKind = "primary"; });
  pi.on("session_before_tree", () => { nextRequestKind = "summary"; });
  pi.on("session_tree", () => { nextRequestKind = "primary"; });

  pi.on("turn_start", (event, ctx) => {
    activate();
    const context = ctx.sessionManager.buildSessionContext();
    snapshots.set(event.turnIndex, { startedAt: event.timestamp, request: { messages: normalizeMessages(context.messages) } });
  });

  pi.on("turn_end", (event) => {
    const current = session;
    if (!current) return;
    const snapshot = snapshots.get(event.turnIndex);
    snapshots.delete(event.turnIndex);
    const exchange = buildExchange(current.id, event.turnIndex, snapshot, event.message, event.toolResults, pi.getSessionName());
    if (!exchange) return;
    const exchangeID = String(exchange.exchange_id);
    delivery.deliver(`exchange:${exchangeID}`, `/sessions/${encodeURIComponent(current.id)}/exchanges`, exchange, captureHeaders(current));
  });

  pi.on("session_info_changed", () => {
    const current = session;
    if (!current?.active) return;
    const body = eventBody(current, "heartbeat");
    delivery.deliver(`title:${current.id}:${body.ts}`, `/sessions/${encodeURIComponent(current.id)}/events`, body);
  });

  pi.on("session_shutdown", async (event) => {
    initialization++;
    clearInterval(heartbeat);
    heartbeat = undefined;
    snapshots.clear();
    const current = session;
    const reason = typeof event?.reason === "string" ? event.reason : "shutdown";
    if (current?.active && reason !== "reload") {
      await request(
        connection,
        `/sessions/${encodeURIComponent(current.id)}/events`,
        eventBody(current, "end", reason),
        captureHeaders(current),
      );
    }
    session = null;
  });
}

export const __testing = {
  parseMimirConfig,
  resolveConnection,
  buildHarnessLoad,
  canonicalSessionID,
  boundedString,
  jsonSafe,
  normalizeMessages,
  normalizeToolActivity,
  buildExchange,
  createDeliveryQueue,
};
