<script setup lang="ts">
import { computed, ref } from "vue";
import { ArrowRight, GitBranch, Search } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import { sessions } from "@/lib/mock";
import { compactNumber, duration, relativeDate } from "@/lib/format";

const query = ref("");
const outcome = ref("");
const filtered = computed(() => sessions.filter((session) => {
  const needle = query.value.toLowerCase();
  return (!needle || `${session.intent} ${session.repo} ${session.harness} ${session.model_primary}`.toLowerCase().includes(needle)) && (!outcome.value || session.outcome === outcome.value);
}));
</script>

<template>
  <section>
    <div class="mb-7 flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
      <div><h1 class="text-[28px] font-semibold tracking-[-0.025em] text-zinc-950 dark:text-zinc-50">Sessions</h1><p class="mt-1.5 max-w-2xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">Understand what your agents attempted, what changed, and which work was worth keeping.</p></div>
      <div class="font-mono text-xs text-zinc-500 dark:text-zinc-400">{{ filtered.length }} of {{ sessions.length }} sessions</div>
    </div>

    <div class="mb-4 flex flex-col gap-2 sm:flex-row">
      <label class="relative block min-w-0 flex-1 sm:max-w-sm"><span class="sr-only">Search sessions</span><Search class="pointer-events-none absolute left-2.5 top-2.25 size-4 text-zinc-400" /><input v-model="query" type="search" placeholder="Search intent, repository, model..." class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white pl-8.5 pr-3 text-[13px] text-zinc-900 placeholder:text-zinc-500 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:placeholder:text-zinc-500" /></label>
      <label><span class="sr-only">Filter by outcome</span><select v-model="outcome" class="h-8.5 rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] text-zinc-800 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200"><option value="">All outcomes</option><option value="promoted">Promoted</option><option value="discarded">Discarded</option><option value="abandoned">Abandoned</option><option value="unknown">Unknown</option></select></label>
    </div>

    <div class="overflow-hidden rounded-[7px] border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
      <div class="hidden grid-cols-[minmax(0,1fr)_150px_150px_90px_90px_28px] gap-4 border-b border-zinc-200 bg-zinc-50 px-4 py-2.5 text-xs font-medium text-zinc-500 lg:grid dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400"><span>Session</span><span>App / model</span><span>Outcome</span><span class="text-right">Requests</span><span class="text-right">Tokens</span><span /></div>
      <RouterLink v-for="session in filtered" :key="session.id" :to="`/sessions/${session.id}`" class="group grid gap-3 border-b border-zinc-200 px-4 py-4 transition-colors last:border-b-0 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 lg:grid-cols-[minmax(0,1fr)_150px_150px_90px_90px_28px] lg:items-center dark:border-zinc-800 dark:hover:bg-zinc-800/70">
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <span v-if="session.state === 'active'" class="size-1.5 shrink-0 rounded-full bg-emerald-500" aria-label="Active session" />
            <h2 class="truncate text-sm font-medium text-zinc-950 dark:text-zinc-100">{{ session.intent }}</h2>
          </div>
          <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400">
            <span class="font-medium text-zinc-700 dark:text-zinc-300">{{ session.repo }}</span>
            <span class="inline-flex items-center gap-1"><GitBranch class="size-3" />{{ session.source_ref }}</span>
            <span>{{ relativeDate(session.started_at) }}</span>
            <span>{{ duration(session.started_at, session.ended_at) }}</span>
            <span class="font-mono">{{ session.id }}</span>
          </div>
        </div>
        <div class="min-w-0 space-y-1.5"><IdentityBadge :label="session.harness" /><IdentityBadge :label="session.model_primary" /></div>
        <div><OutcomeBadge :outcome="session.outcome" /></div>
        <div class="text-left font-mono text-xs text-zinc-700 lg:text-right dark:text-zinc-300"><span class="mr-1 text-zinc-500 lg:hidden">Requests</span>{{ session.request_count }}</div>
        <div class="text-left font-mono text-xs text-zinc-700 lg:text-right dark:text-zinc-300"><span class="mr-1 text-zinc-500 lg:hidden">Tokens</span>{{ compactNumber(session.tokens_in + session.tokens_out) }}</div>
        <ArrowRight class="hidden size-4 text-zinc-400 transition-transform group-hover:translate-x-0.5 lg:block" />
      </RouterLink>
      <div v-if="!filtered.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">No matching sessions</p><p class="mt-1 text-sm text-zinc-500">Clear a filter or try a broader search.</p></div>
    </div>
  </section>
</template>
