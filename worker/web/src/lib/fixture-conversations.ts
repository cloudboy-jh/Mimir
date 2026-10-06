import type { LogEnvelope, SessionExchange } from "./api";

// Deliberately fictional, deterministic examples. No production logs or credentials.
export const fixtureReference = new Date("2026-10-05T17:45:00.000Z");
export const fixtureIso = (minutesAgo: number) => new Date(fixtureReference.getTime() - minutesAgo * 60_000).toISOString();
const root = "ses_fixture_multi_model_result";
const model = "openai/gpt-5.6-sol";
const user = (content: string) => ({ role: "user", content });
const assistant = (content: string) => ({ role: "assistant", content });
const partMessage = (role: string, text: string) => ({ role, parts: [{ type: "text", text }] });
const goal = "Sample project: restore the session evidence hierarchy. Keep commits, conversation, and raw captures inspectable; do not treat a captured patch as proof it shipped.";
const plan = "The result evidence is present, but the page makes you open model details to find it. I will follow the data from the session response to the header before changing the layout.\n\nThe fix should be structural: give captured changes their own section, keep every model identity available, and preserve the links back to the raw requests. I will check keyboard order locally; a captured patch alone is not proof of a push.\n\nThis is a synthetic coding discussion in the fictional example/mimir repository.";
const readCall = { type: "tool", callID: "call_sample_read_header", tool: "read", state: { status: "completed", input: { path: "worker/web/src/components/session/SessionHeader.vue" }, output: "Result evidence is nested inside the model selector; the parent only renders one model." } };
const editCall = { type: "tool", callID: "call_sample_edit_header", tool: "edit", state: { status: "completed", input: { path: "worker/web/src/components/session/SessionHeader.vue" }, output: "Moved evidence to its own section and retained all model identities." } };
const initial = [partMessage("user", goal), partMessage("assistant", plan)];
const inspected = [...initial, { role: "assistant", parts: [readCall] }, partMessage("user", "Please preserve the existing keyboard order and show supporting sessions separately.")];
const implemented = [...inspected, { role: "assistant", parts: [editCall] }, partMessage("assistant", "The result section is independent now. A second commit will handle overlay motion.")];

