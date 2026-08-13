import type {
  CaptureSummary,
  DashboardIdentity,
  Device,
  DeviceIdentity,
  Exchange,
  Facets,
  LogEnvelope,
  Outcome,
  OutcomeEvidence,
  OutcomeEvent,
  Overview,
  Session,
  SessionDetail,
  SessionExchange,
  SessionObjectState,
} from "@/lib/api";

const now = new Date("2026-07-29T17:45:00.000Z");
const iso = (minutesAgo: number) => new Date(now.getTime() - minutesAgo * 60_000).toISOString();
const clone = <T>(value: T): T => structuredClone(value);

const devices: Device[] = [
  { id: "dev_fixture_macbook", name: "Studio MacBook", platform: "darwin", arch: "arm64", created_at: iso(28_800), updated_at: iso(1), last_seen_at: iso(1), revoked_at: null, session_count: 18, harnesses: ["opencode", "claude-code"] },
  { id: "dev_fixture_linux", name: "Build workstation", platform: "linux", arch: "x64", created_at: iso(86_400), updated_at: iso(332), last_seen_at: null, revoked_at: null, session_count: 9, harnesses: ["hermes", "codex"] },
  { id: "dev_fixture_windows", name: "Previous desktop", platform: "windows", arch: "x64", created_at: iso(172_800), updated_at: iso(43_200), last_seen_at: iso(43_200), revoked_at: iso(40_320), session_count: 4, harnesses: ["opencode"] },
];
const deviceIdentity = ({ id, name, platform, arch }: Device): DeviceIdentity => ({ id, name, platform, arch });

const savedCapture = (saved: number, failed = 0): CaptureSummary => ({
  status: failed ? "partial" : "saved",
  saved_exchanges: saved,
  failed_exchanges: failed,
  pending_exchanges: 0,
  last_saved_at: iso(4),
});

const primaryPatch = `diff --git a/worker/web/src/components/session/SessionHeader.vue b/worker/web/src/components/session/SessionHeader.vue
index 1122334..2233445 100644
--- a/worker/web/src/components/session/SessionHeader.vue
+++ b/worker/web/src/components/session/SessionHeader.vue
@@ -10,3 +10,5 @@
-  <div class="model-stack">
+  <div class="model-tree">
+    <span>Models involved</span>
   </div>
diff --git a/worker/web/src/components/session/SessionChanges.vue b/worker/web/src/components/session/SessionChanges.vue
new file mode 100644
index 0000000..3344556
--- /dev/null
+++ b/worker/web/src/components/session/SessionChanges.vue
@@ -0,0 +1,4 @@
+<template>
+  <section aria-labelledby="result-evidence-heading">
+  </section>
+</template>
diff --git a/worker/web/src/lib/dev-fixtures.ts b/worker/web/src/lib/dev-fixtures.ts
new file mode 100644
index 0000000..4455667
--- /dev/null
+++ b/worker/web/src/lib/dev-fixtures.ts
@@ -0,0 +1,3 @@
+export const sessions = [];
+export const exchanges = [];
+export const outcomes = [];
diff --git a/worker/web/src/styles.css b/worker/web/src/styles.css
index 5566778..6677889 100644
--- a/worker/web/src/styles.css
+++ b/worker/web/src/styles.css
@@ -7,2 +7,3 @@
-  --animate-panel-in: panel-in 160ms ease-out;
+  --animate-panel-in: panel-in 200ms cubic-bezier(0.16, 1, 0.3, 1);
+  --animate-panel-out: panel-out 150ms cubic-bezier(0.4, 0, 1, 1);`;

