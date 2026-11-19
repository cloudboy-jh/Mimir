import type { Context, Hono } from "hono";
import type { AppEnv } from "../env";
import { readBoundedText } from "../exchanges/response-codec";
import { listCommits, patchLimit } from "../queries/commits";
import { daySnapshot, today } from "../queries/days";
import { listDevices } from "../queries/devices";
import { queryContext, type QueryContext, type QueryParams } from "../queries/query-context";
import { searchSessions } from "../queries/search";
import { getSession, listSessions } from "../queries/sessions";
import { stats } from "../queries/stats";
import { listTurns } from "../queries/turns";
import { loadGitArtifactPatch } from "../sessions/git-artifacts";
import { QueryError } from "../shared/query-error";

// Stateless MCP over Streamable HTTP: one JSON-RPC message per POST, JSON
// responses only. Tools call the same query functions as `/api`.

const PROTOCOL_VERSIONS = ["2025-06-18", "2025-03-26", "2024-11-05"];
const MAX_MESSAGE_BYTES = 1024 * 1024;
const DEFAULT_PATCH_BYTES = 65_536;
const MAX_PATCH_BYTES = 4 * 1024 * 1024;

type JsonSchema = { type: string; description?: string; enum?: string[] };
type Tool = {
  name: string;
  description: string;
  properties: Record<string, JsonSchema>;
  required?: string[];
  run: (ctx: QueryContext, args: QueryParams) => Promise<unknown>;
};

const text = (description: string): JsonSchema => ({ type: "string", description });
const range = {
  from: text("Start: RFC3339, YYYY-MM-DD (UTC) or relative like 24h / 7d."),
  to: text("End (inclusive): RFC3339, YYYY-MM-DD (UTC) or relative like 1d."),
};
const page = { limit: { type: "integer", description: "Maximum results." }, cursor: text("next_cursor from the previous page.") };

const TOOLS: Tool[] = [
  {
    name: "search",
    description: "Full-text search over everything said and done in past agent sessions (prompts, responses, tool calls and output), ranked by relevance and grouped by session.",
    properties: { query: text("Words to search for."), repo: text("Repository filter."), ...range, limit: page.limit },
    required: ["query"],
    run: (ctx, args) => searchSessions(ctx, { ...args, q: args.query }),
  },
  {
    name: "sessions",
    description: "List recorded agent sessions, newest activity first, filtered by text, repo, harness, device, state, git status or date range.",
    properties: {
      q: text("Title, intent or repo substring, or words said in the session."),
      repo: text("Repository."),
      harness: text("Harness such as claude-code, codex, opencode or pi."),
      device: text("Device id or name."),
      state: { type: "string", enum: ["active", "inactive"] },
      git_status: { type: "string", enum: ["none", "committed", "pushed"] },
      ...range,
      ...page,
    },
    run: listSessions,
  },
  {
    name: "session",
    description: "Get one session with its summary, receipt, touched files, errors, commits and sub-agent sessions.",
    properties: { id: text("Session id.") },
    required: ["id"],
    run: (ctx, args) => getSession(ctx, args.id!),
  },
  {
    name: "turns",
    description: "Read a session's conversation turn by turn: user prompt, assistant reply, reasoning and tool calls with output.",
    properties: { id: text("Session id."), ...page, order: { type: "string", enum: ["asc", "desc"] } },
    required: ["id"],
    run: (ctx, args) => listTurns(ctx, args.id!, args),
  },
  {
    name: "stats",
    description: "Aggregate sessions, requests, tokens and commits by repo, harness, device, model or day for charts and summaries.",
    properties: {
      group_by: { type: "string", enum: ["repo", "harness", "device", "model", "day"] },
      ...range,
      tz: text("IANA time zone for day grouping, default UTC."),
    },
    required: ["group_by"],
    run: stats,
  },
  {
    name: "day",
    description: "Summarize one calendar day of work: sessions grouped into workstreams with activity, tokens, commits and git status.",
    properties: { date: text("YYYY-MM-DD, default today in tz."), tz: text("IANA time zone, default UTC.") },
    run: (ctx, args) => {
      const timeZone = args.tz || "UTC";
      return daySnapshot(ctx, args.date || today(timeZone), timeZone);
    },
  },
  {
    name: "devices",
    description: "List the machines that record sessions, with their harnesses, session counts and tailnet status.",
    properties: {},
    run: (ctx) => listDevices(ctx),
  },
  {
    name: "commits",
    description: "List commits made during agent sessions, newest first, with the sessions that produced them.",
    properties: {
      repo: text("Repository URL or name."),
      ref: text("Branch or ref."),
      q: text("SHA, subject or ref substring."),
      session: text("Session id."),
      ...range,
      ...page,
    },
    run: listCommits,
  },
  {
    name: "commit_patch",
    description: "Read the saved patch of one commit captured in a session.",
    properties: { session_id: text("Session id."), sha: text("Full commit SHA."), max_bytes: { type: "integer", description: "Maximum patch bytes, default 65536." } },
    required: ["session_id", "sha"],
    run: async (ctx, args) => {
      const maxBytes = Math.min(patchLimit(args.max_bytes, DEFAULT_PATCH_BYTES)!, MAX_PATCH_BYTES);
      const patch = await loadGitArtifactPatch(ctx.db, ctx.logs, args.session_id!, args.sha!, maxBytes);
      return {
        session_id: args.session_id,
        sha: args.sha,
        patch: await readBoundedText(patch.body, maxBytes),
        truncated: patch.truncated,
        patch_bytes: patch.artifact.patch_bytes,
      };
    },
  },
];
const TOOLS_BY_NAME = new Map(TOOLS.map((tool) => [tool.name, tool]));

