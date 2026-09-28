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
  /** Exact spawning session identity, not fork/transcript lineage. */
  parentSessionId?: string | null;
  sessionManager?: {
    getSessionId?: () => unknown;
    getSessionFile?: () => unknown;
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
    content?: unknown;
  };
  toolResults?: unknown;
};
type SessionShutdown = { reason?: unknown };

import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { closeSync, existsSync, openSync, readFileSync, readSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const HEARTBEAT_MS = 60_000;
const MAX_EXCHANGE_BYTES = 512 * 1024;
const MAX_PATCH_BYTES = 4 * 1024 * 1024;
const gitExec = promisify(execFile);
const COMMIT_SHA = /^[0-9a-f]{40}$/;
const SESSION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
type Connection = { url: string; token: string };
type Session = { id: string; parentId: string | null; cwd: string; repo: string | null; gitRef: string | null; active: boolean };
type RequestKind = "primary" | "summary" | "compaction";
type NormalizedToolActivity = { name: string; input: Record<string, unknown>; status: "succeeded" | "failed"; output?: string };

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

function sessionHeaderID(path: string): string | null {
  let file: number | undefined;
  try {
    file = openSync(path, "r");
    const bytes = Buffer.allocUnsafe(64 * 1024);
    const count = readSync(file, bytes, 0, bytes.length, 0);
    for (const line of bytes.subarray(0, count).toString("utf8").split(/\r?\n/, 2)) {
      const value = JSON.parse(line) as { type?: unknown; id?: unknown };
      if (value.type === "session" && typeof value.id === "string" && value.id.trim()) return value.id;
    }
  } catch { /* missing or malformed local history is not parent evidence */ }
  finally { if (file !== undefined) try { closeSync(file); } catch { /* best effort */ } }
  return null;
}

function artifactParentSessionID(sessionFile: unknown): string | null {
  if (typeof sessionFile !== "string" || !sessionFile.endsWith(".jsonl")) return null;
  const directory = dirname(sessionFile);
  const name = basename(sessionFile, ".jsonl");
  const separator = name.lastIndexOf(".");
  const parentFile = separator >= 0
    ? join(directory, `${name.slice(0, separator)}.jsonl`)
    : `${directory}.jsonl`;
  return parentFile === sessionFile ? null : sessionHeaderID(parentFile);
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
      try { input = JSON.parse(input); } catch { input = {}; }
    }
    const normalized = safe(input);
    calls.push({
      id: typeof value.id === "string" ? value.id : typeof value.toolCallId === "string" ? value.toolCallId : null,
      name,
      input: normalized && typeof normalized === "object" && !Array.isArray(normalized) ? normalized as Record<string, unknown> : {},
    });
  }
  const normalizedResults = safe(rawToolResults);
  const results = Array.isArray(normalizedResults) ? normalizedResults.filter((value): value is Record<string, unknown> => !!value && typeof value === "object" && !Array.isArray(value)) : [];
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
    const output = content === undefined ? undefined : (typeof content === "string" ? content : JSON.stringify(content)).slice(0, 64 * 1024);
    return { name: call.name, input: call.input, status: failed ? "failed" as const : "succeeded" as const, ...(output ? { output } : {}) };
  });
  for (const [index, result] of results.entries()) {
    if (consumed.has(index)) continue;
    const name = typeof result.toolName === "string" ? result.toolName : typeof result.name === "string" ? result.name : "";
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(name)) continue;
    const failed = result.isError === true || result.is_error === true || result.status === "error" || typeof result.exitCode === "number" && result.exitCode !== 0;
    const content = result.content;
    const output = content === undefined ? undefined : (typeof content === "string" ? content : JSON.stringify(content)).slice(0, 64 * 1024);
    activities.push({ name, input: {}, status: failed ? "failed" : "succeeded", ...(output ? { output } : {}) });
  }
  return activities;
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

// Use a subprocess buffer limit: the host's exec API does not promise bounded stdout.
async function git(cwd: string, args: string[], maxBuffer = 64 * 1024): Promise<string | null> {
  try {
    const { stdout } = await gitExec("git", ["-C", cwd, ...args], { timeout: 5_000, maxBuffer, encoding: "utf8" });
    return stdout;
  } catch { return null; }
}

async function head(cwd: string): Promise<string | null> {
  const value = (await git(cwd, ["rev-parse", "--verify", "HEAD"]))?.trim();
  return value && COMMIT_SHA.test(value) ? value : null;
}