const sessions: Session[] = [
  {
    id: "ses_fixture_multi_model_result",
    parent_session_id: null,
    started_at: iso(78),
    ended_at: iso(6),
    state: "inactive",
    liveness: "finalized",
    last_active_at: iso(6),
    activity_at: iso(6),
    inactive_at: iso(5),
    harness: "opencode",
    boundary: "exact",
    outcome: "landed",
    outcome_src: "agent",
    outcome_updated_at: iso(5),
    outcome_reason: "Restored result evidence, repaired the model tree, and made dashboard overlays transition cleanly.",
    repo: "mimir",
    source_ref: "feature/dashboard-evidence",
    model_primary: "openai/gpt-5.6-sol",
    models: [
      { name: "openai/gpt-5.6-sol", request_count: 12, first_seen_at: iso(77), last_seen_at: iso(6) },
      { name: "anthropic/claude-opus-4.1-thinking", request_count: 4, first_seen_at: iso(54), last_seen_at: iso(18) },
      { name: "google/gemini-2.5-pro-preview-06-05", request_count: 2, first_seen_at: iso(32), last_seen_at: iso(21) },
    ],
    request_count: 21,
    tokens_in: 381_420,
    tokens_out: 42_870,
    title: "Restore dashboard evidence hierarchy",
    title_source: "manual",
    title_updated_at: iso(3),
    display_title: "Restore dashboard evidence hierarchy",
    intent: "Correct session evidence hierarchy, multi-model rendering, and dashboard motion without hiding implementation detail",
    child_session_count: 2,
    capture: savedCapture(21),
    device: deviceIdentity(devices[0]),
  },
  {
    id: "ses_fixture_active_capture",
    parent_session_id: null,
    started_at: iso(24),
    ended_at: null,
    state: "active",
    liveness: "active",
    last_active_at: iso(1),
    activity_at: iso(1),
    inactive_at: null,
    harness: "hermes",
    boundary: "exact",
    outcome: "unresolved",
    outcome_src: null,
    outcome_updated_at: null,
    outcome_reason: null,
    repo: "mimir",
    source_ref: "main",
    model_primary: "anthropic/claude-sonnet-4.5",
    models: [{ name: "anthropic/claude-sonnet-4.5", request_count: 7, first_seen_at: iso(24), last_seen_at: iso(1) }],
    request_count: 7,
    tokens_in: 88_200,
    tokens_out: 9_840,
    title: null,
    title_source: null,
    title_updated_at: null,
    display_title: "Investigate intermittent capture receipts from direct providers",
    intent: "Investigate intermittent capture receipts from direct providers",
    child_session_count: 0,
    capture: { status: "pending", saved_exchanges: 6, failed_exchanges: 0, pending_exchanges: 1, last_saved_at: iso(2) },
    device: deviceIdentity(devices[1]),
  },
  {
    id: "ses_fixture_failed_work",
    parent_session_id: null,
    started_at: iso(390),
    ended_at: iso(332),
    state: "inactive",
    liveness: "finalized",
    last_active_at: iso(332),
    activity_at: iso(332),
    inactive_at: iso(331),
    harness: "opencode",
    boundary: "exact",
    outcome: "discarded",
    outcome_src: "user",
    outcome_updated_at: iso(330),
    outcome_reason: "The migration changed ownership semantics and was reverted.",
    repo: "mimir",
    source_ref: "experiment/session-sync",
    model_primary: "openai/gpt-5.6-sol",
    models: [{ name: "openai/gpt-5.6-sol", request_count: 9, first_seen_at: iso(390), last_seen_at: iso(332) }],
    request_count: 9,
    tokens_in: 144_800,
    tokens_out: 18_420,
    title: null,
    title_source: null,
    title_updated_at: null,
    display_title: "Prototype a session synchronization path and validate ownership behavior",
    intent: "Prototype a session synchronization path and validate ownership behavior",
    child_session_count: 1,
    capture: savedCapture(8, 1),
    device: deviceIdentity(devices[2]),
  },
  {
    id: "ses_fixture_empty",
    parent_session_id: null,
    started_at: iso(1_420),
    ended_at: iso(1_419),
    state: "inactive",
    liveness: "finalized",
    last_active_at: iso(1_419),
    activity_at: iso(1_419),
    inactive_at: iso(1_418),
    harness: "opencode",
    boundary: "fallback",
    outcome: "abandoned",
    outcome_src: "agent",
    outcome_updated_at: iso(1_418),
    outcome_reason: "The provider rejected the request before work began.",
    repo: null,
    source_ref: null,
    model_primary: null,
    models: [],
    request_count: 0,
    tokens_in: 0,
    tokens_out: 0,
    title: null,
    title_source: null,
    title_updated_at: null,
    display_title: null,
    intent: null,
    child_session_count: 0,
    capture: { status: "empty", saved_exchanges: 0, failed_exchanges: 0, pending_exchanges: 0, last_saved_at: null },
    device: null,
  },
];