type RpcId = string | number | null;
const rpcError = (id: RpcId, code: number, message: string) => ({ jsonrpc: "2.0", id, error: { code, message } });

function toolArguments(tool: Tool, value: unknown): QueryParams | string {
  if (value !== undefined && (!value || typeof value !== "object" || Array.isArray(value))) return "arguments must be an object";
  const params: QueryParams = {};
  for (const [key, raw] of Object.entries((value as Record<string, unknown> | undefined) ?? {})) {
    if (!(key in tool.properties)) return `unknown argument: ${key}`;
    if (raw === null || raw === undefined) continue;
    if (typeof raw !== "string" && typeof raw !== "number" && typeof raw !== "boolean") return `invalid argument: ${key}`;
    params[key] = String(raw);
  }
  const missing = tool.required?.find((key) => !params[key]);
  return missing ? `missing required argument: ${missing}` : params;
}

async function callTool(ctx: QueryContext, params: Record<string, unknown>) {
  const tool = typeof params.name === "string" ? TOOLS_BY_NAME.get(params.name) : undefined;
  if (!tool) return null;
  const failure = (message: string) => ({ content: [{ type: "text", text: message }], isError: true });
  const args = toolArguments(tool, params.arguments);
  if (typeof args === "string") return failure(args);
  try {
    const result = await tool.run(ctx, args);
    return { content: [{ type: "text", text: JSON.stringify(result) }], structuredContent: result };
  } catch (error) {
    if (error instanceof QueryError) return failure(error.message);
    throw error;
  }
}

async function handle(c: Context<AppEnv>) {
  let body: unknown;
  try {
    body = JSON.parse(await readBoundedText(c.req.raw.body, MAX_MESSAGE_BYTES));
  } catch {
    return c.json(rpcError(null, -32700, "parse error"), 400);
  }
  const message = body && typeof body === "object" && !Array.isArray(body) ? (body as Record<string, unknown>) : null;
  if (!message || message.jsonrpc !== "2.0") return c.json(rpcError(null, -32600, "invalid request"), 400);
  // Notifications and client responses carry no id and need no reply.
  if (message.id === undefined || typeof message.method !== "string") return c.body(null, 202);
  const id = message.id as RpcId;
  const params = message.params && typeof message.params === "object" && !Array.isArray(message.params) ? (message.params as Record<string, unknown>) : {};
  const reply = (result: unknown) => c.json({ jsonrpc: "2.0", id, result });
  switch (message.method) {
    case "initialize": {
      const requested = typeof params.protocolVersion === "string" ? params.protocolVersion : "";
      return reply({
        protocolVersion: PROTOCOL_VERSIONS.includes(requested) ? requested : PROTOCOL_VERSIONS[0],
        capabilities: { tools: {} },
        serverInfo: { name: "mimir", version: c.env.MIMIR_BUNDLE_VERSION || "dev" },
      });
    }
    case "ping":
      return reply({});
    case "tools/list":
      return reply({
        tools: TOOLS.map((tool) => ({
          name: tool.name,
          description: tool.description,
          inputSchema: { type: "object", properties: tool.properties, ...(tool.required ? { required: tool.required } : {}), additionalProperties: false },
        })),
      });
    case "tools/call": {
      const result = await callTool(queryContext(c), params);
      return result ? reply(result) : c.json(rpcError(id, -32602, `unknown tool: ${String(params.name)}`));
    }
    default:
      return c.json(rpcError(id, -32601, `method not found: ${message.method}`));
  }
}

export function registerMcpRoutes(app: Hono<AppEnv>) {
  app.post("/mcp", handle);
  app.on(["GET", "DELETE"], "/mcp", (c) => c.json({ error: "method not allowed" }, 405, { allow: "POST" }));
}
