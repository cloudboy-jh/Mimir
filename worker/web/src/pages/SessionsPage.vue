<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowRight, Filter, GitBranch, RotateCw, Search, X } from "lucide-vue-next";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import SessionModelStack from "@/components/session/SessionModelStack.vue";
import SessionLivenessBadge from "@/components/session/SessionLivenessBadge.vue";
import Button from "@/components/ui/Button.vue";
import DropdownPanel from "@/components/ui/DropdownPanel.vue";
import Select from "@/components/ui/Select.vue";
import { errorMessage, listSessions, type Outcome, type Session, type SessionFilters } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { facetSelectOptions, useFacets } from "@/lib/facets";
import { compactNumber, duration, relativeDate } from "@/lib/format";
import { outcomeOptions, pageSizeOptions } from "@/lib/options";
import { displayTitle } from "@/lib/sessions";

const SEARCH_DEBOUNCE_MS = 350;
const facetKeys = ["repo", "outcome", "app", "model", "from", "to"] as const;
const facetLabels: Record<string, string> = { repo: "Repository", outcome: "Outcome", app: "App", model: "Model", from: "From", to: "To" };

const route = useRoute();
const router = useRouter();
const sessions = ref<Session[]>([]);
const nextCursor = ref<string | null>(null);
const loading = ref(true);
const loadingMore = ref(false);
const error = ref("");
const filtersOpen = ref(false);
const search = ref("");
const draft = reactive<Record<string, string>>({ repo: "", outcome: "", app: "", model: "", from: "", to: "" });
const { facets } = useFacets();
const repoOptions = computed(() => facetSelectOptions(facets.value.repos, draft.repo, "All repositories"));
const appOptions = computed(() => facetSelectOptions(facets.value.apps, draft.app, "All apps"));
const modelOptions = computed(() => facetSelectOptions(facets.value.models, draft.model, "All models"));
let controller: AbortController | null = null;
let searchTimer: ReturnType<typeof setTimeout> | undefined;

function queryValue(key: string) {
  const value = route.query[key];
  return typeof value === "string" ? value : "";
}

const limit = computed(() => queryValue("limit") || "25");
const activeFacets = computed(() => facetKeys.flatMap((key) => queryValue(key) ? [{ key, label: facetLabels[key], value: queryValue(key) }] : []));
const activeFilterCount = computed(() => activeFacets.value.length + (queryValue("q") ? 1 : 0));

function setParams(patch: Record<string, string>) {
  const query = { ...route.query } as Record<string, string>;
  for (const [key, raw] of Object.entries(patch)) {
    const value = raw.trim();
    if (!value || (key === "limit" && value === "25")) delete query[key];
    else query[key] = value;
  }
  void router.push({ name: "sessions", query });
}

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
    limit: Number(limit.value),
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

function commitSearch() {
  clearTimeout(searchTimer);
  if (search.value.trim() !== queryValue("q")) setParams({ q: search.value });
}

function applyDraft() {
  setParams({ ...draft });
  filtersOpen.value = false;
}

function clearFilters() {
  for (const key of facetKeys) draft[key] = "";
  search.value = "";
  clearTimeout(searchTimer);
  setParams(Object.fromEntries([...facetKeys, "q"].map((key) => [key, ""])));
  filtersOpen.value = false;
}

watch(search, (value) => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (value.trim() !== queryValue("q")) setParams({ q: value });
  }, SEARCH_DEBOUNCE_MS);
});
watch(filtersOpen, (open) => {
  if (open) for (const key of facetKeys) draft[key] = queryValue(key);
});

watch(() => route.fullPath, () => {
  if (queryValue("q") !== search.value.trim()) search.value = queryValue("q");
  void load();
}, { immediate: true });
useAutoRefresh(() => sessions.value.length <= Number(limit.value) ? load(true) : undefined);
onBeforeUnmount(() => { controller?.abort(); clearTimeout(searchTimer); });
</script>

