<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { Filter, RotateCw, Search, X } from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";
import IdentityBadge from "@/components/IdentityBadge.vue";
import Button from "@/components/ui/Button.vue";
import DropdownPanel from "@/components/ui/DropdownPanel.vue";
import Select from "@/components/ui/Select.vue";
import { errorMessage, listSessionExchanges, type SessionDetail, type SessionExchange, type SessionExchangeFilters } from "@/lib/api";
import { facetSelectOptions, useFacets } from "@/lib/facets";
import { shortDate } from "@/lib/format";
import { orderOptions, pageSizeOptions, type SelectOption } from "@/lib/options";
import { displayTitle } from "@/lib/sessions";

const SEARCH_DEBOUNCE_MS = 350;
const facets = [
  { key: "rmodel", label: "Model", field: "models", allLabel: "All models" },
  { key: "rprovider", label: "Provider", field: "providers", allLabel: "All providers" },
  { key: "rapp", label: "App", field: "apps", allLabel: "All apps" },
  { key: "rfinish", label: "Finish reason", field: "finish_reasons", allLabel: "All finish reasons" },
] as const;

const props = defineProps<{ sessionId: string; supportingSessions?: SessionDetail["supporting_sessions"]; refreshKey?: number }>();
const route = useRoute();
const router = useRouter();
const exchanges = ref<SessionExchange[]>([]);
const nextCursor = ref<string | null>(null);
const loading = ref(true);
const loadingMore = ref(false);
const error = ref("");
const filtersOpen = ref(false);
const search = ref("");
const draft = reactive<Record<string, string>>({ rmodel: "", rprovider: "", rapp: "", rfinish: "" });
// Timeline scope defaults to this session's own requests; selecting a
// supporting session restricts the timeline (and facets) to that sub-agent.
const scope = ref<string | null>(null);
const scopeOptions = computed<SelectOption[]>(() => [
  { value: "", label: "This session" },
  ...(props.supportingSessions ?? []).map((session) => ({ value: session.id, label: displayTitle(session) })),
]);
const { facets: facetValues } = useFacets(computed(() => scope.value ?? props.sessionId));
let controller: AbortController | null = null;
let searchTimer: ReturnType<typeof setTimeout> | undefined;

function optionsFor(facet: (typeof facets)[number]) {
  return facetSelectOptions(facetValues.value[facet.field], draft[facet.key] ?? "", facet.allLabel);
}

function queryValue(key: string) {
  const value = route.query[key];
  return typeof value === "string" ? value : "";
}

const order = computed(() => queryValue("rorder") || "desc");
const limit = computed(() => queryValue("rlimit") || "25");
const activeFacets = computed(() => facets.flatMap((facet) => queryValue(facet.key) ? [{ ...facet, value: queryValue(facet.key) }] : []));

function setParams(patch: Record<string, string>) {
  const query = { ...route.query } as Record<string, string>;
  for (const [key, raw] of Object.entries(patch)) {
    const value = raw.trim();
    const isDefault = (key === "rorder" && value === "desc") || (key === "rlimit" && value === "25");
    if (!value || isDefault) delete query[key];
    else query[key] = value;
  }
  void router.push({ query });
}

function currentFilters(cursor?: string): SessionExchangeFilters {
  return {
    q: queryValue("rq") || undefined,
    model: queryValue("rmodel") || undefined,
    provider: queryValue("rprovider") || undefined,
    app: queryValue("rapp") || undefined,
    finishReason: queryValue("rfinish") || undefined,
    // The timeline always shows one session's own exchanges. The scope select
    // picks which session; the default is the page's session.
    session: scope.value ?? props.sessionId,
    order: order.value as "asc" | "desc",
    limit: Number(limit.value),
    cursor,
  };
}

async function load(background = false) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loadingMore.value = false;
  nextCursor.value = null;
  if (!background) loading.value = true;
  error.value = "";
  try {
    const result = await listSessionExchanges(props.sessionId, currentFilters(), active.signal);
    if (background && exchanges.value.length) {
      const combined = order.value === "desc" ? [...result.exchanges, ...exchanges.value] : [...exchanges.value, ...result.exchanges];
      exchanges.value = combined.filter((exchange, index) => combined.findIndex((candidate) => candidate.id === exchange.id) === index);
      if (!nextCursor.value && exchanges.value.length <= result.exchanges.length) nextCursor.value = result.next_cursor;
    } else {
      exchanges.value = result.exchanges;
      nextCursor.value = result.next_cursor;
    }
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "Request evidence could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

async function loadMore() {
  if (!nextCursor.value || loading.value || loadingMore.value) return;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loadingMore.value = true;
  error.value = "";
  try {
    const result = await listSessionExchanges(props.sessionId, currentFilters(nextCursor.value), active.signal);
    exchanges.value.push(...result.exchanges);
    nextCursor.value = result.next_cursor;
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "More request evidence could not be loaded.");
  } finally {
    if (controller === active) loadingMore.value = false;
  }
}

