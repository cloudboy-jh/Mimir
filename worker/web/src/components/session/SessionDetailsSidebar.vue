<script setup lang="ts">
import { computed, nextTick } from "vue";
import SessionChanges from "./SessionChanges.vue";
import SessionEvidenceSidebar from "./SessionEvidenceSidebar.vue";
import SessionOutcome from "./SessionOutcome.vue";
import LiveSessionTurns from "./LiveSessionTurns.vue";
import { currentOutcomeEvidence, type LiveSessionTurn, type SessionDetail, type SessionLiveness } from "@/lib/api";
import { compactNumber, duration, shortDate } from "@/lib/format";

const props = defineProps<{ detail: SessionDetail; liveTurns: LiveSessionTurn[]; liveness: SessionLiveness }>();
const section = defineModel<"changes" | "details">({ required: true });
defineEmits<{ saved: [] }>();
const usage = computed(() => {
  const session = props.detail.session;
  const rows = [
    { label: "Duration", value: duration(session.started_at, session.ended_at) },
    { label: "Requests", value: String(session.request_count) },
    { label: "Input tokens", value: compactNumber(session.tokens_in) },
    { label: "Output tokens", value: compactNumber(session.tokens_out) },
  ];
  if (session.cache_read_tokens) rows.push({ label: "Cache read", value: compactNumber(session.cache_read_tokens) });
  if (session.cache_write_tokens) rows.push({ label: "Cache write", value: compactNumber(session.cache_write_tokens) });
  return rows;
});
function moveTab(event: KeyboardEvent) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  section.value = event.key === "Home" ? "details" : event.key === "End" ? "changes" : section.value === "details" ? "changes" : "details";
  void nextTick(() => document.getElementById(`session-sidebar-tab-${section.value}`)?.focus());
}
</script>

<template>
  <aside id="session-sidebar" aria-labelledby="session-sidebar-heading" class="min-w-0 scroll-mt-20 border-t border-zinc-200 pt-4 lg:self-start lg:border-t-0 lg:border-l lg:pl-6 lg:pt-0 dark:border-zinc-800">
    <h2 id="session-sidebar-heading" tabindex="-1" class="sr-only">Session details and changes</h2>
    <div role="tablist" aria-label="Session sidebar" class="mb-4 flex gap-5 border-b border-zinc-200 dark:border-zinc-800" @keydown="moveTab">
      <button v-for="item in (['details', 'changes'] as const)" :id="`session-sidebar-tab-${item}`" :key="item" type="button" role="tab" :aria-selected="section === item" :aria-controls="`session-sidebar-${item}`" :tabindex="section === item ? 0 : -1" class="border-b-2 py-3 text-sm font-medium capitalize focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600" :class="section === item ? 'border-teal-700 text-zinc-950 dark:border-teal-400 dark:text-zinc-100' : 'border-transparent text-zinc-600 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100'" @click="section = item">{{ item }}<span v-if="item === 'changes' && detail.git_artifacts.length" class="ml-1.5 font-mono text-xs" aria-hidden="true">{{ detail.git_artifacts.length }}</span></button>
    </div>
    <div class="min-w-0">
      <div id="session-sidebar-details" v-show="section === 'details'" role="tabpanel" aria-labelledby="session-sidebar-tab-details" tabindex="0" class="space-y-5 focus-visible:outline-2 focus-visible:outline-teal-600">
        <section aria-labelledby="session-details-heading">
          <h2 id="session-details-heading" class="text-sm font-semibold">Session</h2>
          <dl class="mt-3 space-y-2 text-sm">
            <div><dt class="text-zinc-600 dark:text-zinc-400">Started</dt><dd class="mt-1">{{ shortDate(detail.session.started_at) }}</dd></div>
            <div><dt class="text-zinc-600 dark:text-zinc-400">Session ID</dt><dd class="mt-1 break-all font-mono text-xs">{{ detail.session.id }}</dd></div>
            <div v-if="detail.session.parent_session_id"><dt class="text-zinc-600 dark:text-zinc-400">Parent session</dt><dd class="mt-1 break-all"><RouterLink :to="`/sessions/${detail.session.parent_session_id}`" class="font-mono text-xs text-teal-700 hover:underline dark:text-teal-400">{{ detail.session.parent_session_id }}</RouterLink></dd></div>
          </dl>
        </section>
        <SessionOutcome :detail="detail" @saved="$emit('saved')" />
        <section aria-labelledby="session-usage-heading" class="border-t border-zinc-200 pt-5 dark:border-zinc-800">
          <h2 id="session-usage-heading" class="text-sm font-semibold">Usage</h2>
          <dl class="mt-3 grid grid-cols-2 gap-x-4 gap-y-3 text-sm"><div v-for="item in usage" :key="item.label"><dt class="text-zinc-600 dark:text-zinc-400">{{ item.label }}</dt><dd class="mt-1 font-mono">{{ item.value }}</dd></div></dl>
        </section>
        <section aria-labelledby="session-capture-heading" class="border-t border-zinc-200 pt-5 dark:border-zinc-800">
          <h2 id="session-capture-heading" class="text-sm font-semibold">Capture receipts</h2>
          <dl class="mt-3 grid grid-cols-2 gap-x-4 gap-y-3 text-sm"><div><dt class="text-zinc-600 dark:text-zinc-400">Saved</dt><dd class="mt-1 font-mono">{{ detail.capture.saved_exchanges }}</dd></div><div><dt class="text-zinc-600 dark:text-zinc-400">Pending</dt><dd class="mt-1 font-mono">{{ detail.capture.pending_exchanges }}</dd></div><div><dt class="text-zinc-600 dark:text-zinc-400">Failed</dt><dd class="mt-1 font-mono">{{ detail.capture.failed_exchanges }}</dd></div><div><dt class="text-zinc-600 dark:text-zinc-400">Last saved</dt><dd class="mt-1">{{ detail.capture.last_saved_at ? shortDate(detail.capture.last_saved_at) : 'Never' }}</dd></div></dl>
        </section>
        <SessionEvidenceSidebar :session-id="detail.session.id" :supporting-sessions="detail.supporting_sessions" :files="detail.files" :errors="detail.errors" />
        <LiveSessionTurns v-if="liveTurns.length" :turns="liveTurns" :liveness="liveness" />
      </div>
      <div id="session-sidebar-changes" v-show="section === 'changes'" role="tabpanel" aria-labelledby="session-sidebar-tab-changes" tabindex="0" class="focus-visible:outline-2 focus-visible:outline-teal-600">
        <SessionChanges :session-id="detail.session.id" :artifacts="detail.git_artifacts" :events="detail.outcome_events" :evidence="currentOutcomeEvidence(detail.outcome_events, detail.session.outcome)" :source-ref="detail.session.source_ref" />
        <p v-if="!detail.git_artifacts.length && !detail.outcome_events.some(event => event.evidence_json)" class="text-sm leading-6 text-zinc-600 dark:text-zinc-400">No Git changes or outcome evidence have been recorded for this session.</p>
      </div>
    </div>
  </aside>
</template>