<template>
  <section>
    <div class="mb-7 flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
      <div><h1 class="text-[28px] font-semibold tracking-[-0.025em] text-zinc-950 dark:text-zinc-50">Sessions</h1><p class="mt-1.5 max-w-2xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">Understand what your agents attempted, what changed, and which work was worth keeping.</p></div>
      <div v-if="!loading" class="font-mono text-xs text-zinc-500 dark:text-zinc-400">{{ sessions.length }} loaded</div>
    </div>

    <div class="mb-4 border-y border-zinc-200 py-3 dark:border-zinc-800">
      <div class="flex flex-col gap-2 sm:flex-row">
        <form class="relative min-w-0 flex-1 sm:max-w-lg" role="search" @submit.prevent="commitSearch">
          <label class="sr-only" for="session-search">Search sessions</label>
          <Search class="pointer-events-none absolute left-2.5 top-2.25 size-4 text-zinc-400" aria-hidden="true" />
          <input id="session-search" v-model="search" type="search" placeholder="Search title, intent, repository, app, model, or ID" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white pl-8.5 pr-3 text-[13px] text-zinc-900 placeholder:text-zinc-500 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" />
        </form>
        <DropdownPanel v-model:open="filtersOpen" title="Filter sessions" description="Exact matches, applied together with the current search.">
          <template #trigger><Button variant="outline"><Filter class="size-3.5" />Filters<span v-if="activeFacets.length" class="font-mono text-[11px] text-zinc-500">{{ activeFacets.length }}</span></Button></template>
          <form id="session-filters" class="grid gap-3 sm:grid-cols-2" @submit.prevent="applyDraft">
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Repository</span><Select v-model="draft.repo" label="Repository" :options="repoOptions" placeholder="All repositories" class="w-full font-normal" /></div>
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">App</span><Select v-model="draft.app" label="App" :options="appOptions" placeholder="All apps" class="w-full font-normal" /></div>
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Model</span><Select v-model="draft.model" label="Model" :options="modelOptions" placeholder="All models" class="w-full font-normal" /></div>
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Outcome</span><Select v-model="draft.outcome" label="Outcome" :options="outcomeOptions" placeholder="All outcomes" class="w-full font-normal" /></div>
            <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">From<input v-model="draft.from" type="date" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" /></label>
            <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">To<input v-model="draft.to" type="date" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" /></label>
          </form>
          <template #footer>
            <Button variant="ghost" @click="clearFilters">Clear all</Button>
            <Button variant="outline" @click="filtersOpen = false">Cancel</Button>
            <Button type="submit" form="session-filters">Apply filters</Button>
          </template>
        </DropdownPanel>
        <Select :model-value="limit" label="Rows per page" :options="pageSizeOptions" class="sm:w-28" @update:model-value="setParams({ limit: $event })" />
      </div>
      <ul v-if="activeFacets.length" class="mt-2.5 flex flex-wrap items-center gap-2">
        <li v-for="facet in activeFacets" :key="facet.key">
          <button type="button" class="inline-flex items-center gap-1.5 rounded-[5px] border border-zinc-300 px-2 py-1 text-[11px] text-zinc-700 transition-colors duration-150 ease-out hover:border-zinc-400 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800" @click="setParams({ [facet.key]: '' })">
            <span class="text-zinc-500">{{ facet.label }}</span><span class="font-mono">{{ facet.value }}</span><X class="size-3" aria-hidden="true" />
            <span class="sr-only">Remove {{ facet.label }} filter</span>
          </button>
        </li>
        <li><button type="button" class="text-[11px] font-medium text-zinc-500 hover:text-zinc-900 focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:hover:text-zinc-100" @click="clearFilters">Clear all</button></li>
      </ul>
    </div>

    <div class="overflow-hidden rounded-[7px] border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
      <div class="hidden grid-cols-[minmax(0,1fr)_150px_130px_150px_90px_28px] gap-4 border-b border-zinc-200 bg-zinc-50 px-4 py-2.5 text-xs font-medium text-zinc-500 lg:grid dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400"><span>Session</span><span>App / model</span><span>Outcome</span><span>Capture</span><span class="text-right">Tokens</span><span /></div>
      <div v-if="loading" aria-busy="true" aria-label="Loading sessions"><div v-for="index in 5" :key="index" class="grid gap-3 border-b border-zinc-200 px-4 py-5 last:border-b-0 lg:grid-cols-[minmax(0,1fr)_150px_130px_150px_90px_28px] dark:border-zinc-800"><div class="h-4 w-3/5 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-24 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-20 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
      <div v-else-if="error && !sessions.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Sessions unavailable</p><p class="mx-auto mt-1 max-w-md text-sm text-zinc-500">{{ error }}</p><button class="mt-4 inline-flex h-8.5 items-center gap-2 rounded-[5px] border border-zinc-300 px-3 text-[13px] font-medium hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:border-zinc-700 dark:hover:bg-zinc-800" @click="load()"><RotateCw class="size-3.5" />Retry</button></div>
      <template v-else>
        <RouterLink v-for="session in sessions" :key="session.id" :to="`/sessions/${session.id}`" class="group grid gap-3 border-b border-zinc-200 px-4 py-4 transition-colors last:border-b-0 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 lg:grid-cols-[minmax(0,1fr)_150px_130px_150px_90px_28px] lg:items-center dark:border-zinc-800 dark:hover:bg-zinc-800/70">
          <div class="min-w-0"><div class="flex min-w-0 items-center gap-2"><h2 class="min-w-0 truncate text-sm font-medium text-zinc-950 dark:text-zinc-100">{{ displayTitle(session) }}</h2><SessionLivenessBadge class="shrink-0" :liveness="session.liveness" /></div><div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400"><span class="font-medium text-zinc-700 dark:text-zinc-300">{{ session.repo || "No repository" }}</span><span v-if="session.source_ref" class="inline-flex items-center gap-1"><GitBranch class="size-3" />{{ session.source_ref }}</span><span>{{ relativeDate(session.started_at) }}</span><span>{{ duration(session.started_at, session.ended_at) }}</span><span v-if="session.child_session_count">{{ session.child_session_count }} supporting {{ session.child_session_count === 1 ? "run" : "runs" }}</span><span class="font-mono">{{ session.id }}</span></div></div>
          <SessionModelStack :app="session.harness" :primary="session.model_primary" :models="session.models" />
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