type FixtureTurn = {
  id: string; session: string; ago: number; request: unknown; response: LogEnvelope["response"];
  requestText: string; responseText: string; model?: string; harness?: string; kind?: "primary" | "title" | "summary" | "compaction";
  capture?: "saved" | "accepted" | "failed"; toolNames?: string[]; error?: string; finish?: string;
  activity?: Array<{ name: string; input: Record<string, unknown>; status: "succeeded" | "failed"; output?: unknown }>;
};
const json = (body: unknown): LogEnvelope["response"] => ({ format: "json", body });
const openai = (content: string) => json({ choices: [{ message: assistant(content), finish_reason: "stop" }] });
const turns: FixtureTurn[] = [
  { id: "req_fixture_01", session: root, ago: 77, request: { model, messages: [partMessage("user", goal)] }, response: json({ messages: [partMessage("assistant", plan)] }), requestText: goal, responseText: plan },
  { id: "req_fixture_02", session: root, ago: 71, request: { model, messages: initial }, response: json({ messages: [{ role: "assistant", parts: [readCall] }] }), requestText: "Trace result evidence placement in SessionHeader.vue", responseText: "Result evidence is nested inside the model selector.", toolNames: ["read"], finish: "tool-calls" },
  { id: "req_fixture_03", session: root, ago: 66, request: { model, messages: inspected }, response: json({ messages: [{ role: "assistant", parts: [editCall] }] }), requestText: "Preserve keyboard order and supporting session hierarchy", responseText: "Moved evidence to its own section and retained all model identities.", toolNames: ["edit"], finish: "tool-calls" },
  { id: "req_fixture_04", session: root, ago: 60, request: { model, messages: implemented }, response: json({ messages: [partMessage("assistant", "### Evidence is no longer hidden under a model\n\nThe header was using model details as the container for both model identity and work evidence. That made a multi-model session look like it belonged to only one model, and hid the most useful part of the record behind an unrelated control.\n\nThe panel now has separate **result**, models, and supporting-session sections. The result section consumes the same session evidence; it does not infer success from a saved capture.\n\n```vue\n<section aria-labelledby=\"result-evidence-heading\">\n  <h2 id=\"result-evidence-heading\">Captured changes</h2>\n  <SessionChanges :session=\"session\" />\n</section>\n\n<SessionModels :models=\"session.models\" />\n<SupportingSessions :sessions=\"session.supporting_sessions\" />\n```\n\nThe read and edit outputs above describe the synthetic change. Keyboard order still needs a browser review, and overlay motion is a separate check. A captured commit still does not prove a push.\n\nSample safety input, rendered as text rather than executable markup: <script>alert('sample')</script>")] }), requestText: "Explain the evidence layout change", responseText: "Separated result, model, and supporting-session sections; captured commit does not prove push." },
  { id: "req_fixture_05", session: root, ago: 57, request: { model, messages: [user("Check Access-protected preview before the browser review.")] }, response: json({ messages: [{ role: "assistant", parts: [{ type: "tool", callID: "call_sample_preview", tool: "browser", state: { status: "error", input: { url: "https://preview.example.invalid/dashboard" }, error: "Cloudflare Access authentication required." } }] }] }), requestText: "Check Access-protected preview", responseText: "Cloudflare Access authentication required.", toolNames: ["browser"], error: "Cloudflare Access authentication required.", finish: "tool-calls" },
  { id: "req_fixture_06", session: root, ago: 51, request: { model, messages: [user("Use the local fixture preview; do not bypass Access.")] }, response: json({ messages: [partMessage("assistant", "The local preview shows the result before the model tree. Keyboard focus reaches the patch link, then the supporting sessions. The protected remote preview remains unverified.")] }), requestText: "Use local fixture preview without bypassing Access", responseText: "Local layout and keyboard focus reviewed; protected remote preview unverified." },
  { id: "req_fixture_title", session: root, ago: 50, kind: "title", request: { model, messages: [user("Generate a short session title from the supplied conversation.")] }, response: openai("Restore dashboard evidence hierarchy"), requestText: "Generate session title", responseText: "Restore dashboard evidence hierarchy" },
  { id: "req_fixture_07", session: root, ago: 44, request: { model, messages: [user("Capture the first commit; the fixture repository is https://github.com/example/mimir.")] }, response: openai("Captured 7ad8d9e43a61c59fe22379f8e5ca68dbe8c41120: restore result evidence hierarchy. Patch saved; no push receipt was supplied."), requestText: "Capture first evidence hierarchy commit", responseText: "Patch saved for 7ad8d9e4; no push receipt supplied.", toolNames: ["git"], activity: [{ name: "git", input: { command: "git show --format=fuller 7ad8d9e4" }, status: "succeeded", output: "Sample commit: restore result evidence hierarchy" }] },
  { id: "req_fixture_compaction", session: root, ago: 38, kind: "compaction", request: { model, messages: [user("Compact the conversation while retaining outstanding verification and commit evidence.")] }, response: openai("Context checkpoint: result hierarchy fixed; first patch saved. Still inspect overlay reduced-motion behavior. Access preview unverified. No push receipt."), requestText: "Compact conversation context", responseText: "Context checkpoint: first patch saved; overlay review and remote preview remain open." },
  { id: "req_fixture_08", session: root, ago: 34, model: "google/gemini-2.5-pro-preview-06-05", request: { model: "google/gemini-2.5-pro-preview-06-05", messages: [user("Inspect overlay reduced-motion behavior after the context checkpoint.")] }, response: { format: "reconstructed_sse", content: "Use the shared presence transition and disable animation under prefers-reduced-motion.", events: [ { choices: [{ delta: { role: "assistant", content: "Use the shared presence transition " } }] }, { choices: [{ delta: { content: "and disable animation under prefers-reduced-motion." }, finish_reason: "stop" }] } ] }, requestText: "Inspect overlay reduced-motion behavior", responseText: "Use shared presence transitions and respect prefers-reduced-motion." },
  { id: "req_fixture_09", session: root, ago: 28, model: "anthropic/claude-opus-4.1-thinking", request: { model: "anthropic/claude-opus-4.1-thinking", messages: [user("Run the focused overlay checks.")] }, response: openai("The runner reports 6 passed, 1 failed: focus returned to the page instead of the trigger. I will fix the focus restore before recording verification."), requestText: "Run focused overlay checks", responseText: "Runner: 6 passed, 1 failed; focus restoration needs correction.", toolNames: ["bash"], error: "Focus did not return to overlay trigger", activity: [{ name: "bash", input: { command: "npm run test -- overlay-focus" }, status: "failed", output: "6 passed, 1 failed: expected trigger focus" }] },
  { id: "req_fixture_10", session: root, ago: 21, model: "anthropic/claude-opus-4.1-thinking", request: { model: "anthropic/claude-opus-4.1-thinking", messages: [user("Fix focus restoration and repeat the same checks.")] }, response: openai("Runner output: 7 passed, 0 failed. Reduced-motion preview showed no animated transition. Captured the second patch; this does not establish that either commit was pushed."), requestText: "Fix focus restoration and rerun focused checks", responseText: "Runner output: 7 passed; reduced-motion local preview reviewed.", toolNames: ["edit", "bash"], activity: [{ name: "edit", input: { path: "worker/web/src/styles.css" }, status: "succeeded", output: "Updated overlay transition" }, { name: "bash", input: { command: "npm run test -- overlay-focus" }, status: "succeeded", output: "7 passed, 0 failed" }] },
  { id: "req_fixture_11", session: root, ago: 12, request: { model, messages: [user("Please verify once more.")] }, response: openai("Local keyboard review completed. The protected preview is still unverified."), requestText: "Please verify once more.", responseText: "Local keyboard review completed. The protected preview is still unverified." },
  { id: "req_fixture_12", session: root, ago: 7, request: { model, messages: [user("Please verify once more.")] }, response: openai("Local keyboard review completed. The protected preview is still unverified."), requestText: "Please verify once more.", responseText: "Local keyboard review completed. The protected preview is still unverified." },
  { id: "req_fixture_summary", session: root, ago: 6, kind: "summary", request: { model, messages: [user("Summarize only the supplied evidence.")] }, response: openai("Two captured patches; local focus checks reported success. Remote preview and push not established."), requestText: "Summarize supplied evidence", responseText: "Two captured patches; local checks reported success; remote preview unverified." },
  { id: "req_fixture_sub_01", session: "ses_fixture_supporting_review", ago: 55, model: "anthropic/claude-opus-4.1-thinking", request: { messages: [partMessage("user", "Audit the evidence hierarchy; do not edit the parent branch.")] }, response: json({ messages: [partMessage("assistant", "The result is hidden under model details. Move it into a sibling section; preserve raw evidence links.")] }), requestText: "Audit evidence hierarchy without editing parent branch", responseText: "Move result into sibling section and preserve raw evidence links.", toolNames: ["read"] },
  { id: "req_fixture_sub_02", session: "ses_fixture_supporting_motion", ago: 41, model: "google/gemini-2.5-pro-preview-06-05", request: { messages: [user("Please verify once more.")] }, response: openai("Reduced motion must bypass the presence animation; restore focus to the trigger on close."), requestText: "Please verify once more.", responseText: "Reduced motion should bypass presence animation; restore trigger focus." },
  { id: "req_fixture_sub_03", session: "ses_fixture_supporting_tooling", ago: 52, model: "deepseek/deepseek-v3.2", harness: "goose", request: { messages: [user("Trace patch capture bounds across the evidence pipeline.")] }, response: openai("The patch limit applies before upload. A rejected patch should retain a failed receipt, not an empty diff."), requestText: "Trace patch capture bounds", responseText: "Rejected patch should retain failed receipt, not empty diff.", toolNames: ["read"] },
];

