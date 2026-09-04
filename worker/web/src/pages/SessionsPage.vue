<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowRight, ChevronRight, CornerDownRight, Filter, GitBranch, RotateCw, Search, X } from "lucide-vue-next";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import DeviceIdentity from "@/components/DeviceIdentity.vue";
import SessionModelStack from "@/components/session/SessionModelStack.vue";
import SessionLivenessBadge from "@/components/session/SessionLivenessBadge.vue";
import Button from "@/components/ui/Button.vue";
import DropdownPanel from "@/components/ui/DropdownPanel.vue";
import Select from "@/components/ui/Select.vue";
import { errorMessage, listSessions, setSessionsOutcome, type Outcome, type Session, type SessionFilters } from "@/lib/api";
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
const descendants = ref<Session[]>([]);
const expandedSubAgents = ref<Set<string>>(new Set());
type TreeNode = { session: Session; children: TreeNode[] };
type TreeRow = { session: Session; depth: number };
type SessionBlock = { root: Session; rows: TreeRow[] };

// Sub-agent sessions ride along as descendants on each root's page. Rows are
// rendered as a compact nested panel beneath their parent root so chained
// sessions stay visible without repeating the full table grid.
function buildBlocks(roots: Session[], loose: Session[]): SessionBlock[] {
  const byParent = new Map<string, Session[]>();
  for (const child of loose) {
    if (!child.parent_session_id) continue;
    const list = byParent.get(child.parent_session_id) ?? [];
    list.push(child);
    byParent.set(child.parent_session_id, list);
  }
  const makeTree = (session: Session): TreeNode => ({
    session,
    children: (byParent.get(session.id) ?? []).map(makeTree),
  });
  const flatten = (nodes: TreeNode[], depth = 0, rows: TreeRow[] = []): TreeRow[] => {
    nodes.forEach((node) => {
      rows.push({ session: node.session, depth });
      flatten(node.children, depth + 1, rows);
    });
    return rows;
  };
  return roots.map((root) => ({ root, rows: flatten([makeTree(root)]).slice(1) }));
}

const blocks = computed(() => buildBlocks(sessions.value, descendants.value));
const nextCursor = ref<string | null>(null);
const loadedPageCount = ref(1);
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
const selectedSessionIds = ref<Set<string>>(new Set());
const bulkOutcome = ref<Outcome>("landed");
const bulkReason = ref("");
const bulkApplying = ref(false);
const bulkError = ref("");
const selectedCount = computed(() => selectedSessionIds.value.size);
const allLoadedSelected = computed(
  () =>
    sessions.value.length > 0 &&
    sessions.value.every((session) => selectedSessionIds.value.has(session.id)),
);
let controller: AbortController | null = null;
let searchTimer: ReturnType<typeof setTimeout> | undefined;