function commitSearch() {
  clearTimeout(searchTimer);
  if (search.value.trim() !== queryValue("rq")) setParams({ rq: search.value });
}

function applyDraft() {
  setParams({ ...draft });
  filtersOpen.value = false;
}

function resetDraft() {
  for (const facet of facets) draft[facet.key] = "";
}

function clearAll() {
  resetDraft();
  search.value = "";
  clearTimeout(searchTimer);
  setParams({ rq: "", rmodel: "", rprovider: "", rapp: "", rfinish: "" });
  filtersOpen.value = false;
}

watch(search, (value) => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (value.trim() !== queryValue("rq")) setParams({ rq: value });
  }, SEARCH_DEBOUNCE_MS);
});
watch(filtersOpen, (open) => {
  if (open) for (const facet of facets) draft[facet.key] = queryValue(facet.key);
});

watch([() => props.sessionId, () => route.fullPath], () => {
  if (queryValue("rq") !== search.value.trim()) search.value = queryValue("rq");
  void load();
}, { immediate: true });
watch(() => props.sessionId, () => { scope.value = null; });
watch(() => props.refreshKey, () => { void load(true); });
onBeforeUnmount(() => { controller?.abort(); clearTimeout(searchTimer); });
</script>

<template>
  <section id="session-activity" aria-labelledby="timeline-heading">
    <div class="mb-3">
      <div>
        <h2 id="timeline-heading" class="text-base font-semibold text-zinc-900 dark:text-zinc-100">Activity</h2>
        <p class="mt-1 text-xs text-zinc-500">{{ exchanges.length }} {{ exchanges.length === 1 ? "request" : "requests" }} loaded · {{ order === "desc" ? "Newest first" : "Oldest first" }}</p>
      </div>
    </div>

    <div class="border-y border-zinc-200 py-3 dark:border-zinc-800">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div class="flex min-w-0 flex-1 flex-wrap gap-2">
        <Select v-if="props.supportingSessions?.length" :model-value="scope ?? ''" label="Session scope" :options="scopeOptions" class="w-full sm:w-52" @update:model-value="scope = $event || null" />
        <form class="relative min-w-0 flex-1 lg:max-w-lg" role="search" @submit.prevent="commitSearch">
          <label class="sr-only" for="timeline-search">Search request evidence</label>
          <Search class="pointer-events-none absolute left-2.5 top-2.25 size-4 text-zinc-400" aria-hidden="true" />
          <input id="timeline-search" v-model="search" type="search" placeholder="Search request excerpt or ID" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white pl-8.5 pr-3 text-[13px] focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" />
        </form>
        <DropdownPanel v-model:open="filtersOpen" title="Filter requests" description="Exact matches within the selected session's requests.">
          <template #trigger><Button variant="outline"><Filter class="size-3.5" />Filters<span v-if="activeFacets.length" class="font-mono text-[11px] text-zinc-500">{{ activeFacets.length }}</span></Button></template>
          <form id="timeline-filters" class="grid gap-3 sm:grid-cols-2" @submit.prevent="applyDraft">
            <div v-for="facet in facets" :key="facet.key" class="text-xs font-medium text-zinc-600 dark:text-zinc-400">
              <span class="mb-1 block">{{ facet.label }}</span>
              <Select v-model="draft[facet.key]" :label="facet.label" :options="optionsFor(facet)" :placeholder="facet.allLabel" class="w-full font-normal" />
            </div>
          </form>
          <template #footer>
            <Button variant="ghost" @click="clearAll">Clear all</Button>
            <Button variant="outline" @click="filtersOpen = false">Cancel</Button>
            <Button type="submit" form="timeline-filters">Apply filters</Button>
          </template>
        </DropdownPanel>
        </div>
        <div class="flex gap-2">
          <Select :model-value="order" label="Timeline order" :options="orderOptions" class="min-w-0 flex-1 sm:w-36 sm:flex-none" @update:model-value="setParams({ rorder: $event })" />
          <Select :model-value="limit" label="Requests per page" :options="pageSizeOptions" class="min-w-0 flex-1 sm:w-28 sm:flex-none" @update:model-value="setParams({ rlimit: $event })" />
        </div>
      </div>
      <ul v-if="activeFacets.length" class="mt-2.5 flex flex-wrap items-center gap-2">
        <li v-for="facet in activeFacets" :key="facet.key">
          <button type="button" class="inline-flex items-center gap-1.5 rounded-[5px] border border-zinc-300 px-2 py-1 text-[11px] text-zinc-700 transition-colors duration-150 ease-out hover:border-zinc-400 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800" @click="setParams({ [facet.key]: '' })">
            <span class="text-zinc-500">{{ facet.label }}</span><span class="font-mono">{{ facet.value }}</span><X class="size-3" aria-hidden="true" />
            <span class="sr-only">Remove {{ facet.label }} filter</span>
          </button>
        </li>
        <li><button type="button" class="text-[11px] font-medium text-zinc-500 hover:text-zinc-900 focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:hover:text-zinc-100" @click="clearAll">Clear all</button></li>
      </ul>
    </div>

    <div>
      <div v-if="loading" aria-busy="true" aria-label="Loading request timeline"><div v-for="index in 4" :key="index" class="grid gap-3 border-b border-zinc-200 py-4 sm:grid-cols-[120px_minmax(0,1fr)_100px] sm:px-3 dark:border-zinc-800"><div class="h-3 w-24 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-2/3 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
      <div v-else-if="error && !exchanges.length" class="border-b border-zinc-200 py-10 text-center dark:border-zinc-800"><p class="text-sm text-zinc-600 dark:text-zinc-400">{{ error }}</p><button class="mt-3 inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load()"><RotateCw class="size-4" />Retry</button></div>
      <template v-else>
        <RouterLink v-for="exchange in exchanges" :key="exchange.id" :to="{ path: `/requests/${exchange.id}`, query: { session: sessionId } }" class="grid gap-3 border-b border-zinc-200 py-4 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 sm:grid-cols-[136px_minmax(0,1fr)_auto] sm:px-3 dark:border-zinc-800 dark:hover:bg-stone-900">
          <div><time class="whitespace-nowrap font-mono text-xs text-zinc-500" :datetime="exchange.ts">{{ shortDate(exchange.ts) }}</time><p class="mt-1 font-mono text-xs text-zinc-600 dark:text-zinc-400">{{ exchange.latency_ms.toLocaleString() }} ms</p></div>
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2"><IdentityBadge :label="exchange.provider || 'Unknown provider'" /><IdentityBadge :label="exchange.model" /><span v-if="exchange.finish_reason" class="text-xs text-zinc-500">{{ exchange.finish_reason }}</span></div>
            <p class="mt-2 line-clamp-2 break-words text-[13px] leading-5 text-zinc-600 dark:text-zinc-400">{{ exchange.request_excerpt || exchange.id }}</p>
            <p v-if="exchange.capture_status !== 'saved' || exchange.failure_code" class="mt-1.5 text-xs" :class="exchange.capture_status === 'failed' ? 'text-red-700 dark:text-red-400' : 'text-amber-700 dark:text-amber-400'">Capture {{ exchange.capture_status }}<template v-if="exchange.failure_code"> · {{ exchange.failure_code }}</template><template v-else-if="exchange.capture_reason"> · {{ exchange.capture_reason }}</template></p>
          </div>
          <dl class="flex gap-4 text-right font-mono text-[11px] text-zinc-500 sm:block"><div><dt class="sr-only">Input tokens</dt><dd>{{ exchange.input_tokens.toLocaleString() }} in</dd></div><div><dt class="sr-only">Output tokens</dt><dd>{{ exchange.output_tokens.toLocaleString() }} out</dd></div></dl>
        </RouterLink>
        <div v-if="!exchanges.length" class="border-b border-zinc-200 py-10 dark:border-zinc-800"><p class="text-sm font-medium text-zinc-700 dark:text-zinc-300">No saved requests match this view.</p><p class="mt-1 text-xs text-zinc-500">Clear filters or wait for pending capture to finish.</p></div>
      </template>
    </div>
    <div v-if="nextCursor || error" class="mt-3 flex items-center justify-between gap-4"><p class="text-xs text-red-700 dark:text-red-400" role="alert">{{ error }}</p><Button v-if="nextCursor" variant="outline" class="ml-auto" :disabled="loading || loadingMore" @click="loadMore">{{ loadingMore ? "Loading..." : "Load more requests" }}</Button></div>
  </section>
</template>
