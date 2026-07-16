export type Outcome = "landed" | "discarded" | "abandoned" | "unresolved";
export type CaptureStatus = "empty" | "pending" | "saved" | "failed" | "partial";

export type CaptureSummary = {
  status: CaptureStatus;
  saved_exchanges: number;
  failed_exchanges: number;
  pending_exchanges: number;
  last_saved_at: string | null;
};

export type Exchange = {
  id: string;
  session_id: string;
  ts: string;
  model: string;
  provider: string;
  finish_reason: string;
  latency_ms: number;
  repo: string;
  harness: string;
  access_token_label: string;
  input_tokens: number;
  output_tokens: number;
  request: string;
  response: string;
};

export type Session = {
  id: string;
  started_at: string;
  ended_at: string | null;
  repo: string;
  source_ref: string;
  harness: string;
  model_primary: string;
  state: "active" | "inactive";
  outcome: Outcome;
  outcome_src: "agent" | "user" | "git" | "migration" | null;
  outcome_reason: string | null;
  outcome_updated_at: string | null;
  capture: CaptureSummary;
  request_count: number;
  tokens_in: number;
  tokens_out: number;
  intent: string;
  files: string[];
  errors: string[];
};

const request = (task: string) => JSON.stringify({ messages: [{ role: "user", content: task }], tools: ["read", "apply_patch", "bash"] }, null, 2);
const response = (summary: string) => JSON.stringify({ choices: [{ message: { role: "assistant", content: summary }, finish_reason: "stop" }] }, null, 2);

export const exchanges: Exchange[] = [
  { id: "01JZ7G1B2R6M", session_id: "01JZ7FZK9P", ts: "2026-07-15T18:15:00Z", model: "GPT-5.6 Luna", provider: "OpenAI", finish_reason: "tool_calls", latency_ms: 2840, repo: "mimir", harness: "OpenCode", access_token_label: "opencode-mac", input_tokens: 56122, output_tokens: 143, request: request("Replace custom dashboard authentication with Cloudflare Access."), response: response("Inspected the Access JWT contract and updated dashboard middleware.") },
  { id: "01JZ7G0NCC3S", session_id: "01JZ7FZK9P", ts: "2026-07-15T18:12:00Z", model: "GPT-5.6 Luna", provider: "OpenAI", finish_reason: "stop", latency_ms: 2130, repo: "mimir", harness: "OpenCode", access_token_label: "opencode-mac", input_tokens: 53491, output_tokens: 69, request: request("Review current dashboard authentication."), response: response("Found custom cookie auth competing with Cloudflare Access.") },
  { id: "01JZ7FWFBT5E", session_id: "01JZ7FJ1WH", ts: "2026-07-15T17:54:00Z", model: "Claude Haiku 4.5", provider: "Amazon Bedrock", finish_reason: "stop", latency_ms: 2260, repo: "mimir", harness: "Hermes", access_token_label: "hermes-jserv", input_tokens: 21689, output_tokens: 101, request: request("Build dashboard request log schema."), response: response("Added provider, finish reason, and normalized token metadata.") },
  { id: "01JZ7FQJ95ZT", session_id: "01JZ7FJ1WH", ts: "2026-07-15T17:51:00Z", model: "Claude Haiku 4.5", provider: "Amazon Bedrock", finish_reason: "stop", latency_ms: 1020, repo: "mimir", harness: "Hermes", access_token_label: "hermes-jserv", input_tokens: 139, output_tokens: 7, request: request("Inspect exchange persistence."), response: response("Located the capture and D1 write path.") },
  { id: "01JZ6YBR10SB", session_id: "01JZ6Y2GRP", ts: "2026-07-14T23:11:00Z", model: "Gemini 2.5 Flash", provider: "Google", finish_reason: "stop", latency_ms: 1680, repo: "portfolio", harness: "Claude Code", access_token_label: "claude-mac", input_tokens: 18302, output_tokens: 812, request: request("Fix image loading on the portfolio route."), response: response("Changed the asset path and verified the production build.") },
  { id: "01JZ6Y8RX2T8", session_id: "01JZ6Y2GRP", ts: "2026-07-14T23:05:00Z", model: "Gemini 2.5 Flash", provider: "Google", finish_reason: "tool_calls", latency_ms: 1940, repo: "portfolio", harness: "Claude Code", access_token_label: "claude-mac", input_tokens: 17445, output_tokens: 344, request: request("Trace broken portfolio images."), response: response("Found an incorrect Vite-relative asset import.") },
  { id: "01JZ5Q0A9Y6F", session_id: "01JZ5PT6FK", ts: "2026-07-14T16:29:00Z", model: "GPT-4o mini", provider: "OpenAI", finish_reason: "stop", latency_ms: 640, repo: "mimir", harness: "OpenCode", access_token_label: "opencode-mac", input_tokens: 9214, output_tokens: 91, request: request("Consolidate the Go CLI package."), response: response("Moved command handling under internal/mimircli and preserved public behavior.") },
  { id: "01JZ5PV3HSW5", session_id: "01JZ5PT6FK", ts: "2026-07-14T16:27:00Z", model: "GPT-4o mini", provider: "OpenAI", finish_reason: "stop", latency_ms: 720, repo: "mimir", harness: "OpenCode", access_token_label: "opencode-mac", input_tokens: 8951, output_tokens: 54, request: request("Map CLI package boundaries."), response: response("Identified setup, remote, and local responsibilities.") }
];

