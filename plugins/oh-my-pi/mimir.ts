// Mimir capture extension for Oh My Pi.
type ExtensionHandler = (...args: never[]) => unknown;
type ExtensionAPI = {
  on(event: string, handler: ExtensionHandler): void;
  registerProvider(name: string, provider: { baseUrl: string; apiKey: string; headers: Record<string, string> }): void;
  getSessionName(): unknown;
  exec(command: string, args: string[], options: { timeout: number }): Promise<{ code: number; stdout: string }>;
};
type SessionContext = {
  cwd?: string;
  sessionManager?: {
    getSessionId?: () => unknown;
    buildSessionContext?: () => { messages?: unknown };
  };
};
type TurnStart = { turnIndex: number; timestamp: number };
type TurnEnd = {
  turnIndex: number;
  message?: {
    role?: unknown;
    provider?: unknown;
    model?: unknown;
    timestamp?: unknown;
    usage?: { input?: unknown; cacheRead?: unknown; output?: unknown };
  };
  toolResults?: unknown;
};
type SessionShutdown = { reason?: unknown };

import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { basename, join } from "node:path";
import { fileURLToPath } from "node:url";

const HEARTBEAT_MS = 60_000;
const MAX_EXCHANGE_BYTES = 512 * 1024;
const SESSION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
type Connection = { url: string; token: string };
type Session = { id: string; cwd: string; repo: string | null; gitRef: string | null; active: boolean };
type RequestKind = "primary" | "summary" | "compaction";

function read(path: string): string | null {
  try { return existsSync(path) ? readFileSync(path, "utf8") : null; } catch { return null; }
}

function connection(): Connection | null {
  const envURL = process.env.MIMIR_URL?.trim().replace(/\/+$/, "");
  const envToken = process.env.MIMIR_TOKEN?.trim();
  if (envURL && envToken) return { url: envURL, token: envToken };
  let home: string;
  try { home = homedir(); } catch { return null; }
  const directory = process.env.MIMIR_HOME?.trim() || join(home, ".mimir");
  const config = read(join(directory, "config"));
  const token = read(join(directory, "token"))?.trim();
  const url = config?.match(/^\s*url\s*=\s*"?([^"\n]+?)"?\s*$/m)?.[1]?.replace(/\/+$/, "");
  return url && token ? { url, token } : null;
}

function sessionID(value: string): string {
  return SESSION_ID.test(value) ? value : `oh-my-pi-${createHash("sha256").update(value).digest("hex").slice(0, 32)}`;
}

function safe(value: unknown, depth = 0, seen = new WeakSet<object>()): unknown {
  if (value === null || typeof value === "boolean" || typeof value === "string") return typeof value === "string" ? value.slice(0, 64 * 1024) : value;
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (typeof value !== "object" || depth >= 8 || seen.has(value)) return undefined;
  seen.add(value);
  if (Array.isArray(value)) return value.slice(-128).flatMap((item) => { const result = safe(item, depth + 1, seen); return result === undefined ? [] : [result]; });
  const result: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value).slice(0, 512)) {
    const normalized = safe(item, depth + 1, seen);
    if (normalized !== undefined) result[key] = normalized;
  }
  return result;
}

async function post(config: Connection, path: string, body: unknown, metadata: Record<string, string> = {}): Promise<boolean> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 10_000);
  try {
    const response = await fetch(`${config.url}${path}`, { method: "POST", headers: { authorization: `Bearer ${config.token}`, "content-type": "application/json", ...metadata }, body: JSON.stringify(body), signal: controller.signal });
    return response.ok;
  } catch { return false; } finally { clearTimeout(timeout); }
}