const supportingSessions: SessionDetail["supporting_sessions"] = [
  {
    id: "ses_fixture_supporting_review",
    parent_session_id: sessions[0].id,
    started_at: iso(58),
    ended_at: iso(49),
    state: "inactive",
    last_active_at: iso(49),
    inactive_at: iso(48),
    harness: "opencode",
    boundary: "exact",
    outcome: "landed",
    outcome_src: "agent",
    outcome_updated_at: iso(48),
    outcome_reason: "Located evidence selection and layout faults.",
    repo: "mimir",
    source_ref: "feature/dashboard-evidence",
    model_primary: "anthropic/claude-opus-4.1-thinking",
    models: [{ name: "anthropic/claude-opus-4.1-thinking", request_count: 2, first_seen_at: iso(58), last_seen_at: iso(49) }],
    request_count: 2,
    tokens_in: 28_400,
    tokens_out: 3_120,
    title: null,
    title_source: null,
    title_updated_at: null,
    display_title: "Audit the session detail information architecture and identify evidence regressions",
    intent: "Audit the session detail information architecture and identify evidence regressions",
    device: deviceIdentity(devices[0]),
  },
  {
    id: "ses_fixture_supporting_motion",
    parent_session_id: sessions[0].id,
    started_at: iso(42),
    ended_at: iso(36),
    state: "inactive",
    last_active_at: iso(36),
    inactive_at: iso(35),
    harness: "opencode",
    boundary: "exact",
    outcome: "landed",
    outcome_src: "agent",
    outcome_updated_at: iso(35),
    outcome_reason: "Reviewed Reka presence states and reduced-motion handling.",
    repo: "mimir",
    source_ref: "feature/dashboard-evidence",
    model_primary: "google/gemini-2.5-pro-preview-06-05",
    models: [{ name: "google/gemini-2.5-pro-preview-06-05", request_count: 1, first_seen_at: iso(42), last_seen_at: iso(36) }],
    request_count: 1,
    tokens_in: 11_220,
    tokens_out: 1_980,
    title: null,
    title_source: null,
    title_updated_at: null,
    display_title: "Inspect shared overlay motion and select transitions",
    intent: "Inspect shared overlay motion and select transitions",
    device: deviceIdentity(devices[0]),
  },
];

const outcomeEvents: OutcomeEvent[] = [
  {
    id: "out_fixture_landed",
    outcome: "landed",
    source: "agent",
    reason: sessions[0].outcome_reason,
    evidence_json: JSON.stringify({
      commit: "7ad8d9e43a61c59fe22379f8e5ca68dbe8c41120",
      base_commit: "412ceaa3928a9d896af1cb1d447688fcaa82bc31",
      patch: primaryPatch,
      provenance: "opencode",
      repository_url: "https://github.com/example/mimir",
      commit_url: "https://github.com/example/mimir/commit/7ad8d9e43a61c59fe22379f8e5ca68dbe8c41120",
      ref: "feature/dashboard-evidence",
    }),
    created_at: iso(5),
  },
  { id: "out_fixture_unresolved", outcome: "unresolved", source: "agent", reason: "Implementation was still in progress.", evidence_json: JSON.stringify({ note: "Waiting on responsive layout verification." }), created_at: iso(44) },
];

const sessionExchanges: SessionExchange[] = Array.from({ length: 21 }, (_, index) => {
  const model = sessions[0].models[index % sessions[0].models.length];
  return {
    id: `req_fixture_${String(index + 1).padStart(2, "0")}`,
    session_id: sessions[0].id,
    ts: iso(7 + index * 3),
    model: model.name,
    provider: model.name.split("/")[0],
    finish_reason: index % 5 === 0 ? "tool-calls" : "stop",
    latency_ms: 1_800 + index * 173,
    harness: "opencode",
    input_tokens: 8_400 + index * 1_170,
    output_tokens: 720 + index * 83,
    request_excerpt: index % 3 === 0 ? "Inspect the dashboard session detail components and trace the missing result evidence." : index % 3 === 1 ? "Implement the fixture-backed development transport without leaking mock behavior into components." : "Verify the model hierarchy, overlay transitions, and responsive evidence layout.",
    capture_status: "saved",
    capture_reason: null,
    failure_code: null,
  };
});

const exchanges: Exchange[] = sessionExchanges.map((exchange) => ({
  id: exchange.id,
  session_id: exchange.session_id,
  ts: exchange.ts,
  model: exchange.model,
  provider: exchange.provider,
  finish_reason: exchange.finish_reason,
  endpoint: "/v1/chat/completions",
  latency_ms: exchange.latency_ms,
  repo: "mimir",
  harness: exchange.harness,
  access_token_label: "fixture-machine",
  input_tokens: exchange.input_tokens,
  output_tokens: exchange.output_tokens,
  r2_key: `fixtures/${exchange.id}.json`,
}));