const piGoal = "Sample task: add an explicit health-check exit status to the example CLI.";
const piCall = { role: "assistant", content: [{ type: "thinking", thinking: "An exit code should represent service readiness, not transport success." }, { type: "toolCall", id: "call_sample_pi_read", name: "read", arguments: { path: "src/health.ts" } }] };
const piResult = { role: "toolResult", toolCallId: "call_sample_pi_read", toolName: "read", content: [{ type: "text", text: "Health response: { ready: false, transport: 'ok' }" }], isError: false };
const captureQuestion = "Investigate missing direct-provider capture receipts. The adapter returns an accepted response, but the dashboard sometimes has no saved conversation yet.\n\nTrace where transport acceptance ends and durable capture begins. Explain the distinction with the smallest useful code example, and do not call an accepted turn saved.\n\nSynthetic sample: paths, code, and tool output below illustrate a fictional adapter, not a diagnosis of a production incident.";
const captureAnswer = [
  "The gap is between **accepting the provider turn** and **receiving the archive save receipt**. In this synthetic adapter, the response is returned after the capture job is queued. The authoritative receipt arrives separately, after persistence.",
  "",
  "The source inspection shows why a successful provider response is not enough: the adapter returns `accepted` before the archive worker has acknowledged the exchange. The trace shows a receipt for the earlier exchange, but only an acceptance event for the next turn.",
  "",
  "```ts",
  "type CaptureReceipt =",
  "  | { status: 'accepted'; exchangeId: string }",
  "  | { status: 'saved'; exchangeId: string; savedAt: string };",
  "",
  "async function acceptCapture(turn: ProviderTurn): Promise<CaptureReceipt> {",
  "  const exchangeId = await captureQueue.enqueue(turn);",
  "  return { status: 'accepted', exchangeId };",
  "}",
  "",
  "// Only the archive acknowledgement can produce a saved receipt.",
  "function acknowledgeArchive(exchangeId: string, savedAt: string): CaptureReceipt {",
  "  return { status: 'saved', exchangeId, savedAt };",
  "}",
  "```",
  "",
  "I would keep those states separate in the receipt handler: show pending while the job is accepted, and mark it saved only when the acknowledgement identifies the same exchange. A timeout can leave the outcome unknown; it should not silently become a successful save.",
  "",
  "The read and trace outputs are attached to their calls in this turn. This is an illustrative investigation, not an applied patch or a production verification. The next exchange still has no authoritative save receipt, so its conversation remains unavailable.",
].join("\n");
turns.push(
  { id: "req_fixture_pi_01", session: "ses_fixture_harness_pi", ago: 23, harness: "pi", model: "anthropic/claude-sonnet-4.5", request: { messages: [user(piGoal)] }, response: json(piCall), requestText: piGoal, responseText: "Inspect service readiness before choosing the health exit status.", toolNames: ["read"], finish: "tool-calls" },
  { id: "req_fixture_pi_02", session: "ses_fixture_harness_pi", ago: 16, harness: "pi", model: "anthropic/claude-sonnet-4.5", request: { messages: [user(piGoal), piCall, piResult] }, response: json({ role: "assistant", content: [{ type: "text", text: "The CLI now exits 1 when ready is false. The focused check reported exit status 1 for an unready service." }] }), requestText: "Health response ready:false with successful transport", responseText: "CLI exits 1 for unready service; focused check reports exit status 1.", toolNames: ["edit", "bash"] },
  { id: "req_fixture_omp_01", session: "ses_fixture_harness_oh_my_pi", ago: 24, harness: "oh-my-pi", request: { messages: [user("Sample task: rename the release script, delete the obsolete config, and mark the script executable.")] }, response: json({ role: "assistant", content: [{ type: "thinking", thinking: "Keep rename and mode changes visible in patch evidence." }, { type: "toolCall", id: "call_sample_omp_git", name: "bash", arguments: { command: "git diff --summary" } }] }), requestText: "Rename release script, delete obsolete config, mark executable", responseText: "Inspect rename and mode changes in patch evidence.", toolNames: ["bash"], finish: "tool-calls" },
  { id: "req_fixture_omp_02", session: "ses_fixture_harness_oh_my_pi", ago: 10, harness: "oh-my-pi", request: { messages: [user("Report release cleanup evidence."), { role: "toolResult", toolCallId: "call_sample_omp_git", toolName: "bash", content: [{ type: "text", text: "rename scripts/publish.sh => scripts/release.sh; mode 100644 => 100755; delete legacy.json" }] }] }, response: openai("The sample cleanup patch includes a rename, executable-bit change, deletion, and binary asset update. No deployment was attempted."), requestText: "Report release cleanup evidence", responseText: "Cleanup patch includes rename, mode, deletion, binary update. No deployment.", toolNames: ["bash"] },
  {
    id: "req_fixture_active_01", session: "ses_fixture_active_capture", ago: 13, harness: "hermes", model: "anthropic/claude-sonnet-4.5",
    request: { messages: [
      user(captureQuestion),
      assistant("I will inspect the adapter's return path and compare it with the receipt trace. The important question is which event actually authorizes the dashboard to say saved."),
      { role: "assistant", content: [
        { type: "tool_use", id: "call_sample_capture_read", name: "read", input: { path: "src/capture/direct-provider.ts", lines: "42-58" } },
      ] },
      { role: "toolResult", toolCallId: "call_sample_capture_read", toolName: "read", content: "Synthetic source excerpt, src/capture/direct-provider.ts:42-58\nconst exchangeId = await captureQueue.enqueue(turn);\nreturn { status: 'accepted', exchangeId };\n// Archive worker emits status: 'saved' after persistence.", isError: false },
      { role: "assistant", content: [
        { type: "tool_use", id: "call_sample_capture_trace", name: "bash", input: { command: "cat samples/direct-provider-receipts.jsonl" } },
      ] },
      { role: "toolResult", toolCallId: "call_sample_capture_trace", toolName: "bash", content: "Synthetic receipt trace\n17:32:00 accepted exchange=req_fixture_active_01\n17:32:02 saved exchange=req_fixture_active_01\n17:44:00 accepted exchange=req_fixture_active_pending\nNo saved receipt recorded for req_fixture_active_pending.", isError: false },
    ] },
    response: { format: "reconstructed_sse", content: captureAnswer, events: [
      { type: "content_block_start", index: 0, content_block: { type: "thinking", thinking: "Transport acceptance is not persistence. Check which exchange the receipt identifies before assigning a saved state." } },
      { type: "content_block_stop", index: 0 },
      { type: "content_block_start", index: 1, content_block: { type: "text", text: "" } },
      { type: "content_block_delta", index: 1, delta: { type: "text_delta", text: captureAnswer } },
      { type: "content_block_stop", index: 1 },
      { type: "message_stop" },
    ] },
    requestText: "Investigate missing direct-provider capture receipts", responseText: "Synthetic investigation: adapter acceptance precedes the authoritative archive receipt; the next turn remains pending.", toolNames: ["read", "bash"],
  },
  { id: "req_fixture_active_pending", session: "ses_fixture_active_capture", ago: 1, harness: "hermes", model: "anthropic/claude-sonnet-4.5", capture: "accepted", request: { messages: [user("Wait for authoritative save receipt.")] }, response: openai("Awaiting receipt."), requestText: "Wait for authoritative save receipt", responseText: "" },
  { id: "req_fixture_failed_01", session: "ses_fixture_failed_work", ago: 378, request: { messages: [user("Prototype synchronization without changing ownership boundaries.")] }, response: openai("The prototype incorrectly admits another owner's session. Discard this approach."), requestText: "Prototype synchronization ownership boundaries", responseText: "Prototype admits another owner; discard approach.", error: "Ownership check rejected prototype", toolNames: ["bash"] },
  { id: "req_fixture_failed_capture", session: "ses_fixture_failed_work", ago: 332, capture: "failed", request: { messages: [user("Verify the rejected prototype was reverted.")] }, response: openai("Capture upload failed."), requestText: "Verify rejected prototype revert", responseText: "", error: "Archive upload failed" },
  { id: "req_fixture_capture_failed_only", session: "ses_fixture_capture_failed", ago: 45, capture: "failed", request: { messages: [user("Sample: inspect the archive failure receipt.")] }, response: openai("Archive unavailable."), requestText: "Sample: inspect archive failure receipt", responseText: "", error: "Archive upload failed" },
  { id: "req_fixture_unknown_shape", session: "ses_fixture_harness_cursor", ago: 16, harness: "cursor", model: "google/gemini-2.5-pro", request: { vendor_payload: { future_format: true, prompt: "Sample unsupported capture envelope" } }, response: json({ vendor_output: { fragments: ["Preserve unknown evidence for raw inspection."] } }), requestText: "Inspect unsupported vendor capture shape", responseText: "Unknown response format; inspect raw archive." },
  { id: "req_fixture_codex_01", session: "ses_fixture_harness_codex", ago: 14, harness: "codex", model: "openai/gpt-5.4", request: { messages: [user("Sample task: document the rejected synchronization experiment.")] }, response: openai("Recorded the ownership violation and the user's discarded outcome. No replacement implementation is claimed."), requestText: "Document rejected synchronization experiment", responseText: "Recorded ownership violation and discarded outcome." },
  { id: "req_fixture_claude_01", session: "ses_fixture_harness_claude_code", ago: 12, harness: "claude-code", model: "anthropic/claude-opus-4.1", request: { messages: [user("Sample review: inspect the same hierarchy commit from another session.")] }, response: openai("Review capture includes the same commit SHA but a narrower patch. Preserve both captures rather than assuming patch equality."), requestText: "Review shared hierarchy commit from separate session", responseText: "Same SHA has narrower review patch; preserve both captures." },
);

