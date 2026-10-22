<script setup lang="ts">
import { computed, nextTick } from "vue";
import SessionChanges from "./SessionChanges.vue";
import SessionEvidenceSidebar from "./SessionEvidenceSidebar.vue";
import SessionOutcome from "./SessionOutcome.vue";
import LiveSessionTurns from "./LiveSessionTurns.vue";
import type { LiveSessionTurn, SessionDetail, SessionLiveness } from "@/lib/api";
import type { SessionChangeEvidence } from "@/lib/git";

// The page passes only sections that have evidence, so a lone section renders
// without tabs and an empty sidebar is never mounted.
const props = defineProps<{ detail: SessionDetail; sections: Array<"details" | "changes">; changes: SessionChangeEvidence; outcomeRecorded: boolean; liveTurns: LiveSessionTurn[]; liveness: SessionLiveness }>();
const section = defineModel<"details" | "changes">({ required: true });
defineEmits<{ saved: [] }>();
const tabbed = computed(() => props.sections.length > 1);
function moveTab(event: KeyboardEvent) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  section.value = event.key === "Home" ? "details" : event.key === "End" ? "changes" : section.value === "details" ? "changes" : "details";
  void nextTick(() => document.getElementById(`session-sidebar-tab-${section.value}`)?.focus());
}
</script>

<template>
  <aside id="session-sidebar" aria-labelledby="session-sidebar-heading" class="min-w-0 scroll-mt-20 border-t border-zinc-200 pt-4 lg:self-start lg:border-t-0 lg:border-l lg:pl-6 lg:pt-0 dark:border-zinc-800">
    <h2 id="session-sidebar-heading" tabindex="-1" class="sr-only">Session evidence</h2>
    <div v-if="tabbed" role="tablist" aria-label="Session sidebar" class="mb-4 flex gap-5 border-b border-zinc-200 dark:border-zinc-800" @keydown="moveTab">
      <button v-for="item in sections" :id="`session-sidebar-tab-${item}`" :key="item" type="button" role="tab" :aria-selected="section === item" :aria-controls="`session-sidebar-${item}`" :tabindex="section === item ? 0 : -1" class="border-b-2 py-3 text-sm font-medium capitalize focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600" :class="section === item ? 'border-teal-700 text-zinc-950 dark:border-teal-400 dark:text-zinc-100' : 'border-transparent text-zinc-600 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100'" @click="section = item">{{ item }}<span v-if="item === 'changes' && detail.git_artifacts.length" class="ml-1.5 font-mono text-xs" aria-hidden="true">{{ detail.git_artifacts.length }}</span></button>
    </div>
    <div class="min-w-0">
      <div v-if="sections.includes('details')" id="session-sidebar-details" v-show="section === 'details'" :role="tabbed ? 'tabpanel' : undefined" :aria-labelledby="tabbed ? 'session-sidebar-tab-details' : undefined" :tabindex="tabbed ? 0 : undefined" class="space-y-5 focus-visible:outline-2 focus-visible:outline-teal-600">
        <SessionOutcome v-if="outcomeRecorded" :detail="detail" @saved="$emit('saved')" />
        <SessionEvidenceSidebar :session-id="detail.session.id" :supporting-sessions="detail.supporting_sessions" :files="detail.files" :errors="detail.errors" />
        <LiveSessionTurns v-if="liveTurns.length" :turns="liveTurns" :liveness="liveness" />
      </div>
      <div v-if="sections.includes('changes')" id="session-sidebar-changes" v-show="section === 'changes'" :role="tabbed ? 'tabpanel' : undefined" :aria-labelledby="tabbed ? 'session-sidebar-tab-changes' : undefined" :tabindex="tabbed ? 0 : undefined" class="focus-visible:outline-2 focus-visible:outline-teal-600">
        <SessionChanges :session-id="detail.session.id" :artifacts="detail.git_artifacts" :changes="changes" :source-ref="detail.session.source_ref" />
      </div>
    </div>
  </aside>
</template>