function detailFor(session: Session): SessionDetail {
  const { capture, liveness: _liveness, ...detailSession } = session;
  const rich = session.id === sessions[0].id;
  return {
    session: detailSession,
    capture,
    supporting_sessions: rich ? supportingSessions : [],
    outcome_events: rich ? outcomeEvents : [],
    files: rich ? [
      "worker/web/src/components/session/SessionHeader.vue",
      "worker/web/src/components/session/SessionOutcome.vue",
      "worker/web/src/components/session/RequestTimeline.vue",
      "worker/web/src/lib/api.ts",
      "worker/src/routes/dashboard.ts",
      "docs/DESIGN.md",
      "README.md",
    ] : [],
    errors: rich ? [
      { signature: "Cloudflare Access authentication required.", count: 2, first_seen_at: iso(68), last_seen_at: iso(63), latest_exchange_id: sessionExchanges[15].id },
      { signature: "Patch evidence exceeded the configured capture bound", count: 1, first_seen_at: iso(22), last_seen_at: iso(22), latest_exchange_id: sessionExchanges[5].id },
    ] : [],
  };
}

function paginate<T>(items: T[], params: URLSearchParams) {
  const start = Number(params.get("cursor") ?? 0);
  const limit = Number(params.get("limit") ?? 25);
  const page = items.slice(start, start + limit);
  return { page, next_cursor: start + limit < items.length ? String(start + limit) : null };
}

function facetsFor(): Facets {
  return {
    repos: ["mimir"],
    apps: ["opencode", "hermes"],
    models: [...new Set(sessions.flatMap((session) => session.models.map((model) => model.name)))],
    providers: ["openai", "anthropic", "google"],
    finish_reasons: ["stop", "tool-calls"],
  };
}