export const fixtureTurns = turns;
export const fixtureSessionExchanges = turns.map((turn): SessionExchange & { tool_names: string[]; has_errors: boolean } => ({
  id: turn.id, session_id: turn.session, ts: fixtureIso(turn.ago), model: turn.model ?? model,
  provider: (turn.model ?? model).split("/")[0] ?? "openai", finish_reason: turn.finish ?? "stop",
  latency_ms: 1_800, harness: turn.harness ?? "opencode", input_tokens: 2_400, output_tokens: 320,
  request_excerpt: turn.requestText, response_excerpt: turn.responseText, request_kind: turn.kind ?? "primary",
  capture_status: turn.capture ?? "saved", capture_reason: turn.capture === "failed" ? "Archive upload failed" : turn.capture === "accepted" ? "Awaiting authoritative save receipt" : null,
  failure_code: turn.capture === "failed" ? "archive_upload_failed" : null,
  tool_names: turn.toolNames ?? [], has_errors: Boolean(turn.error),
}));

export function fixtureEnvelope(id: string): LogEnvelope | undefined {
  const turn = turns.find((entry) => entry.id === id);
  if (!turn || (turn.capture && turn.capture !== "saved")) return undefined;
  return {
    schema_version: 1, exchange_id: turn.id, session_id: turn.session, captured_at: fixtureIso(turn.ago),
    endpoint: "/v1/chat/completions", request: turn.request, response: turn.response,
    ...(turn.activity ? { tool_activity: turn.activity } : {}),
  };
}
