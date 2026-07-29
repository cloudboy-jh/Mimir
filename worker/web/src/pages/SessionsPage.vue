<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowRight, Filter, GitBranch, RotateCw, Search, X } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import { errorMessage, listSessions, type Outcome, type Session, type SessionFilters } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { compactNumber, duration, relativeDate } from "@/lib/format";

const route = useRoute();
const router = useRouter();
const sessions = ref<Session[]>([]);
const nextCursor = ref<string | null>(null);
const loading = ref(true);
const loadingMore = ref(false);
const error = ref("");
const filtersOpen = ref(false);
const draft = reactive({ q: "", repo: "", outcome: "", app: "", model: "", from: "", to: "", limit: "25" });
let controller: AbortController | null = null;

function queryValue(key: string) {
  const value = route.query[key];
  return typeof value === "string" ? value : "";
}

function syncDraft() {
  draft.q = queryValue("q");
  draft.repo = queryValue("repo");
  draft.outcome = queryValue("outcome");
  draft.app = queryValue("app");
  draft.model = queryValue("model");
  draft.from = queryValue("from");
  draft.to = queryValue("to");
  draft.limit = queryValue("limit") || "25";
  filtersOpen.value ||= activeFilterCount.value > 1;
}

const activeFilterCount = computed(() => ["q", "repo", "outcome", "app", "model", "from", "to"].filter((key) => queryValue(key)).length);

function currentFilters(cursor?: string): SessionFilters {
  const from = queryValue("from");
  const to = queryValue("to");
  return {
    q: queryValue("q") || undefined,
    repo: queryValue("repo") || undefined,
    outcome: (queryValue("outcome") || undefined) as Outcome | undefined,
    app: queryValue("app") || undefined,
    model: queryValue("model") || undefined,
    from: from ? `${from}T00:00:00.000Z` : undefined,
    to: to ? `${to}T23:59:59.999Z` : undefined,
    limit: Number(queryValue("limit") || 25),
    cursor,
  };
}

async function load(silent = false) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loadingMore.value = false;
  nextCursor.value = null;
  if (!silent) loading.value = true;
  error.value = "";
  try {
    const result = await listSessions(currentFilters(), active.signal);
    sessions.value = result.sessions;
    nextCursor.value = result.next_cursor;
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "Sessions could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

async function loadMore() {
  if (!nextCursor.value || loading.value || loadingMore.value) return;
  controller?.abort();
  loadingMore.value = true;
  const active = new AbortController();
  controller = active;
  try {
    const result = await listSessions(currentFilters(nextCursor.value), active.signal);
    sessions.value.push(...result.sessions);
    nextCursor.value = result.next_cursor;
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "More sessions could not be loaded.");
  } finally {
    if (controller === active) loadingMore.value = false;
  }
}

function applyFilters() {
  const query: Record<string, string> = {};
  for (const key of ["q", "repo", "outcome", "app", "model", "from", "to", "limit"] as const) {
    const value = draft[key].trim();
    if (value && !(key === "limit" && value === "25")) query[key] = value;
  }
  void router.push({ name: "sessions", query });
}

function clearFilters() {
  Object.assign(draft, { q: "", repo: "", outcome: "", app: "", model: "", from: "", to: "", limit: "25" });
  void router.push({ name: "sessions" });
}