export async function fixtureRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  if (init.signal?.aborted) throw new DOMException("Aborted", "AbortError");
  const url = new URL(path, "https://mimir.fixture");
  const segments = url.pathname.split("/").filter(Boolean);

  if (url.pathname === "/dashboard/api/identity") {
    return clone({ email: "developer@mimir.local", name: "Fixture Developer", source: "local-development" } satisfies DashboardIdentity) as T;
  }

  if (url.pathname === "/dashboard/api/devices") return clone({ devices }) as T;

  if (segments[2] === "devices" && segments[3] && init.method === "PATCH") {
    const device = devices.find((item) => item.id === segments[3]);
    if (!device) throw new Error("Fixture device not found.");
    const body = JSON.parse(String(init.body ?? "{}")) as { name?: string };
    device.name = body.name?.trim() || device.name;
    device.updated_at = now.toISOString();
    return clone({ device }) as T;
  }

  if (segments[2] === "devices" && segments[3] && segments[4] === "revoke" && init.method === "POST") {
    const device = devices.find((item) => item.id === segments[3]);
    if (!device) throw new Error("Fixture device not found.");
    device.revoked_at = now.toISOString();
    device.updated_at = now.toISOString();
    return clone({ device }) as T;
  }

  if (url.pathname === "/dashboard/api/sessions") {
    const needle = (url.searchParams.get("q") ?? "").toLowerCase();
    const filtered = sessions.filter((session) => {
      const haystack = [session.id, session.display_title, session.title, session.intent, session.repo, session.harness, ...session.models.map((model) => model.name)].filter(Boolean).join(" ").toLowerCase();
      return (!needle || haystack.includes(needle))
        && (!url.searchParams.get("repo") || session.repo === url.searchParams.get("repo"))
        && (!url.searchParams.get("outcome") || session.outcome === url.searchParams.get("outcome"))
        && (!url.searchParams.get("app") || session.harness === url.searchParams.get("app"))
        && (!url.searchParams.get("model") || session.models.some((model) => model.name === url.searchParams.get("model")));
    });
    const { page, next_cursor } = paginate(filtered, url.searchParams);
    return clone({ sessions: page, next_cursor }) as T;
  }

  if (segments[2] === "sessions" && segments[3] && segments[4] === "outcome" && init.method === "POST") {
    const session = sessions.find((item) => item.id === segments[3]);
    if (!session) throw new Error("Fixture session not found.");
    const body = JSON.parse(String(init.body ?? "{}")) as { outcome: Outcome; reason?: string; evidence?: OutcomeEvidence };
    session.outcome = body.outcome;
    session.outcome_reason = body.reason ?? null;
    session.outcome_src = "user";
    session.outcome_updated_at = now.toISOString();
    outcomeEvents.unshift({ id: `out_fixture_${outcomeEvents.length + 1}`, outcome: body.outcome, source: "user", reason: body.reason ?? null, evidence_json: body.evidence ? JSON.stringify(body.evidence) : null, created_at: now.toISOString() });
    return clone({ id: session.id, outcome: session.outcome }) as T;
  }

  if (segments[2] === "sessions" && segments[3] && segments[4] === "title" && init.method === "PATCH") {
    const session = sessions.find((item) => item.id === segments[3]);
    if (!session) throw new Error("Fixture session not found.");
    const body = JSON.parse(String(init.body ?? "{}")) as { title: string };
    session.title = body.title.trim();
    session.title_source = "manual";
    session.title_updated_at = now.toISOString();
    session.display_title = session.title || session.intent || null;
    return clone({ session: { id: session.id, title: session.title, title_source: session.title_source, title_updated_at: session.title_updated_at, display_title: session.display_title } }) as T;
  }

  if (segments[2] === "sessions" && segments[3] && segments[4] === "exchanges") {
    let filtered = segments[3] === sessions[0].id ? sessionExchanges : [];
    const q = (url.searchParams.get("q") ?? "").toLowerCase();
    if (q) filtered = filtered.filter((exchange) => `${exchange.id} ${exchange.request_excerpt}`.toLowerCase().includes(q));
    for (const [parameter, field] of [["model", "model"], ["provider", "provider"], ["app", "harness"], ["finish_reason", "finish_reason"]] as const) {
      const value = url.searchParams.get(parameter);
      if (value) filtered = filtered.filter((exchange) => exchange[field] === value);
    }
    if (url.searchParams.get("order") !== "asc") filtered = [...filtered].reverse();
    const { page, next_cursor } = paginate(filtered, url.searchParams);
    return clone({ exchanges: page, next_cursor }) as T;
  }

  if (segments[2] === "sessions" && segments[3] && segments[4] === "object-state") {
    const session = sessions.find((item) => item.id === segments[3]);
    if (!session) throw new Error("Fixture session not found.");
    return clone({
      session_id: session.id,
      parent_session_id: session.parent_session_id,
      liveness: session.liveness,
      harness: session.harness,
      repo: session.repo,
      started_at: session.started_at,
      last_event_at: session.last_active_at ?? session.started_at,
      finalized_at: session.liveness === "finalized" ? session.ended_at : null,
      end_reason: session.liveness === "finalized" ? "fixture" : null,
      turn_count: session.request_count,
      tokens_in: session.tokens_in,
      tokens_out: session.tokens_out,
    } satisfies SessionObjectState) as T;
  }

  if (segments[2] === "sessions" && segments[3]) {
    const session = sessions.find((item) => item.id === segments[3]);
    if (!session) throw new Error("Fixture session not found.");
    return clone(detailFor(session)) as T;
  }

  if (url.pathname === "/dashboard/api/facets") return clone(facetsFor()) as T;

  if (url.pathname === "/dashboard/api/log") {
    let filtered = exchanges;
    const provider = url.searchParams.get("provider");
    const app = url.searchParams.get("app");
    if (provider) filtered = filtered.filter((exchange) => exchange.provider === provider);
    if (app) filtered = filtered.filter((exchange) => exchange.harness === app);
    const { page, next_cursor } = paginate(filtered, url.searchParams);
    return clone({ exchanges: page, next_cursor }) as T;
  }

  if (segments[2] === "log" && segments[3]) {
    const exchange = exchanges.find((item) => item.id === segments[3]);
    if (!exchange) throw new Error("Fixture request not found.");
    return clone({ exchange, log_url: `/dashboard/dev-fixtures/log/${exchange.id}` }) as T;
  }

  if (segments[1] === "dev-fixtures" && segments[2] === "log" && segments[3]) {
    const exchange = exchanges.find((item) => item.id === segments[3]);
    if (!exchange) throw new Error("Fixture log not found.");
    return clone({
      schema_version: 1,
      exchange_id: exchange.id,
      session_id: exchange.session_id,
      captured_at: exchange.ts,
      endpoint: exchange.endpoint,
      request: { model: exchange.model, messages: [{ role: "user", content: "Fixture request body for dashboard development." }] },
      response: { format: "json", body: { choices: [{ message: { role: "assistant", content: "Fixture response body." }, finish_reason: exchange.finish_reason }] } },
    } satisfies LogEnvelope) as T;
  }

  if (url.pathname === "/dashboard/api/overview") {
    const overview: Overview = {
      totals: { requests: 37, sessions: sessions.length, saved_exchanges: 35, capture_failures: 1, input_tokens: 614_420, output_tokens: 73_110 },
      models: facetsFor().models.slice(0, 4).map((name) => ({ name, requests: sessions.reduce((sum, session) => sum + (session.models.find((model) => model.name === name)?.request_count ?? 0), 0) })),
      providers: [{ name: "openai", requests: 21 }, { name: "anthropic", requests: 13 }, { name: "google", requests: 3 }],
      apps: [{ name: "opencode", requests: 30 }, { name: "hermes", requests: 7 }],
    };
    return clone(overview) as T;
  }

  throw new Error(`No dashboard fixture for ${init.method ?? "GET"} ${url.pathname}`);
}