function commitHints(message: unknown, results: unknown): string[] {
  const activities = normalizeToolActivity(message, results);
  const hints = new Set<string>();
  for (const activity of activities) {
    if (activity.status !== "succeeded" || !activity.output || !/(?:^|[^\w])git\s+(?:(?:-[Cc]\s+\S+|--git-dir(?:=|\s+)\S+)\s+)*commit(?:\s|$)/i.test(JSON.stringify(activity.input))) continue;
    // Standard git commit output identifies the actual commit (not arbitrary hashes in a tool result).
    for (const match of activity.output.matchAll(/\[[^\]\r\n]+\s([0-9a-f]{7,40})\](?=\s|\\n|"|$)/g)) hints.add(match[1]);
  }
  return [...hints];
}

function redactPatch(patch: string): string {
  return patch
    .replace(/-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----[\s\S]*?-----END (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----/g, "[REDACTED PRIVATE KEY]")
    .replace(/\b(Bearer\s+)[^\s"']+/gi, "$1[REDACTED]")
    .replace(/\b((?:api[_-]?key|access[_-]?token|secret|password)\s*[:=]\s*["']?)[^\s"']+/gi, "$1[REDACTED]");
}

async function collectCommits(cwd: string, baseline: string, hints: string[]) {
  if (!hints.length) return [];
  const tip = await head(cwd);
  if (!tip || tip === baseline || (await git(cwd, ["merge-base", "--is-ancestor", baseline, tip])) === null) return [];
  const range = await git(cwd, ["rev-list", "--reverse", "--max-count=32", `${baseline}..${tip}`]);
  if (!range) return [];
  const commits = range.trim().split("\n").filter((sha) => COMMIT_SHA.test(sha));
  if (commits.length > 16) return [];
  const artifacts = [];
  for (const sha of commits) {
    if (!hints.some((hint) => sha.startsWith(hint) && commits.filter((candidate) => candidate.startsWith(hint)).length === 1)) continue;
    const details = await git(cwd, ["show", "-s", "--format=%P%n%cI%n%s", sha]);
    const lines = details?.trimEnd().split("\n");
    if (!lines || lines.length < 3) continue;
    const [parents, committedAt, ...subjectParts] = lines;
    const subject = subjectParts.join(" ");
    const patch = await git(cwd, ["-c", "diff.external=", "show", "--format=", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", sha, "--"], MAX_PATCH_BYTES + 1);
    if (!patch || Buffer.byteLength(patch, "utf8") > MAX_PATCH_BYTES) continue;
    const cleaned = redactPatch(patch);
    if (!cleaned || Buffer.byteLength(cleaned, "utf8") > MAX_PATCH_BYTES) continue;
    const committed = new Date(committedAt);
    if (Number.isNaN(committed.getTime())) continue;
    artifacts.push({ commit_sha: sha, parent_commit_sha: parents.split(" ")[0] || null, committed_at: committed.toISOString(), subject: redactPatch(subject).slice(0, 500).replace(/[\p{Cc}]/gu, " "), repository_url: null, ref: null, provenance: "oh-my-pi", patch: cleaned });
  }
  return artifacts;
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
  const snapshots = new Map<number, { startedAt: number; messages: unknown[]; session: Session; baseline: string | null }>();
  const artifactJobs = new Set<Promise<void>>();
  const flushArtifacts = async () => { await Promise.all([...artifactJobs]); };
  const sendArtifacts = async (session: Session, commits: Awaited<ReturnType<typeof collectCommits>>) => {
    await Promise.all(commits.map(async (commit) => {
        for (let attempt = 0; attempt < 4; attempt++) {
          if (await post(config, `/sessions/${encodeURIComponent(session.id)}/git-artifacts`, { version: 1, commits: [commit] }, headersFor(session))) return;
          await new Promise<void>((resolve) => setTimeout(resolve, 250 * 2 ** attempt));
        }
    }));
  };
  let current: Session | null = null;
  let heartbeat: ReturnType<typeof setInterval> | undefined;
  let requestKind: RequestKind = "primary";
  let initialization = 0;
  const inheritedSessionID = process.env.MIMIR_SESSION_ID;
  let exportedSessionID: string | null = null;

  const exposeSessionID = (id: string) => {
    process.env.MIMIR_SESSION_ID = id;
    exportedSessionID = id;
  };
  const restoreSessionID = () => {
    if (!exportedSessionID || process.env.MIMIR_SESSION_ID !== exportedSessionID) return;
    if (inheritedSessionID === undefined) delete process.env.MIMIR_SESSION_ID;
    else process.env.MIMIR_SESSION_ID = inheritedSessionID;
    exportedSessionID = null;
  };

  const headersFor = (session: Session) => ({
    "x-mimir-session": session.id,
    "x-mimir-harness": "oh-my-pi",
    "x-mimir-request-kind": requestKind,
    ...(session.repo ? { "x-mimir-repo": session.repo } : {}),
    ...(session.gitRef ? { "x-mimir-git-ref": session.gitRef } : {}),
  });
  const headers = () => current ? headersFor(current) : { "x-mimir-harness": "oh-my-pi" };

  const configureProvider = () => pi.registerProvider("openrouter", { baseUrl: config.url, apiKey: config.token, headers: headers() });
  configureProvider();

  const event = (session: Session, kind: "heartbeat" | "end", reason?: string) => ({
    version: 1, kind, session_id: session.id, parent_session_id: session.parentId, harness: "oh-my-pi", repo: session.repo ?? undefined,
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
    await flushArtifacts();
    snapshots.clear();
    if (generation !== initialization) return;
    const cwd = ctx?.cwd || process.cwd();
    const rawID = ctx?.sessionManager?.getSessionId?.();
    if (!rawID) return;
    const previous = current;
    const id = sessionID(String(rawID));
    const rawParentId = Object.prototype.hasOwnProperty.call(ctx, "parentSessionId")
      ? ctx.parentSessionId
      : artifactParentSessionID(ctx.sessionManager?.getSessionFile?.());
    const parentId = typeof rawParentId === "string" && rawParentId.trim()
      ? sessionID(rawParentId) : null;
    const candidate: Session = {
      id, parentId: parentId !== id ? parentId : null, cwd, repo: basename(cwd) || null, gitRef: null,
      active: previous?.id === id && previous.active,
    };
    Object.assign(candidate, await gitMetadata(pi, cwd));
    if (generation !== initialization) return;
    if (previous?.active && previous.id !== candidate.id) {
      await post(config, `/sessions/${encodeURIComponent(previous.id)}/events`, event(previous, "end", "switch"), headersFor(previous));
      if (generation !== initialization) return;
    }
    current = candidate;
    exposeSessionID(candidate.id);
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

  pi.on("turn_start", async (turn: TurnStart, ctx: SessionContext) => {
    activate();
    const session = current;
    if (!session) return;
    const messages = ctx.sessionManager?.buildSessionContext?.().messages;
    const baseline = await head(session.cwd);
    if (current !== session) return;
    snapshots.set(turn.turnIndex, { startedAt: turn.timestamp, messages: Array.isArray(messages) ? messages.slice(-128) : [], session, baseline });
  });

  pi.on("turn_end", async (turn: TurnEnd) => {
    const snapshot = snapshots.get(turn.turnIndex);
    snapshots.delete(turn.turnIndex);
    if (snapshot && current === snapshot.session && snapshot.baseline) {
      const hints = commitHints(turn.message, turn.toolResults);
      if (hints.length) {
        const job = (async () => sendArtifacts(snapshot.session, await collectCommits(snapshot.session.cwd, snapshot.baseline!, hints)))();
        artifactJobs.add(job);
        try { await job; } finally { artifactJobs.delete(job); }
      }
    }
    if (!current || turn.message?.role !== "assistant" || typeof turn.message.provider !== "string" || turn.message.provider === "openrouter" || typeof turn.message.model !== "string") return;
    const timestamp = Number(turn.message.timestamp) || Date.now();
    const payload: Record<string, unknown> = {
      exchange_id: `oh-my-pi:${createHash("sha256").update(`${current.id}\0${timestamp}\0${turn.turnIndex}`).digest("hex").slice(0, 40)}`,
      ts: new Date(timestamp).toISOString(), provider: turn.message.provider, model: turn.message.model, request_kind: "primary",
      request: { messages: safe(snapshot?.messages ?? []) }, response: { message: safe(turn.message), tool_results: safe(turn.toolResults ?? []) },
      usage: { input_tokens: Math.max(0, Number(turn.message.usage?.input || 0) + Number(turn.message.usage?.cacheRead || 0)), output_tokens: Math.max(0, Number(turn.message.usage?.output || 0)) },
      tool_activity: normalizeToolActivity(turn.message, turn.toolResults),
      latency_ms: Math.max(0, Date.now() - (snapshot?.startedAt ?? timestamp)), title: sessionTitle(pi),
    };
    if (new TextEncoder().encode(JSON.stringify(payload)).byteLength > MAX_EXCHANGE_BYTES) return;
    deliver(`exchange:${payload.exchange_id}`, `/sessions/${encodeURIComponent(current.id)}/exchanges`, payload, headers());
  });

  pi.on("session_shutdown", async (shutdown: SessionShutdown) => {
    initialization++;
    clearInterval(heartbeat);
    heartbeat = undefined;
    await flushArtifacts();
    const reason = typeof shutdown?.reason === "string" ? shutdown.reason : "shutdown";
    const session = current;
    if (reason !== "reload" && session?.active) {
      await post(config, `/sessions/${encodeURIComponent(session.id)}/events`, event(session, "end", reason), headersFor(session));
    }
    current = null;
    restoreSessionID();
    snapshots.clear();
  });
}

export const __testing = { sessionID, safe };