watch(() => route.fullPath, () => {
  syncDraft();
  void load();
}, { immediate: true });
useAutoRefresh(() => sessions.value.length <= Number(queryValue("limit") || 25) ? load(true) : undefined);
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section>
    <div class="mb-7 flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
      <div><h1 class="text-[28px] font-semibold tracking-[-0.025em] text-zinc-950 dark:text-zinc-50">Sessions</h1><p class="mt-1.5 max-w-2xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">Understand what your agents attempted, what changed, and which work was worth keeping.</p></div>
      <div v-if="!loading" class="font-mono text-xs text-zinc-500 dark:text-zinc-400">{{ sessions.length }} loaded</div>
    </div>

    <form class="mb-4 border-y border-zinc-200 py-3 dark:border-zinc-800" @submit.prevent="applyFilters">
      <div class="flex flex-col gap-2 sm:flex-row">
        <label class="relative block min-w-0 flex-1 sm:max-w-lg"><span class="sr-only">Search sessions</span><Search class="pointer-events-none absolute left-2.5 top-2.25 size-4 text-zinc-400" /><input v-model="draft.q" type="search" placeholder="Search intent, repository, app, model, or ID" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white pl-8.5 pr-3 text-[13px] text-zinc-900 placeholder:text-zinc-500 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" /></label>
        <button type="button" class="inline-flex h-8.5 items-center justify-center gap-2 rounded-[5px] border border-zinc-300 px-3 text-[13px] font-medium text-zinc-700 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-900" :aria-expanded="filtersOpen" aria-controls="session-filters" @click="filtersOpen = !filtersOpen"><Filter class="size-3.5" />Filters<span v-if="activeFilterCount" class="font-mono text-[11px] text-zinc-500">{{ activeFilterCount }}</span></button>
        <button type="submit" class="h-8.5 rounded-[5px] bg-zinc-900 px-3 text-[13px] font-medium text-zinc-50 hover:bg-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300">Apply</button>
        <button v-if="activeFilterCount" type="button" class="inline-flex h-8.5 items-center justify-center gap-1.5 px-2 text-[13px] font-medium text-zinc-500 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:hover:text-zinc-100" @click="clearFilters"><X class="size-3.5" />Clear</button>
      </div>
      <div v-if="filtersOpen" id="session-filters" class="mt-3 grid gap-3 border-t border-zinc-200 pt-3 sm:grid-cols-2 lg:grid-cols-4 dark:border-zinc-800">
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Repository<input v-model="draft.repo" placeholder="Exact repository" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" /></label>
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">App<input v-model="draft.app" placeholder="Exact app" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" /></label>
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Model<input v-model="draft.model" placeholder="Exact model" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" /></label>
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Outcome<select v-model="draft.outcome" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"><option value="">All outcomes</option><option value="landed">Landed</option><option value="discarded">Discarded</option><option value="abandoned">Abandoned</option><option value="unresolved">Unresolved</option></select></label>
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">From<input v-model="draft.from" type="date" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" /></label>
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">To<input v-model="draft.to" type="date" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" /></label>
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Rows per page<select v-model="draft.limit" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100"><option value="10">10</option><option value="25">25</option><option value="50">50</option><option value="100">100</option></select></label>
      </div>
    </form>

    <div class="overflow-hidden rounded-[7px] border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
      <div class="hidden grid-cols-[minmax(0,1fr)_150px_130px_150px_90px_28px] gap-4 border-b border-zinc-200 bg-zinc-50 px-4 py-2.5 text-xs font-medium text-zinc-500 lg:grid dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400"><span>Session</span><span>App / model</span><span>Outcome</span><span>Capture</span><span class="text-right">Tokens</span><span /></div>
      <div v-if="loading" aria-busy="true" aria-label="Loading sessions"><div v-for="index in 5" :key="index" class="grid gap-3 border-b border-zinc-200 px-4 py-5 last:border-b-0 lg:grid-cols-[minmax(0,1fr)_150px_130px_150px_90px_28px] dark:border-zinc-800"><div class="h-4 w-3/5 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-24 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-20 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
      <div v-else-if="error && !sessions.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Sessions unavailable</p><p class="mx-auto mt-1 max-w-md text-sm text-zinc-500">{{ error }}</p><button class="mt-4 inline-flex h-8.5 items-center gap-2 rounded-[5px] border border-zinc-300 px-3 text-[13px] font-medium hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:border-zinc-700 dark:hover:bg-zinc-800" @click="load()"><RotateCw class="size-3.5" />Retry</button></div>
      <template v-else>
        <RouterLink v-for="session in sessions" :key="session.id" :to="`/sessions/${session.id}`" class="group grid gap-3 border-b border-zinc-200 px-4 py-4 transition-colors last:border-b-0 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 lg:grid-cols-[minmax(0,1fr)_150px_130px_150px_90px_28px] lg:items-center dark:border-zinc-800 dark:hover:bg-zinc-800/70">
          <div class="min-w-0"><div class="flex items-center gap-2"><span v-if="session.state === 'active'" class="size-1.5 shrink-0 rounded-full bg-emerald-500" aria-label="Active session" /><h2 class="truncate text-sm font-medium text-zinc-950 dark:text-zinc-100">{{ session.intent || "Untitled session" }}</h2></div><div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400"><span class="font-medium text-zinc-700 dark:text-zinc-300">{{ session.repo || "No repository" }}</span><span v-if="session.source_ref" class="inline-flex items-center gap-1"><GitBranch class="size-3" />{{ session.source_ref }}</span><span>{{ relativeDate(session.started_at) }}</span><span>{{ duration(session.started_at, session.ended_at) }}</span><span v-if="session.child_session_count">{{ session.child_session_count }} supporting {{ session.child_session_count === 1 ? "run" : "runs" }}</span><span class="font-mono">{{ session.id }}</span></div></div>
          <div class="min-w-0 space-y-1.5"><IdentityBadge :label="session.harness || 'Unknown app'" /><IdentityBadge :label="session.model_primary || 'Unknown model'" /></div>
          <div><OutcomeBadge :outcome="session.outcome" /></div>
          <div class="text-xs text-zinc-700 dark:text-zinc-300"><span class="mr-1 text-zinc-500 lg:hidden">Capture</span><strong class="font-medium capitalize">{{ session.capture.status }}</strong> · {{ session.capture.saved_exchanges }} {{ session.capture.saved_exchanges === 1 ? "exchange" : "exchanges" }}<span v-if="session.capture.failed_exchanges"> · {{ session.capture.failed_exchanges }} failed</span></div>
          <div class="text-left font-mono text-xs text-zinc-700 lg:text-right dark:text-zinc-300"><span class="mr-1 text-zinc-500 lg:hidden">Tokens</span>{{ compactNumber(session.tokens_in + session.tokens_out) }}</div><ArrowRight class="hidden size-4 text-zinc-400 transition-transform group-hover:translate-x-0.5 lg:block" />
        </RouterLink>
        <div v-if="!sessions.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">{{ activeFilterCount ? "No matching sessions" : "No sessions captured yet" }}</p><p class="mt-1 text-sm text-zinc-500">{{ activeFilterCount ? "Clear a filter or try a broader search." : "Captured model traffic will appear here as work sessions." }}</p></div>
      </template>
    </div>
    <div v-if="nextCursor || error" class="mt-4 flex items-center justify-between gap-4"><p class="text-xs text-red-700 dark:text-red-400" role="alert">{{ error }}</p><button v-if="nextCursor" :disabled="loading || loadingMore" class="ml-auto inline-flex h-8.5 items-center gap-2 rounded-[5px] border border-zinc-300 bg-white px-3 text-[13px] font-medium hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-wait disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-900 dark:hover:bg-zinc-800" @click="loadMore">{{ loadingMore ? "Loading..." : "Load more sessions" }}</button></div>
  </section>
</template>
