import type {
  CaptureSummary,
  DashboardIdentity,
  Device,
  DeviceIdentity,
  Exchange,
  Facets,
  CommitCapture,
  Outcome,
  OutcomeEvidence,
  OutcomeEvent,
  Overview,
  Session,
  SessionDetail,
  SessionObjectState,
} from "@/lib/api";

import { fixtureEnvelope, fixtureIso as iso, fixtureReference as now, fixtureSessionExchanges, fixtureTurns } from "./fixture-conversations";
import { fixtureArtifactRows, sampleHierarchyPatch as primaryPatch } from "./fixture-git-artifacts";
import { fixtureCommitPage, fixtureDateMatches, fixtureRepositoryKey, fixtureRepositoryPage, fixtureRefPage, fixtureRowPage } from "./fixture-queries";
const clone = <T>(value: T): T => structuredClone(value);

const devices: Device[] = [
  { id: "dev_fixture_macbook", name: "Studio MacBook", platform: "darwin", arch: "arm64", created_at: iso(28_800), updated_at: iso(1), last_seen_at: iso(1), revoked_at: null, session_count: 18, harnesses: ["opencode", "pi", "oh-my-pi", "claude-code", "cursor"] },
  { id: "dev_fixture_linux", name: "Build workstation", platform: "linux", arch: "x64", created_at: iso(86_400), updated_at: iso(332), last_seen_at: null, revoked_at: null, session_count: 9, harnesses: ["hermes", "codex"] },
  { id: "dev_fixture_windows", name: "Previous desktop", platform: "windows", arch: "x64", created_at: iso(172_800), updated_at: iso(43_200), last_seen_at: iso(43_200), revoked_at: iso(40_320), session_count: 4, harnesses: ["opencode"] },
];
const deviceIdentity = ({ id, name, platform, arch }: Device): DeviceIdentity => ({ id, name, platform, arch });

const emptyCapture: CaptureSummary = {
  status: "empty", saved_exchanges: 0, failed_exchanges: 0, pending_exchanges: 0, last_saved_at: null,
};