function deliveryQueue(config: Connection) {
  const pending = new Map<string, { path: string; body: unknown; metadata: Record<string, string>; attempts: number }>();
  const attempt = async (key: string) => {
    const item = pending.get(key);
    if (!item) return;
    item.attempts++;
    if (await post(config, item.path, item.body, item.metadata) || item.attempts >= 4) { pending.delete(key); return; }
    const timer = setTimeout(() => { void attempt(key); }, 250 * 2 ** (item.attempts - 1));
    timer.unref?.();
  };
  return (key: string, path: string, body: unknown, metadata: Record<string, string> = {}) => {
    if (pending.has(key)) return;
    pending.set(key, { path, body, metadata, attempts: 0 });
    void attempt(key);
  };
}

async function gitMetadata(pi: ExtensionAPI, cwd: string) {
  let repo: string | null = basename(cwd) || null;
  let gitRef: string | null = null;
  try {
    const root = await pi.exec("git", ["-C", cwd, "rev-parse", "--show-toplevel"], { timeout: 5_000 });
    if (root.code === 0 && root.stdout.trim()) repo = basename(root.stdout.trim());
    const branch = await pi.exec("git", ["-C", cwd, "rev-parse", "--abbrev-ref", "HEAD"], { timeout: 5_000 });
    if (branch.code === 0 && branch.stdout.trim() !== "HEAD") gitRef = branch.stdout.trim().slice(0, 512);
  } catch { /* metadata is optional */ }
  return { repo, gitRef };
}
function sessionTitle(pi: ExtensionAPI): string | undefined {
  const value = pi.getSessionName();
  return typeof value === "string" ? value.trim().slice(0, 200) || undefined : undefined;
}