function toggleSession(id: string) {
  const next = new Set(selectedSessionIds.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  selectedSessionIds.value = next;
}

function toggleAllLoaded() {
  const next = new Set(selectedSessionIds.value);
  if (allLoadedSelected.value) {
    for (const session of sessions.value) next.delete(session.id);
  } else {
    for (const session of sessions.value) next.add(session.id);
  }
  selectedSessionIds.value = next;
}

function cacheHitRate(session: Session) {
  const cacheRead = session.cache_read_tokens ?? 0;
  const promptTokens = session.tokens_in + cacheRead;
  return promptTokens > 0 ? Math.round((cacheRead / promptTokens) * 100) : 0;
}

async function applyBulkOutcome() {
  if (!selectedCount.value || bulkApplying.value) return;
  bulkApplying.value = true;
  bulkError.value = "";
  const ids = [...selectedSessionIds.value];
  try {
    await setSessionsOutcome(ids, bulkOutcome.value, bulkReason.value);
    for (const session of sessions.value) {
      if (!selectedSessionIds.value.has(session.id)) continue;
      session.outcome = bulkOutcome.value;
      session.outcome_reason = bulkReason.value.trim() || null;
      session.outcome_src = "user";
      session.outcome_updated_at = new Date().toISOString();
    }
    selectedSessionIds.value = new Set();
    bulkReason.value = "";
  } catch (cause) {
    bulkError.value = errorMessage(cause, "Selected sessions could not be updated.");
  } finally {
    bulkApplying.value = false;
  }
}

function toggleSubAgents(id: string) {
  const next = new Set(expandedSubAgents.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  expandedSubAgents.value = next;
}

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

async function load(silent = false, targetPages = 1) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loadingMore.value = false;
  if (!silent) {
    loading.value = true;
    nextCursor.value = null;
  }
  error.value = "";
  try {
    const refreshed: Session[] = [];
    const seen = new Set<string>();
    let cursor: string | undefined;
    let refreshedCursor: string | null = null;
    let pagesLoaded = 0;
    for (let page = 0; page < targetPages; page++) {
      const result = await listSessions(currentFilters(cursor), active.signal);
      for (const session of result.sessions) {
        if (!seen.has(session.id)) {
          refreshed.push(session);
          seen.add(session.id);
        }
      }
      for (const child of result.descendants) {
        if (!seen.has(child.id)) {
          refreshed.push(child);
          seen.add(child.id);
        }
      }
      pagesLoaded++;
      refreshedCursor = result.next_cursor;
      if (!refreshedCursor) break;
      cursor = refreshedCursor;
    }
    sessions.value = refreshed.filter((session) => !session.parent_session_id);
    descendants.value = refreshed.filter((session) => session.parent_session_id);
    const loadedRootIDs = new Set(sessions.value.map((session) => session.id));
    selectedSessionIds.value = new Set(
      [...selectedSessionIds.value].filter((id) => loadedRootIDs.has(id)),
    );
    nextCursor.value = refreshedCursor;
    loadedPageCount.value = pagesLoaded;
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
    const existing = new Set(sessions.value.map((session) => session.id));
    sessions.value.push(...result.sessions.filter((session) => !existing.has(session.id)));
    const seen = new Set(descendants.value.map((session) => session.id));
    descendants.value.push(...result.descendants.filter((session) => !seen.has(session.id)));
    nextCursor.value = result.next_cursor;
    loadedPageCount.value++;
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
useAutoRefresh(() => load(true, loadedPageCount.value));
onBeforeUnmount(() => { controller?.abort(); clearTimeout(searchTimer); });
</script>

<template>
  <section>
    <div class="mb-7 flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
      <div><h1 class="text-[28px] font-semibold tracking-[-0.025em] text-zinc-950 dark:text-zinc-50">Sessions</h1><p class="mt-1.5 max-w-2xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">Understand what your agents attempted, what changed, and which work was worth keeping.</p></div>
      <div v-if="!loading" class="font-mono text-xs text-zinc-500 dark:text-zinc-400">{{ sessions.length + descendants.length }} loaded</div>
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

    <div v-if="selectedCount" class="mb-3 border-y border-zinc-200 bg-stone-50 px-3 py-2.5 dark:border-zinc-800 dark:bg-zinc-900">
      <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
        <p class="min-w-28 text-sm font-medium text-zinc-900 dark:text-zinc-100" aria-live="polite">{{ selectedCount }} selected</p>
        <Select v-model="bulkOutcome" label="Outcome for selected sessions" :options="outcomeOptions" class="sm:w-36" />
        <label class="min-w-0 flex-1"><span class="sr-only">Reason for outcome</span><input v-model="bulkReason" type="text" maxlength="2000" placeholder="Reason (optional)" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] text-zinc-900 placeholder:text-zinc-500 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-100" /></label>
        <Button :disabled="bulkApplying" @click="applyBulkOutcome">{{ bulkApplying ? "Applying..." : "Set outcome" }}</Button>
        <Button variant="ghost" :disabled="bulkApplying" @click="selectedSessionIds = new Set()">Clear</Button>
      </div>
      <p v-if="bulkError" class="mt-2 text-xs text-red-700 dark:text-red-400" role="alert">{{ bulkError }}</p>
    </div>

    <div class="overflow-hidden rounded-[7px] border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
      <div class="hidden grid-cols-[28px_minmax(0,1fr)_150px_130px_150px_104px_28px] gap-4 border-b border-zinc-200 bg-zinc-50 px-4 py-2.5 text-xs font-medium text-zinc-500 lg:grid dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400"><label class="flex items-center"><input type="checkbox" :checked="allLoadedSelected" class="size-4 rounded border-zinc-300 accent-teal-700" @change="toggleAllLoaded" /><span class="sr-only">Select all loaded sessions</span></label><span>Session</span><span>App / model</span><span>Outcome</span><span>Capture</span><span class="text-right">Tokens</span><span /></div>
      <div v-if="loading" aria-busy="true" aria-label="Loading sessions"><div v-for="index in 5" :key="index" class="grid gap-3 border-b border-zinc-200 px-4 py-5 last:border-b-0 lg:grid-cols-[28px_minmax(0,1fr)_150px_130px_150px_104px_28px] dark:border-zinc-800"><div class="h-4 w-4 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-3/5 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-24 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-20 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
      <div v-else-if="error && !sessions.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Sessions unavailable</p><p class="mx-auto mt-1 max-w-md text-sm text-zinc-500">{{ error }}</p><button class="mt-4 inline-flex h-8.5 items-center gap-2 rounded-[5px] border border-zinc-300 px-3 text-[13px] font-medium hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:border-zinc-700 dark:hover:bg-zinc-800" @click="load()"><RotateCw class="size-3.5" />Retry</button></div>
      <template v-else>
        <div v-for="block in blocks" :key="block.root.id" class="border-b border-zinc-200 last:border-b-0 dark:border-zinc-800">
          <div class="group relative grid gap-3 py-4 pl-12 pr-4 transition-colors hover:bg-stone-50 lg:grid-cols-[28px_minmax(0,1fr)_150px_130px_150px_104px_28px] lg:items-center lg:px-4 dark:hover:bg-stone-900/40">
            <RouterLink :to="`/sessions/${block.root.id}`" class="absolute inset-0 z-0 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600" :aria-label="`Open ${displayTitle(block.root)}`"><span class="sr-only">Open {{ displayTitle(block.root) }}</span></RouterLink>
            <label class="absolute left-4 top-4 z-20 flex items-center lg:static"><input type="checkbox" :checked="selectedSessionIds.has(block.root.id)" class="size-4 rounded border-zinc-300 accent-teal-700" @click.stop @change="toggleSession(block.root.id)" /><span class="sr-only">Select {{ displayTitle(block.root) }}</span></label>
            <div class="pointer-events-none relative z-10 min-w-0"><div class="flex min-w-0 items-center gap-2"><h2 class="min-w-0 truncate text-sm font-medium text-zinc-950 group-hover:underline dark:text-zinc-100">{{ displayTitle(block.root) }}</h2><SessionLivenessBadge class="shrink-0" :liveness="block.root.liveness" /></div><div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400"><span class="font-medium text-zinc-700 dark:text-zinc-300">{{ block.root.repo || "No repository" }}</span><DeviceIdentity v-if="block.root.device" :device="block.root.device" compact /><span v-if="block.root.source_ref" class="inline-flex items-center gap-1"><GitBranch class="size-3" />{{ block.root.source_ref }}</span><span>Active {{ duration(block.root.started_at, block.root.activity_at) }}</span><span>{{ relativeDate(block.root.activity_at) }}</span><button v-if="block.rows.length" type="button" class="pointer-events-auto inline-flex items-center gap-1 font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" :aria-expanded="expandedSubAgents.has(block.root.id)" :aria-controls="`sub-agents-${block.root.id}`" @click.prevent.stop="toggleSubAgents(block.root.id)"><ChevronRight class="size-3 transition-transform" :class="{ 'rotate-90': expandedSubAgents.has(block.root.id) }" aria-hidden="true" />{{ block.rows.length }} {{ block.rows.length === 1 ? "sub-agent" : "sub-agents" }}</button></div></div>
            <SessionModelStack class="pointer-events-none relative z-10" :app="block.root.harness" :primary="block.root.model_primary" :models="block.root.models" />
            <div class="pointer-events-none relative z-10"><OutcomeBadge :outcome="block.root.outcome" /></div>
            <div class="pointer-events-none relative z-10 text-xs text-zinc-700 dark:text-zinc-300"><span class="mr-1 text-zinc-500 lg:hidden">Capture</span><strong class="font-medium capitalize">{{ block.root.capture.status }}</strong> · {{ block.root.capture.saved_exchanges }} {{ block.root.capture.saved_exchanges === 1 ? "exchange" : "exchanges" }}<span v-if="block.root.capture.failed_exchanges"> · {{ block.root.capture.failed_exchanges }} failed</span></div>
            <div class="pointer-events-none relative z-10 text-left font-mono text-xs text-zinc-700 lg:text-right dark:text-zinc-300"><div><span class="mr-1 text-zinc-500 lg:hidden">Tokens</span>{{ compactNumber(block.root.tokens_in + block.root.tokens_out + (block.root.cache_read_tokens ?? 0) + (block.root.cache_write_tokens ?? 0)) }}</div><div v-if="(block.root.cache_read_tokens ?? 0) > 0 || (block.root.cache_write_tokens ?? 0) > 0" class="mt-0.5 text-[10px] text-zinc-500">{{ cacheHitRate(block.root) }}% cache<span v-if="(block.root.cache_write_tokens ?? 0) > 0"> · {{ compactNumber(block.root.cache_write_tokens ?? 0) }} write</span></div></div><ArrowRight class="pointer-events-none relative z-10 hidden size-4 text-zinc-400 transition-transform group-hover:translate-x-0.5 lg:block" aria-hidden="true" />
          </div>
          <div v-if="block.rows.length && expandedSubAgents.has(block.root.id)" :id="`sub-agents-${block.root.id}`" class="border-t border-zinc-200 bg-stone-50/60 dark:border-zinc-800 dark:bg-zinc-950/20">
              <div class="hidden grid-cols-[minmax(0,1fr)_260px_180px_20px] gap-4 border-b border-zinc-200 px-4 py-2 text-[11px] font-medium text-zinc-500 lg:grid dark:border-zinc-800">
                <span>Session</span><span>App / model</span><span>Outcome / activity</span><span />
              </div>
              <RouterLink v-for="row in block.rows" :key="row.session.id" :to="`/sessions/${row.session.id}`" class="group/row grid min-w-0 gap-x-4 gap-y-1 border-b border-zinc-200 px-4 py-2.5 last:border-b-0 hover:bg-stone-100 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 lg:grid-cols-[minmax(0,1fr)_260px_180px_20px] lg:items-center dark:border-zinc-800 dark:hover:bg-zinc-900">
                <div class="flex min-w-0 items-center gap-2" :style="row.depth > 1 ? { paddingLeft: `${(row.depth - 1) * 1.25}rem` } : undefined">
                  <CornerDownRight class="size-3.5 shrink-0 text-zinc-400" aria-hidden="true" />
                  <span class="min-w-0 truncate text-[13px] font-medium text-zinc-800 dark:text-zinc-200">{{ displayTitle(row.session) }}</span>
                </div>
                <p class="min-w-0 truncate text-xs text-zinc-500 dark:text-zinc-400"><span class="font-medium text-zinc-700 dark:text-zinc-300">{{ row.session.harness || "Unknown app" }}</span><span v-if="row.session.model_primary"> · {{ row.session.model_primary }}</span></p>
                <p class="text-xs text-zinc-500 dark:text-zinc-400"><span class="capitalize text-zinc-700 dark:text-zinc-300">{{ row.session.outcome }}</span> · {{ relativeDate(row.session.activity_at) }}</p>
                <ArrowRight class="hidden size-4 text-zinc-400 transition-transform group-hover/row:translate-x-0.5 lg:block" aria-hidden="true" />
              </RouterLink>
          </div>
        </div>
        <div v-if="!sessions.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">{{ activeFilterCount ? "No matching sessions" : "No sessions captured yet" }}</p><p class="mt-1 text-sm text-zinc-500">{{ activeFilterCount ? "Clear a filter or try a broader search." : "Captured model traffic will appear here as work sessions." }}</p></div>
      </template>
    </div>
    <div v-if="nextCursor || error" class="mt-4 flex items-center justify-between gap-4"><p class="text-xs text-red-700 dark:text-red-400" role="alert">{{ error }}</p><button v-if="nextCursor" :disabled="loading || loadingMore" class="ml-auto inline-flex h-8.5 items-center gap-2 rounded-[5px] border border-zinc-300 bg-white px-3 text-[13px] font-medium hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-wait disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-900 dark:hover:bg-zinc-800" @click="loadMore">{{ loadingMore ? "Loading..." : "Load more sessions" }}</button></div>
  </section>
</template>
