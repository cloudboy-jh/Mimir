<script setup lang="ts">
import { TriangleAlert } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import SessionFiles from "@/components/session/SessionFiles.vue";
import type { OutcomeEvidence, SessionDetail } from "@/lib/api";
import { relativeDate } from "@/lib/format";

defineProps<{ supportingSessions: SessionDetail["supporting_sessions"]; files: string[]; errors: SessionDetail["errors"]; evidence: OutcomeEvidence | null }>();
</script>

<template>
  <aside class="space-y-7">
    <div v-if="supportingSessions.length"><h2 class="mb-3 text-sm font-semibold text-zinc-900 dark:text-zinc-100">Supporting runs</h2><ul class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800"><li v-for="run in supportingSessions" :key="run.id" class="py-3"><p class="text-xs font-medium text-zinc-800 dark:text-zinc-200">{{ run.intent || "Supporting agent run" }}</p><div class="mt-1.5 flex flex-wrap gap-2"><IdentityBadge :label="run.model_primary || 'Unknown model'" /><span class="font-mono text-[11px] text-zinc-500">{{ run.id }}</span></div></li></ul></div>
    <SessionFiles :files="files" :evidence="evidence" />
    <div>
      <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><TriangleAlert class="size-4" />Errors</h2>
      <ul v-if="errors.length" class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="item in errors" :key="item.signature" class="py-2.5">
          <details class="group">
            <summary class="flex cursor-pointer list-none items-baseline justify-between gap-3 [&::-webkit-details-marker]:hidden">
              <span class="min-w-0 truncate text-xs leading-5 text-zinc-800 dark:text-zinc-200">{{ item.signature }}</span>
              <span class="shrink-0 font-mono text-[11px] text-zinc-500">{{ item.count }}×</span>
            </summary>
            <div class="mt-2 space-y-1.5 text-[11px] leading-4 text-zinc-500">
              <p class="whitespace-pre-wrap break-words font-mono text-zinc-600 dark:text-zinc-400">{{ item.signature }}</p>
              <p v-if="item.last_seen_at">Last seen {{ relativeDate(item.last_seen_at) }}</p>
              <p v-else>Recorded before error tracking; no timing data.</p>
              <RouterLink v-if="item.latest_exchange_id" :to="`/requests/${item.latest_exchange_id}`" class="inline-block font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">Newest matching request</RouterLink>
            </div>
          </details>
        </li>
      </ul>
      <p v-else class="text-sm text-zinc-500">No errors detected.</p>
    </div>
  </aside>
</template>
