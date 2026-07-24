// Mimir capture plugin for OpenCode.
//
// Reports completed turns, heartbeats, and session ends to the Mimir session
// object. Capture happens above provider transport, so every OpenCode
// provider (OpenRouter, Zen subscription, Claude key, Codex/ChatGPT OAuth) is
// covered identically.
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

import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

const HEARTBEAT_MS = 60_000;
const ACTIVITY_WINDOW_MS = 5 * 60_000;

type Connection = { url: string; token: string };
type OpenCodeConfig = { mcp?: Record<string, unknown> };

type SessionEvent = {
  version: 1;
  kind: "turn" | "heartbeat" | "end";
  session_id: string;
  harness: string | null;
  repo?: string | null;
  ts: string;
  turn?: Record<string, unknown>;
  reason?: string;
};

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

function resolveMCPCommand(
  env: Record<string, string | undefined>,
  readFile: (path: string) => string | null,
  home: string | undefined,
): string[] | null {
  const dir = env.MIMIR_HOME?.trim() || (home ? join(home, ".mimir") : null);
  if (!dir) return null;
  const raw = readFile(join(dir, "install-receipt.json"));
  if (!raw) return null;
  try {
    const receipt = JSON.parse(raw) as { cli?: { path?: unknown } };
    const path = receipt.cli?.path;
    return typeof path === "string" && path ? [path, "serve"] : null;
  } catch {
    return null;
  }
}

function injectMCP(config: OpenCodeConfig, command: string[]): void {
  config.mcp ??= {};
  config.mcp.mimir = { type: "local", command, enabled: true };
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

export const MimirPlugin = async ({ directory, worktree }: { directory?: string; worktree?: string }) => {
  const conn = loadConnection();
  if (!conn) return {};
  const repo = repoName(worktree ?? directory);
  const delivery = createDeliveryQueue((event) => postEvent(conn, event));
  const activity = createActivityTracker();

  const timer = setInterval(() => {
    const sessionID = activity.active();
    if (sessionID) delivery.deliver({ version: 1, kind: "heartbeat", session_id: sessionID, harness: "opencode", repo, ts: new Date().toISOString() });
  }, HEARTBEAT_MS);
  (timer as { unref?: () => void }).unref?.();

  return {
    config: async (config: OpenCodeConfig) => {
      const command = resolveMCPCommand(process.env, (path) => {
        try { return existsSync(path) ? readFileSync(path, "utf8") : null; } catch { return null; }
      }, (() => { try { return homedir(); } catch { return undefined; } })());
      if (!command) return;
      injectMCP(config, command);
    },
    event: async ({ event }: { event: { type: string; properties?: Record<string, unknown> } }) => {
      const properties = event.properties ?? {};
      if (event.type === "message.updated") {
        const turn = buildTurnEvent(properties.info, repo);
        const info = properties.info as Record<string, unknown> | undefined;
        if (typeof info?.sessionID === "string") activity.touch(info.sessionID);
        if (turn && typeof info?.id === "string") {
          delivery.deliver(turn);
        }
        return;
      }
      if (event.type === "session.created" || event.type === "session.updated") {
        const info = properties.info as Record<string, unknown> | undefined;
        if (typeof info?.id === "string") {
          activity.touch(info.id);
          delivery.deliver({ version: 1, kind: "heartbeat", session_id: info.id, harness: "opencode", repo, ts: new Date().toISOString() });
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

// Test surface. The OpenCode plugin loader only invokes function exports, so
// this object is inert in production.
export const __testing = { parseMimirConfig, resolveConnection, resolveMCPCommand, injectMCP, buildTurnEvent, repoName, createActivityTracker, createDeliveryQueue, postEvent };