export default function (pi: ExtensionAPI) {
  const config = connection();
  if (!config) return;
  const deliver = deliveryQueue(config);
  const snapshots = new Map<number, { startedAt: number; messages: unknown[] }>();
  let current: Session | null = null;
  let heartbeat: ReturnType<typeof setInterval> | undefined;
  let requestKind: RequestKind = "primary";
  let initialization = 0;

  const headersFor = (session: Session) => ({
    "x-mimir-session": session.id,
    "x-mimir-harness": "oh-my-pi",
    "x-mimir-request-kind": requestKind,
    ...(session.repo ? { "x-mimir-repo": session.repo } : {}),
    ...(session.gitRef ? { "x-mimir-git-ref": session.gitRef } : {}),
  });
  const headers = () => current ? headersFor(current) : { "x-mimir-harness": "oh-my-pi" };

  const configureProvider = () => pi.registerProvider("openrouter", { baseUrl: `${config.url}/v1`, apiKey: config.token, headers: headers() });
  configureProvider();

  const event = (session: Session, kind: "heartbeat" | "end", reason?: string) => ({
    version: 1, kind, session_id: session.id, harness: "oh-my-pi", repo: session.repo ?? undefined,
    title: reason === "switch" ? undefined : sessionTitle(pi), ts: new Date().toISOString(), reason,
  });

  const sendHeartbeat = () => {
    if (!current?.active) return;
    const body = event(current, "heartbeat");
    deliver(`heartbeat:${current.id}:${body.ts}`, `/sessions/${encodeURIComponent(current.id)}/events`, body);
  };
  const activate = () => {
    if (!current || current.active) return;
    current.active = true;
    sendHeartbeat();
    heartbeat = setInterval(sendHeartbeat, HEARTBEAT_MS);
    heartbeat.unref?.();
  };

  const initialize = async (_event: unknown, ctx: SessionContext) => {
    const generation = ++initialization;
    clearInterval(heartbeat);
    heartbeat = undefined;
    snapshots.clear();
    const cwd = ctx?.cwd || process.cwd();
    const rawID = ctx?.sessionManager?.getSessionId?.();
    if (!rawID) return;
    const previous = current;
    const id = sessionID(String(rawID));
    const candidate: Session = {
      id, cwd, repo: basename(cwd) || null, gitRef: null,
      active: previous?.id === id && previous.active,
    };
    Object.assign(candidate, await gitMetadata(pi, cwd));
    if (generation !== initialization) return;
    if (previous?.active && previous.id !== candidate.id) {
      await post(config, `/sessions/${encodeURIComponent(previous.id)}/events`, event(previous, "end", "switch"), headersFor(previous));
      if (generation !== initialization) return;
    }
    current = candidate;
    requestKind = "primary";
    configureProvider();
    const source = read(fileURLToPath(import.meta.url));
    if (source) {
      const receipt = read(join(process.env.MIMIR_HOME?.trim() || join(homedir(), ".mimir"), "install-receipt.json"));
      let installation_id: string | undefined;
      try { installation_id = JSON.parse(receipt || "{}").installation_id; } catch { /* optional */ }
      const load = { version: 1, harness: "oh-my-pi", source_sha256: createHash("sha256").update(source).digest("hex"), installation_id };
      deliver(`load:${load.source_sha256}`, "/integrations/harness-loads", load);
    }
    if (current.active) {
      sendHeartbeat();
      heartbeat = setInterval(sendHeartbeat, HEARTBEAT_MS);
      heartbeat.unref?.();
    }
  };

  pi.on("session_start", initialize);
  pi.on("session_switch", initialize);
  pi.on("session_branch", initialize);
  pi.on("session_before_compact", () => { requestKind = "compaction"; configureProvider(); });
  pi.on("session_compact", () => { requestKind = "primary"; configureProvider(); });
  pi.on("session_before_tree", () => { requestKind = "summary"; configureProvider(); });
  pi.on("session_tree", () => { requestKind = "primary"; configureProvider(); });

  pi.on("turn_start", (turn: TurnStart, ctx: SessionContext) => {
    activate();
    const messages = ctx.sessionManager?.buildSessionContext?.().messages;
    snapshots.set(turn.turnIndex, { startedAt: turn.timestamp, messages: Array.isArray(messages) ? messages.slice(-128) : [] });
  });

  pi.on("turn_end", (turn: TurnEnd) => {
    const snapshot = snapshots.get(turn.turnIndex);
    snapshots.delete(turn.turnIndex);
    if (!current || turn.message?.role !== "assistant" || typeof turn.message.provider !== "string" || turn.message.provider === "openrouter" || typeof turn.message.model !== "string") return;
    const timestamp = Number(turn.message.timestamp) || Date.now();
    const payload: Record<string, unknown> = {
      exchange_id: `oh-my-pi:${createHash("sha256").update(`${current.id}\0${timestamp}\0${turn.turnIndex}`).digest("hex").slice(0, 40)}`,
      ts: new Date(timestamp).toISOString(), provider: turn.message.provider, model: turn.message.model, request_kind: "primary",
      request: { messages: safe(snapshot?.messages ?? []) }, response: { message: safe(turn.message), tool_results: safe(turn.toolResults ?? []) },
      usage: { input_tokens: Math.max(0, Number(turn.message.usage?.input || 0) + Number(turn.message.usage?.cacheRead || 0)), output_tokens: Math.max(0, Number(turn.message.usage?.output || 0)) },
      latency_ms: Math.max(0, Date.now() - (snapshot?.startedAt ?? timestamp)), title: sessionTitle(pi),
    };
    if (new TextEncoder().encode(JSON.stringify(payload)).byteLength > MAX_EXCHANGE_BYTES) return;
    deliver(`exchange:${payload.exchange_id}`, `/sessions/${encodeURIComponent(current.id)}/exchanges`, payload, headers());
  });

  pi.on("session_shutdown", async (shutdown: SessionShutdown) => {
    initialization++;
    clearInterval(heartbeat);
    heartbeat = undefined;
    const reason = typeof shutdown?.reason === "string" ? shutdown.reason : "shutdown";
    const session = current;
    if (reason !== "reload" && session?.active) {
      await post(config, `/sessions/${encodeURIComponent(session.id)}/events`, event(session, "end", reason), headersFor(session));
    }
    current = null;
    snapshots.clear();
  });
}

export const __testing = { sessionID, safe };