export const sessions: Session[] = [
  { id: "01JZ7FZK9P", started_at: "2026-07-15T17:51:00Z", ended_at: null, repo: "mimir", source_ref: "master", harness: "OpenCode", model_primary: "GPT-5.6 Luna", state: "active", outcome: "unresolved", outcome_src: null, outcome_reason: null, outcome_updated_at: null, capture: { status: "pending", saved_exchanges: 17, failed_exchanges: 0, pending_exchanges: 1, last_saved_at: "2026-07-15T18:15:00Z" }, request_count: 17, tokens_in: 230441, tokens_out: 4182, intent: "Replace custom dashboard authentication with Cloudflare Access", files: ["worker/src/app.ts", "worker/wrangler.jsonc", "internal/mimircli/dashboard.go"], errors: ["Access JWT audience was not configured"] },
  { id: "01JZ7FJ1WH", started_at: "2026-07-15T17:20:00Z", ended_at: "2026-07-15T17:58:00Z", repo: "mimir", source_ref: "master", harness: "Hermes", model_primary: "Claude Haiku 4.5", state: "inactive", outcome: "landed", outcome_src: "agent", outcome_reason: "Provider metadata shipped and the integration checks passed.", outcome_updated_at: "2026-07-15T18:01:00Z", capture: { status: "saved", saved_exchanges: 7, failed_exchanges: 0, pending_exchanges: 0, last_saved_at: "2026-07-15T17:54:00Z" }, request_count: 7, tokens_in: 42361, tokens_out: 981, intent: "Add provider-aware request metadata for the dashboard", files: ["worker/migrations/0006_dashboard.sql", "worker/src/app.ts", "worker/test/integration.test.ts"], errors: [] },
  { id: "01JZ6Y2GRP", started_at: "2026-07-14T22:40:00Z", ended_at: "2026-07-14T23:14:00Z", repo: "portfolio", source_ref: "main", harness: "Claude Code", model_primary: "Gemini 2.5 Flash", state: "inactive", outcome: "landed", outcome_src: "agent", outcome_reason: "Git evidence shows the image-path fix on main.", outcome_updated_at: "2026-07-14T23:20:00Z", capture: { status: "partial", saved_exchanges: 10, failed_exchanges: 1, pending_exchanges: 0, last_saved_at: "2026-07-14T23:11:00Z" }, request_count: 10, tokens_in: 121762, tokens_out: 2843, intent: "Repair broken image loading in the portfolio build", files: ["src/pages/Work.vue", "vite.config.ts"], errors: ["Asset URL resolved outside Vite base path"] },
  { id: "01JZ5PT6FK", started_at: "2026-07-14T15:52:00Z", ended_at: "2026-07-14T16:31:00Z", repo: "mimir", source_ref: "master", harness: "OpenCode", model_primary: "GPT-4o mini", state: "inactive", outcome: "landed", outcome_src: "user", outcome_reason: "CLI package consolidation was accepted after build verification.", outcome_updated_at: "2026-07-14T16:36:00Z", capture: { status: "saved", saved_exchanges: 9, failed_exchanges: 0, pending_exchanges: 0, last_saved_at: "2026-07-14T16:29:00Z" }, request_count: 9, tokens_in: 88204, tokens_out: 1210, intent: "Consolidate command handling into the internal CLI package", files: ["cmd/mimir/main.go", "internal/mimircli/command.go", "internal/mimircli/setup.go"], errors: [] },
  { id: "01JZ4M9D7Q", started_at: "2026-07-13T20:18:00Z", ended_at: "2026-07-13T20:46:00Z", repo: "mimir", source_ref: "dashboard-experiment", harness: "OpenCode", model_primary: "GPT-5.6 Luna", state: "inactive", outcome: "discarded", outcome_src: "user", outcome_reason: "The approach conflicted with canonical remote D1 ownership.", outcome_updated_at: "2026-07-13T20:49:00Z", capture: { status: "saved", saved_exchanges: 13, failed_exchanges: 0, pending_exchanges: 0, last_saved_at: "2026-07-13T20:44:00Z" }, request_count: 13, tokens_in: 164883, tokens_out: 3062, intent: "Prototype a Git-backed session synchronization flow", files: ["internal/mimircli/sync.go", "docs/Spec.md"], errors: ["Session sync conflicts with canonical remote D1 ownership"] },
  { id: "01JZ3A2KPM", started_at: "2026-07-12T18:06:00Z", ended_at: "2026-07-12T18:17:00Z", repo: "mimir", source_ref: "master", harness: "Claude Code", model_primary: "Claude Sonnet 4", state: "inactive", outcome: "abandoned", outcome_src: "agent", outcome_reason: "Work stopped when Vectorize exceeded the approved product scope.", outcome_updated_at: "2026-07-12T18:18:00Z", capture: { status: "failed", saved_exchanges: 0, failed_exchanges: 4, pending_exchanges: 0, last_saved_at: null }, request_count: 0, tokens_in: 0, tokens_out: 0, intent: "Explore semantic embeddings for session search", files: ["worker/src/search.ts"], errors: ["Vectorize would add infrastructure outside the current scope"] }
];

export const overview = {
  totals: { requests: 1284, sessions: 42, saved_exchanges: 1279, capture_failures: 5, input_tokens: 4800000, output_tokens: 126000 },
  models: [{ name: "GPT-5.6 Luna", requests: 642 }, { name: "Claude Haiku 4.5", requests: 411 }, { name: "Gemini 2.5 Flash", requests: 231 }],
  providers: [{ name: "OpenAI", requests: 738 }, { name: "Amazon Bedrock", requests: 356 }, { name: "Google", requests: 190 }],
  apps: [{ name: "OpenCode", requests: 812 }, { name: "Hermes", requests: 298 }, { name: "Claude Code", requests: 174 }]
};

export const sessionById = (id: string) => sessions.find((session) => session.id === id);
export const exchangeById = (id: string) => exchanges.find((exchange) => exchange.id === id);
export const exchangesForSession = (id: string) => exchanges.filter((exchange) => exchange.session_id === id);
