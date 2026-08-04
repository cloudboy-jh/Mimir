<script setup lang="ts">
import { computed, ref } from "vue";
import { TriangleAlert } from "lucide-vue-next";
import SessionFiles from "@/components/session/SessionFiles.vue";
import SessionModelStack from "@/components/session/SessionModelStack.vue";
import type { OutcomeEvidence, SessionDetail } from "@/lib/api";
import { relativeDate } from "@/lib/format";
import { displayTitle } from "@/lib/sessions";

const ERROR_PREVIEW = 5;
const RUN_PREVIEW = 5;

const props = defineProps<{ supportingSessions: SessionDetail["supporting_sessions"]; files: string[]; errors: SessionDetail["errors"]; evidence: OutcomeEvidence | null }>();

const showAllErrors = ref(false);
const showAllRuns = ref(false);
const visibleErrors = computed(() => showAllErrors.value ? props.errors : props.errors.slice(0, ERROR_PREVIEW));
const visibleRuns = computed(() => showAllRuns.value ? props.supportingSessions : props.supportingSessions.slice(0, RUN_PREVIEW));
</script>

<template>
  <aside class="space-y-7">
    <div v-if="supportingSessions.length">
      <h2 class="mb-3 text-sm font-semibold text-zinc-900 dark:text-zinc-100">Supporting runs ({{ supportingSessions.length }})</h2>
      <ul class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800"><li v-for="run in visibleRuns" :key="run.id" class="py-3"><p class="line-clamp-2 text-xs font-medium text-zinc-800 dark:text-zinc-200">{{ displayTitle(run) }}</p><SessionModelStack class="mt-2" :app="run.harness" :primary="run.model_primary" :models="run.models" /><span class="mt-1.5 block truncate font-mono text-[11px] text-zinc-500">{{ run.id }}</span></li></ul>
      <button v-if="supportingSessions.length > RUN_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllRuns = !showAllRuns">{{ showAllRuns ? "Show fewer runs" : `Show all ${supportingSessions.length} runs` }}</button>
    </div>
    <SessionFiles :files="files" :evidence="evidence" />
    <div>
      <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><TriangleAlert class="size-4" />Errors<span v-if="errors.length" class="font-mono text-[11px] font-normal text-zinc-500">{{ errors.length }}</span></h2>
      <ul v-if="errors.length" class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="item in visibleErrors" :key="item.signature" class="py-2.5">
          <details>
            <summary class="flex cursor-pointer list-none items-baseline justify-between gap-3 [&::-webkit-details-marker]:hidden">
              <span class="min-w-0 truncate text-xs leading-5 text-zinc-800 dark:text-zinc-200">{{ item.signature }}</span>
              <span class="shrink-0 font-mono text-[11px] text-zinc-500">{{ item.count }}×</span>
            </summary>
            <div class="mt-2 space-y-1.5 text-[11px] leading-4 text-zinc-500">
              <p class="max-h-40 overflow-y-auto whitespace-pre-wrap break-words font-mono text-zinc-600 dark:text-zinc-400">{{ item.signature }}</p>
              <p v-if="item.last_seen_at">Last seen {{ relativeDate(item.last_seen_at) }}</p>
              <p v-else>Recorded before error tracking; no timing data.</p>
              <RouterLink v-if="item.latest_exchange_id" :to="`/requests/${item.latest_exchange_id}`" class="inline-block font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">Newest matching request</RouterLink>
            </div>
          </details>
        </li>
      </ul>
      <p v-else class="text-sm text-zinc-500">No errors detected.</p>
      <button v-if="errors.length > ERROR_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllErrors = !showAllErrors">{{ showAllErrors ? "Show fewer errors" : `Show all ${errors.length} errors` }}</button>
    </div>
  </aside>
</template>