function harnessFixture(
  id: string,
  harness: string,
  title: string,
  model: string,
  minutesAgo: number,
  device: Device,
): Session {
  return {
    id: `ses_fixture_harness_${id}`,
    parent_session_id: null,
    started_at: iso(minutesAgo + 18),
    ended_at: iso(minutesAgo),
    state: "inactive",
    liveness: "finalized",
    last_active_at: iso(minutesAgo),
    activity_at: iso(minutesAgo),
    inactive_at: iso(minutesAgo - 1),
    harness,
    boundary: "exact",
    outcome: "landed",
    outcome_src: "agent",
    outcome_updated_at: iso(minutesAgo - 1),
    outcome_reason: `Verified the ${title} development fixture.`,
    repo: "mimir",
    source_ref: `fixtures/${id}`,
    model_primary: model,
    models: [],
    request_count: 0,
    tokens_in: 0,
    tokens_out: 0,
    title: `${title} harness fixture`,
    title_source: "harness",
    title_updated_at: iso(minutesAgo),
    display_title: `${title} harness fixture`,
    intent: `Exercise the dashboard session presentation for ${title}`,
    summary_text: `Fixture session for validating ${title} identity, model, capture, and outcome presentation.`,
    summary_status: "ready",
    summary_source: "generated",
    summary_updated_at: iso(minutesAgo),
    child_session_count: 0,
    capture: emptyCapture,
    device: deviceIdentity(device),
  };
}


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
    models: [],
    request_count: 0,
    tokens_in: 0,
    tokens_out: 0,
    title: "Restore dashboard evidence hierarchy",
    title_source: "manual",
    title_updated_at: iso(3),
    display_title: "Restore dashboard evidence hierarchy",
    intent: "Correct session evidence hierarchy, multi-model rendering, and dashboard motion without hiding implementation detail",
    summary_text: "Sample: restored result evidence hierarchy and reduced-motion handling. Two patches captured; runner reported 7 passed after a focus fix. Protected preview and push remain unverified. Recorded outcome: landed.", summary_status: "ready", summary_source: "reconstructed:sample", summary_updated_at: iso(5),
    child_session_count: 2,
    capture: emptyCapture,
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
    summary_text: null, summary_status: "pending", summary_source: null, summary_updated_at: null,
    child_session_count: 0,
    capture: emptyCapture,
    device: deviceIdentity(devices[1]),
  },
  harnessFixture("pi", "pi", "Pi", "anthropic/claude-sonnet-4.5", 8, devices[0]),
  harnessFixture("oh_my_pi", "oh-my-pi", "Oh My Pi", "openai/gpt-5.6-sol", 10, devices[0]),
  harnessFixture("claude_code", "claude-code", "Claude Code", "anthropic/claude-opus-4.1", 12, devices[0]),
  harnessFixture("codex", "codex", "Codex", "openai/gpt-5.4", 14, devices[1]),
  harnessFixture("cursor", "cursor", "Cursor", "google/gemini-2.5-pro", 16, devices[0]),
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
    summary_text: "The session prototyped synchronization behavior, but the approach was discarded after ownership validation.", summary_status: "ready", summary_source: "generated", summary_updated_at: iso(1100),
    child_session_count: 0,
    capture: emptyCapture,
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
    summary_text: null, summary_status: "unavailable", summary_source: "generated", summary_updated_at: iso(2800),
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
    summary_text: null, summary_status: "ready", summary_source: "generated", summary_updated_at: iso(20),
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
    summary_text: null, summary_status: "ready", summary_source: "generated", summary_updated_at: iso(30),
    device: deviceIdentity(devices[0]),
  },
  {
    id: "ses_fixture_supporting_tooling",
    parent_session_id: "ses_fixture_supporting_review",
    started_at: iso(53),
    ended_at: iso(50),
    state: "inactive",
    last_active_at: iso(50),
    inactive_at: iso(49),
    harness: "goose",
    boundary: "exact",
    outcome: "landed",
    outcome_src: "agent",
    outcome_updated_at: iso(49),
    outcome_reason: "Traced the capture bound regression to the evidence pipeline.",
    repo: "mimir",
    source_ref: "feature/dashboard-evidence",
    model_primary: "qwen/qwen3-coder",
    models: [{ name: "qwen/qwen3-coder", request_count: 1, first_seen_at: iso(53), last_seen_at: iso(50) }],
    request_count: 1,
    tokens_in: 14_600,
    tokens_out: 1_740,
    title: null,
    title_source: null,
    title_updated_at: null,
    display_title: "Trace patch capture bounds across the evidence pipeline",
    intent: "Trace patch capture bounds across the evidence pipeline",
    summary_text: null, summary_status: "ready", summary_source: "generated", summary_updated_at: iso(20),
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
const sessionExchanges = fixtureSessionExchanges;

const exchanges: Array<Exchange & { tool_names: string[]; has_errors: boolean }> = sessionExchanges.map((exchange) => ({
  ...exchange,
  endpoint: "/v1/chat/completions",
  repo: exchange.session_id.includes("harness_pi") || exchange.session_id.includes("oh_my_pi") ? "sample-cli" : "mimir",
  access_token_label: "sample-machine-no-credentials",
  r2_key: `fixtures/${exchange.id}.json`,
}));
sessions.push(
  { ...harnessFixture("local_health", "codex", "Local readiness review", "openai/gpt-5.4", 30, devices[1]), id: "ses_fixture_unknown_repo", repo: null, source_ref: "local/health", outcome: "unresolved", outcome_src: null, outcome_reason: null },
  { ...harnessFixture("local_health_other", "pi", "Separate local repository", "anthropic/claude-sonnet-4.5", 31, devices[0]), id: "ses_fixture_unknown_repo_other", repo: null, source_ref: "local/health", outcome: "unresolved", outcome_src: null, outcome_reason: null },
  { ...harnessFixture("capture_failure", "opencode", "Archive failure receipt", "openai/gpt-5.6-sol", 45, devices[1]), id: "ses_fixture_capture_failed", outcome: "unresolved", outcome_src: null, outcome_reason: null, summary_text: "Sample archive upload failed; no response archive or work outcome is available." },
);

const sampleDescriptions: Record<string, { title: string; summary: string; reason: string }> = {
  pi: { title: "Sample: signal unready services with a useful exit code", summary: "Read health response and changed the sample CLI readiness exit status. Focused output reported exit status 1 for ready:false.", reason: "Readiness exit status change was retained in the sample project." },
  oh_my_pi: { title: "Sample: clean release scripts and binary assets", summary: "Captured release cleanup with a rename, deletion, executable-bit change, and binary asset update. No deployment was attempted.", reason: "Release cleanup was retained; no deployment was attempted." },
  claude_code: { title: "Sample: review the captured evidence hierarchy commit", summary: "Reviewed the same commit from another session. The review capture contains a narrower patch; both variants remain inspectable.", reason: "Completed the evidence review; this is not a new implementation." },
  codex: { title: "Sample: document a rejected ownership experiment", summary: "Recorded the ownership violation and discarded outcome. No replacement implementation is claimed.", reason: "Review notes were retained, not the rejected synchronization prototype." },
  cursor: { title: "Sample: inspect an unsupported vendor capture", summary: "Unsupported request and response bodies remain available in the raw archive. Structured reconstruction is incomplete.", reason: "Kept the raw capture for inspection; no implementation change is claimed." },
};
for (const session of sessions) {
  const key = session.id.replace("ses_fixture_harness_", "");
  const description = sampleDescriptions[key];
  if (description) {
    session.title = session.display_title = description.title;
    session.intent = description.title.replace("Sample: ", "");
    session.summary_text = description.summary;
    session.outcome_reason = description.reason;
  }
  if (key === "pi" || key === "oh_my_pi") session.repo = "sample-cli";
  if (session.id.startsWith("ses_fixture_unknown_repo")) {
    session.intent = "Inspect a sample captured commit from a local repository without a configured remote";
    session.summary_text = "Sample patch is available, but no conversation was captured and no outcome is recorded.";
  }
  if (session.title && !session.title.startsWith("Sample: ")) session.title = `Sample: ${session.title}`;
  if (session.display_title && !session.display_title.startsWith("Sample: ")) session.display_title = `Sample: ${session.display_title}`;
}


const allSessions = [...sessions, ...supportingSessions];
for (const session of allSessions) {
  const own = sessionExchanges.filter((exchange) => exchange.session_id === session.id);
  session.request_count = own.length;
  session.tokens_in = own.reduce((sum, row) => sum + row.input_tokens, 0);
  session.tokens_out = own.reduce((sum, row) => sum + row.output_tokens, 0);
  const names = [...new Set(own.map((row) => row.model))];
  session.models = names.map((name) => {
    const rows = own.filter((row) => row.model === name).sort((a, b) => a.ts.localeCompare(b.ts));
    return { name, request_count: rows.length, first_seen_at: rows[0]?.ts ?? null, last_seen_at: rows.at(-1)?.ts ?? null };
  });
  session.model_primary = names[0] ?? null;
  if (session.display_title && !session.display_title.startsWith("Sample: ")) session.display_title = `Sample: ${session.display_title}`;
  session.summary_source = session.summary_text ? "reconstructed:sample" : session.summary_source;
  if ("capture" in session) session.capture = captureFor(new Set([session.id, ...descendantsFor(session.id).map((node) => node.id)]));
}
for (const device of devices) device.session_count = allSessions.filter((session) => session.device?.id === device.id).length;
const sessionRecord = (id: string): Session | undefined => {
  const item = allSessions.find((entry) => entry.id === id);
  return item ? asSession(item) : undefined;
};

// Supporting fixtures omit the list/detail-only fields; complete them so a
// sub-agent opens as a full session row or detail page.
function asSession(item: Session | SessionDetail["supporting_sessions"][number]): Session {
  if ("capture" in item && "liveness" in item) return item as Session;
  const partial = item as SessionDetail["supporting_sessions"][number];
  return {
    ...partial,
    activity_at: partial.last_active_at ?? partial.started_at,
    liveness: "finalized",
    capture: captureFor(new Set([partial.id, ...descendantsFor(partial.id).map((node) => node.id)])),
    child_session_count: supportingSessions.filter((child) => child.parent_session_id === partial.id).length,
  };
}

function descendantsFor(rootId: string): SessionDetail["supporting_sessions"] {
  const visited = new Set([rootId]);
  const descendants: SessionDetail["supporting_sessions"] = [];
  const pending = [rootId];
  while (pending.length) {
    const parentId = pending.pop()!;
    for (const child of supportingSessions) {
      if (child.parent_session_id !== parentId || visited.has(child.id)) continue;
      visited.add(child.id);
      descendants.push(child);
      pending.push(child.id);
    }
  }
  return descendants;
}

type ArtifactRow = Awaited<typeof fixtureArtifactRows>[number];

function captureFor(ids: Set<string>): CaptureSummary {
  const rows = sessionExchanges.filter((row) => ids.has(row.session_id));
  const saved = rows.filter((row) => row.capture_status === "saved");
  const failed = rows.filter((row) => row.capture_status === "failed").length;
  const pending = rows.filter((row) => row.capture_status === "accepted").length;
  return {
    status: pending ? "pending" : saved.length && failed ? "partial" : failed ? "failed" : saved.length ? "saved" : "empty",
    saved_exchanges: saved.length, failed_exchanges: failed, pending_exchanges: pending,
    last_saved_at: saved.map((row) => row.ts).sort().at(-1) ?? null,
  };
}

function rootFor(id: string): string {
  let session = sessionRecord(id);
  const seen = new Set<string>();
  while (session?.parent_session_id && !seen.has(session.id)) {
    seen.add(session.id);
    session = sessionRecord(session.parent_session_id);
  }
  return session?.id ?? id;
}

function detailFor(session: Session, artifacts: ArtifactRow[]): SessionDetail {
  const { capture: _capture, liveness: _liveness, ...detailSession } = session;
  const ids = new Set([session.id, ...descendantsFor(session.id).map((node) => node.id)]);
  const capture = captureFor(ids);
  const rich = session.id === sessions[0].id;
  const turns = fixtureTurns.filter((turn) => ids.has(turn.session));
  const errorTurns = turns.filter((turn) => turn.error);
  const ownArtifacts = artifacts.filter((row) => row.sessionId === rootFor(session.id)).map((row) => row.artifact);
  const partial = session.state === "active" || capture.status !== "saved" || turns.some((turn) => turn.id === "req_fixture_unknown_shape");
  return {
    session: detailSession, capture, supporting_sessions: descendantsFor(session.id),
    outcome_events: eventsFor(session.id),
    files: rich ? ["worker/web/src/components/session/SessionHeader.vue", "worker/web/src/styles.css"] : [...new Set(turns.flatMap((turn) => turn.activity?.flatMap((tool) => typeof tool.input.path === "string" ? [tool.input.path] : []) ?? []))],
    errors: errorTurns.map((turn) => ({ signature: turn.error!, count: 1, first_seen_at: iso(turn.ago), last_seen_at: iso(turn.ago), latest_exchange_id: turn.id })),
    git_artifacts: ownArtifacts,
    summary: {
      goal: session.intent ?? null,
      actions: rich ? ["Moved result evidence out of the model selector and retained supporting-session links.", "Captured two sample commits covering evidence hierarchy and reduced-motion behavior.", "Fixed focus restoration after a failed focused check."] : turns.filter((turn) => !turn.capture || turn.capture === "saved").slice(0, 3).map((turn) => `Recorded response: ${turn.responseText}`),
      result: session.outcome === "unresolved" ? null : `Recorded outcome: ${session.outcome}${session.outcome_reason ? ` — ${session.outcome_reason}` : ""}`,
      verification: rich ? ["Recorded focused runner output: 7 passed, 0 failed after the focus fix.", "Recorded local keyboard and reduced-motion preview review."] : [],
      unresolved: rich ? ["Protected remote preview was not verified.", "No push receipt was supplied; captured patches do not establish shipment."] : [...(partial ? ["Capture or structured reconstruction is incomplete."] : []), ...(session.outcome === "unresolved" ? ["No final work outcome has been recorded."] : [])],
      partial, source: "reconstructed",
      evidence: rich ? [
        { section: "actions", index: 1, href: "#changes-heading", label: "Captured sample commits" },
        { section: "verification", index: 0, href: "/requests/req_fixture_10", label: "Focused check output" },
        { section: "unresolved", index: 0, href: "/requests/req_fixture_05", label: "Protected preview error" },
        { section: "result", index: 0, href: "#outcome-heading", label: "Recorded outcome" },
      ] : [],
    },
  };
}

const changedOutcomeEvents = new Map<string, OutcomeEvent[]>();

function eventsFor(id: string): OutcomeEvent[] {
  const changed = changedOutcomeEvents.get(id) ?? [];
  if (id === sessions[0].id) return [...changed, ...outcomeEvents];
  if (changed.length) return changed;
  const session = sessionRecord(id);
  return [...changed, ...(session?.outcome_src ? [{
    id: `out_sample_${id}`, outcome: session.outcome, source: session.outcome_src,
    reason: session.outcome_reason, evidence_json: JSON.stringify({ note: "Fictional sample outcome; not production evidence." }),
    created_at: session.outcome_updated_at ?? session.started_at,
  }] : [])];
}

function filteredExchanges(params: URLSearchParams, ids?: Set<string>) {
  const kind = params.get("request_kind");
  const capture = params.get("capture_status");
  if (kind && !["primary", "title", "summary", "compaction"].includes(kind)) throw new Error("Invalid fixture request kind.");
  if (capture && !["accepted", "saved", "failed", "skipped"].includes(capture)) throw new Error("Invalid fixture capture status.");
  if (params.has("errors") && params.get("errors") !== "true") throw new Error("Invalid fixture error filter.");
  const needle = (params.get("q") ?? "").toLowerCase();
  return exchanges.filter((row) => {
    if (ids && !ids.has(row.session_id)) return false;
    if (!fixtureDateMatches(row.ts, params)) return false;
    for (const [parameter, field] of [["repo", "repo"], ["session", "session_id"], ["model", "model"], ["provider", "provider"], ["app", "harness"], ["finish_reason", "finish_reason"], ["request_kind", "request_kind"], ["capture_status", "capture_status"]] as const) {
      const value = params.get(parameter);
      if (value && row[field] !== value) return false;
    }
    const tool = params.get("tool");
    if (tool && (row.capture_status !== "saved" || !row.tool_names.includes(tool))) return false;
    if (params.get("errors") === "true" && !(row.capture_status === "failed" || row.capture_status === "saved" && row.has_errors)) return false;
    return !needle || [row.id, row.session_id, row.repo, row.model, row.provider, row.harness, row.request_excerpt, row.response_excerpt].some((field) => field?.toLowerCase().includes(needle));
  });
}

function facetsFor(ids?: Set<string>): Facets {
  const nodes = ids ? allSessions.filter((session) => ids.has(session.id)) : allSessions;
  const rows = sessionExchanges.filter((row) => row.capture_status === "saved" && (!ids || ids.has(row.session_id)));
  return {
    repos: [...new Set(nodes.flatMap((session) => session.repo ? [session.repo] : []))].sort(),
    apps: [...new Set(nodes.flatMap((session) => session.harness ? [session.harness] : []))].sort(),
    models: [...new Set([...nodes.flatMap((session) => session.models.map((model) => model.name)), ...rows.map((row) => row.model)])].sort(),
    providers: [...new Set(rows.flatMap((row) => row.provider ? [row.provider] : []))].sort(),
    finish_reasons: [...new Set(rows.flatMap((row) => row.finish_reason ? [row.finish_reason] : []))].sort(),
    tools: [...new Set(rows.flatMap((row) => row.tool_names))].sort(),
  };
}

export async function fixtureRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  if (init.signal?.aborted) throw new DOMException("Aborted", "AbortError");
  const url = new URL(path, "https://mimir.fixture");
  const segments = url.pathname.split("/").filter(Boolean).map(decodeURIComponent);
  const api = segments[0] === "dashboard" && segments[1] === "api";
  const method = init.method ?? "GET";
  const params = url.searchParams;
  const artifacts = await fixtureArtifactRows;
  if (init.signal?.aborted) throw new DOMException("Aborted", "AbortError");

  // Archive namespace must never be mistaken for the request-metadata endpoint.
  if (segments.length === 4 && segments[0] === "dashboard" && segments[1] === "dev-fixtures" && segments[2] === "log" && method === "GET") {
    const envelope = fixtureEnvelope(segments[3]!);
    if (!envelope) throw new Error("Fixture archive unavailable: capture has not been saved.");
    return clone(envelope) as T;
  }
  if (url.pathname === "/dashboard/api/identity" && method === "GET") return clone({ email: "developer@example.invalid", name: "Sample Developer", source: "local-development" } satisfies DashboardIdentity) as T;
  if (url.pathname === "/dashboard/api/devices" && method === "GET") return clone({ devices }) as T;
  if (api && segments[2] === "devices" && (segments.length === 4 && method === "PATCH" || segments.length === 5 && segments[4] === "revoke" && method === "POST")) {
    const device = devices.find((item) => item.id === segments[3]);
    if (!device) throw new Error("Fixture device not found.");
    if (method === "PATCH") {
      const body = JSON.parse(String(init.body ?? "{}")) as { name?: string };
      device.name = body.name?.trim() || device.name;
    } else device.revoked_at = now.toISOString();
    device.updated_at = now.toISOString();
    return clone({ device }) as T;
  }
  if (method === "GET" && ["/dashboard/api/commits", "/dashboard/api/commits/captures", "/dashboard/api/commits/repositories", "/dashboard/api/commits/refs"].includes(url.pathname)) {
    const captures: CommitCapture[] = artifacts.map(({ sessionId, artifact }) => {
      const session = sessionRecord(sessionId)!;
      return { ...artifact, session_id: session.id, session_title: session.display_title ?? session.intent ?? session.id, repo: session.repo, outcome: session.outcome };
    });
    if (url.pathname === "/dashboard/api/commits/repositories") return clone(fixtureRepositoryPage(captures, params)) as T;
    if (url.pathname === "/dashboard/api/commits/refs") return clone(fixtureRefPage(captures, params)) as T;
    if (url.pathname === "/dashboard/api/commits/captures") {
      const repo = params.get("repo");
      const sha = params.get("commit");
      if (!repo || !sha) throw new Error("Repository and commit are required.");
      const cursor = params.get("cursor");
      const matching = captures.filter((capture) => fixtureRepositoryKey(capture.repository_url, capture.session_id) === repo && capture.commit_sha === sha && (!cursor || capture.session_id > cursor)).sort((a, b) => a.session_id.localeCompare(b.session_id));
      const limit = Math.min(50, Math.max(1, Math.trunc(Number(params.get("limit")) || 50)));
      const page = matching.slice(0, limit);
      return clone({ captures: page, next_cursor: matching.length > limit ? page.at(-1)!.session_id : null }) as T;
    }
    return clone(fixtureCommitPage(captures, params)) as T;
  }
  if (url.pathname === "/dashboard/api/sessions" && method === "GET") {
    const needle = (params.get("q") ?? "").toLowerCase();
    const filtered = sessions.filter((session) => {
      const ids = new Set([session.id, ...descendantsFor(session.id).map((node) => node.id)]);
      const nodes = allSessions.filter((node) => ids.has(node.id));
      const rows = sessionExchanges.filter((row) => ids.has(row.session_id));
      if (!fixtureDateMatches(session.started_at, params)) return false;
      for (const [parameter, field] of [["repo", "repo"], ["outcome", "outcome"], ["state", "state"]] as const) {
        const value = params.get(parameter);
        if (value && session[field] !== value) return false;
      }
      if (params.get("capture") && captureFor(ids).status !== params.get("capture")) return false;
      if (params.get("provider") && !rows.some((row) => row.capture_status === "saved" && row.provider === params.get("provider"))) return false;
      if (params.get("errors") === "true" && !rows.some((row) => row.capture_status === "failed" || row.has_errors)) return false;
      if (params.get("commits") === "true" && !artifacts.some((row) => ids.has(row.sessionId))) return false;
      if (params.get("app") && !nodes.some((node) => node.harness === params.get("app"))) return false;
      if (params.get("model") && !nodes.some((node) => node.models.some((model) => model.name === params.get("model")) || node.model_primary === params.get("model"))) return false;
      return !needle || nodes.some((node) => [node.id, node.title, node.display_title, node.intent, node.summary_text, node.repo, node.harness, ...node.models.map((model) => model.name)].some((field) => field?.toLowerCase().includes(needle)));
    });
    const { page, next_cursor } = fixtureRowPage(filtered, params, (row) => row.activity_at, 25, true);
    return clone({ sessions: page, descendants: page.flatMap((session) => descendantsFor(session.id).map(asSession)), next_cursor }) as T;
  }
  if (url.pathname === "/dashboard/api/sessions/outcomes" && method === "POST") {
    const body = JSON.parse(String(init.body ?? "{}")) as { session_ids?: string[]; outcome: Outcome; reason?: string };
    const ids = [...new Set(body.session_ids ?? [])];
    const selected = ids.map((id) => {
      const session = allSessions.find((item) => item.id === id);
      if (!session) throw new Error("Fixture session not found.");
      return session;
    });
    for (const session of selected) recordOutcome(session, body);
    return clone({ updated: selected.map((session) => ({ id: session.id, outcome: session.outcome })) }) as T;
  }
  if (api && segments[2] === "sessions" && segments[3]) {
    const id = segments[3];
    const session = sessionRecord(id);
    if (!session) throw new Error("Fixture session not found.");
    if (segments.length === 7 && segments[4] === "git-artifacts" && segments[6] === "patch" && method === "GET") {
      const row = artifacts.find((item) => item.sessionId === rootFor(id) && item.artifact.commit_sha === segments[5]);
      if (!row || row.artifact.capture_status !== "saved") throw new Error("Fixture git artifact patch unavailable.");
      return row.patch as T;
    }
    if (segments.length === 5 && segments[4] === "outcome" && method === "POST") {
      const body = JSON.parse(String(init.body ?? "{}")) as { outcome: Outcome; reason?: string; evidence?: OutcomeEvidence };
      recordOutcome(allSessions.find((item) => item.id === id)!, body);
      return clone({ id, outcome: body.outcome }) as T;
    }
    if (segments.length === 5 && segments[4] === "title" && method === "PATCH") {
      const body = JSON.parse(String(init.body ?? "{}")) as { title: string };
      const target = allSessions.find((item) => item.id === id)!;
      target.title = body.title.trim();
      target.title_source = "manual";
      target.title_updated_at = now.toISOString();
      target.display_title = target.title || target.intent || null;
      return clone({ session: { id, title: target.title, title_source: target.title_source, title_updated_at: target.title_updated_at, display_title: target.display_title } }) as T;
    }
    if (segments.length === 5 && segments[4] === "exchanges" && method === "GET") {
      const subtree = new Set([id, ...descendantsFor(id).map((node) => node.id)]);
      const selected = params.get("session");
      if (selected && !subtree.has(selected)) throw new Error("Selected session is outside this fixture subtree.");
      const ids = selected ? new Set([selected]) : subtree;
      const { page, next_cursor } = fixtureRowPage(filteredExchanges(params, ids), params, (row) => row.ts);
      return clone({ exchanges: page.map((row) => sessionExchanges.find((item) => item.id === row.id)!), next_cursor }) as T;
    }
    if (segments.length === 5 && segments[4] === "object-state" && method === "GET") {
      return clone({ session_id: id, parent_session_id: session.parent_session_id, liveness: session.liveness, harness: session.harness, repo: session.repo, started_at: session.started_at, last_event_at: session.last_active_at ?? session.started_at, finalized_at: session.liveness === "finalized" ? session.ended_at : null, end_reason: session.liveness === "finalized" ? "sample" : null, turn_count: session.request_count, tokens_in: session.tokens_in, tokens_out: session.tokens_out } satisfies SessionObjectState) as T;
    }
    if (segments.length === 4 && method === "GET") return clone(detailFor(session, artifacts)) as T;
  }
  if (url.pathname === "/dashboard/api/facets" && method === "GET") {
    const id = params.get("session");
    if (id && !sessionRecord(id)) throw new Error("Fixture session not found.");
    const ids = id ? new Set([id, ...(params.get("scope") === "own" ? [] : descendantsFor(id).map((node) => node.id))]) : undefined;
    return clone(facetsFor(ids)) as T;
  }
  if (url.pathname === "/dashboard/api/log" && method === "GET") {
    const { page, next_cursor } = fixtureRowPage(filteredExchanges(params), params, (row) => row.ts, 50);
    return clone({ exchanges: page, next_cursor }) as T;
  }
  if (api && segments.length === 4 && segments[2] === "log" && method === "GET") {
    const exchange = exchanges.find((item) => item.id === segments[3]);
    if (!exchange) throw new Error("Fixture request not found.");
    return clone({ exchange, log_url: `/dashboard/dev-fixtures/log/${exchange.id}` }) as T;
  }
  if (url.pathname === "/dashboard/api/overview" && method === "GET") {
    const counts = (field: "model" | "provider" | "harness") => [...new Set(exchanges.flatMap((row) => row[field] ? [row[field]!] : []))].map((name) => ({ name, requests: exchanges.filter((row) => row[field] === name).length }));
    return clone({
      totals: { requests: exchanges.length, sessions: sessions.length, saved_exchanges: exchanges.filter((row) => row.capture_status === "saved").length, capture_failures: exchanges.filter((row) => row.capture_status === "failed").length, input_tokens: exchanges.reduce((sum, row) => sum + row.input_tokens, 0), output_tokens: exchanges.reduce((sum, row) => sum + row.output_tokens, 0) },
      models: counts("model"), providers: counts("provider"), apps: counts("harness"),
    } satisfies Overview) as T;
  }
  throw new Error(`No dashboard fixture for ${method} ${url.pathname}`);
}

function recordOutcome(session: Session | SessionDetail["supporting_sessions"][number], body: { outcome: Outcome; reason?: string; evidence?: OutcomeEvidence }) {
  const events = changedOutcomeEvents.get(session.id) ?? (session.id === sessions[0].id ? [] : eventsFor(session.id));
  session.outcome = body.outcome;
  session.outcome_reason = body.reason ?? null;
  session.outcome_src = "user";
  session.outcome_updated_at = now.toISOString();
  events.unshift({ id: `out_sample_${session.id}_${events.length}`, outcome: body.outcome, source: "user", reason: body.reason ?? null, evidence_json: body.evidence ? JSON.stringify(body.evidence) : null, created_at: now.toISOString() });
  changedOutcomeEvents.set(session.id, events);
}
