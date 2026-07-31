<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { ArrowDown, ArrowUp, Filter, RotateCw, Search, X } from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";
import IdentityBadge from "@/components/IdentityBadge.vue";
import Button from "@/components/ui/Button.vue";
import DropdownPanel from "@/components/ui/DropdownPanel.vue";
import Select from "@/components/ui/Select.vue";
import { errorMessage, listSessionExchanges, type SessionExchange, type SessionExchangeFilters } from "@/lib/api";
import { facetSelectOptions, useFacets } from "@/lib/facets";
import { shortDate } from "@/lib/format";
import { orderOptions, pageSizeOptions } from "@/lib/options";

const SEARCH_DEBOUNCE_MS = 350;
const facets = [
  { key: "rmodel", label: "Model", field: "models", allLabel: "All models" },
  { key: "rprovider", label: "Provider", field: "providers", allLabel: "All providers" },
  { key: "rapp", label: "App", field: "apps", allLabel: "All apps" },
  { key: "rfinish", label: "Finish reason", field: "finish_reasons", allLabel: "All finish reasons" },
] as const;

const props = defineProps<{ sessionId: string }>();
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
const { facets: facetValues } = useFacets(computed(() => props.sessionId));
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
    order: order.value as "asc" | "desc",
    limit: Number(limit.value),
    cursor,
  };
}

async function load() {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loadingMore.value = false;
  nextCursor.value = null;
  loading.value = true;
  error.value = "";
  try {
    const result = await listSessionExchanges(props.sessionId, currentFilters(), active.signal);
    exchanges.value = result.exchanges;
    nextCursor.value = result.next_cursor;
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
onBeforeUnmount(() => { controller?.abort(); clearTimeout(searchTimer); });
</script>

<template>
  <section aria-labelledby="timeline-heading">
    <div class="mb-3 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h2 id="timeline-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Request timeline</h2>
        <p class="mt-1 text-xs text-zinc-500">{{ exchanges.length }} loaded · {{ order === "desc" ? "Newest first" : "Oldest first" }}</p>
      </div>
    </div>

    <div class="border-y border-zinc-200 py-3 dark:border-zinc-800">
      <div class="flex flex-col gap-2 sm:flex-row">
        <form class="relative min-w-0 flex-1" role="search" @submit.prevent="commitSearch">
          <label class="sr-only" for="timeline-search">Search request evidence</label>
          <Search class="pointer-events-none absolute left-2.5 top-2.25 size-4 text-zinc-400" aria-hidden="true" />
          <input id="timeline-search" v-model="search" type="search" placeholder="Search request excerpt or ID" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white pl-8.5 pr-3 text-[13px] focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" />
        </form>
        <DropdownPanel v-model:open="filtersOpen" title="Filter requests" description="Exact matches within this session and its supporting runs.">
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
        <Select :model-value="order" label="Timeline order" :options="orderOptions" class="sm:w-36" @update:model-value="setParams({ rorder: $event })" />
        <Select :model-value="limit" label="Requests per page" :options="pageSizeOptions" class="sm:w-28" @update:model-value="setParams({ rlimit: $event })" />
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

    <div class="border-t border-zinc-200 dark:border-zinc-800">
      <div v-if="loading" aria-busy="true" aria-label="Loading request timeline"><div v-for="index in 4" :key="index" class="grid gap-3 border-b border-zinc-200 py-4 sm:grid-cols-[120px_minmax(0,1fr)_100px] sm:px-3 dark:border-zinc-800"><div class="h-3 w-24 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-2/3 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
      <div v-else-if="error && !exchanges.length" class="border-b border-zinc-200 py-10 text-center dark:border-zinc-800"><p class="text-sm text-zinc-600 dark:text-zinc-400">{{ error }}</p><button class="mt-3 inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button></div>
      <template v-else>
        <RouterLink v-for="exchange in exchanges" :key="exchange.id" :to="`/requests/${exchange.id}`" class="grid gap-3 border-b border-zinc-200 py-4 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 sm:grid-cols-[120px_minmax(0,1fr)_100px] sm:px-3 dark:border-zinc-800 dark:hover:bg-zinc-900"><time class="font-mono text-xs text-zinc-500">{{ shortDate(exchange.ts) }}</time><div><div class="flex flex-wrap items-center gap-3"><IdentityBadge :label="exchange.provider || 'Unknown provider'" /><IdentityBadge :label="exchange.model" /><span v-if="exchange.finish_reason" class="text-[11px] text-zinc-500">{{ exchange.finish_reason }}</span></div><p class="mt-2 line-clamp-1 text-xs text-zinc-500">{{ exchange.request_excerpt || exchange.id }}</p></div><div class="flex items-center gap-1 font-mono text-xs text-zinc-500 sm:justify-end"><ArrowDown v-if="order === 'desc'" class="size-3" /><ArrowUp v-else class="size-3" />{{ exchange.input_tokens.toLocaleString() }} in</div></RouterLink>
        <p v-if="!exchanges.length" class="border-b border-zinc-200 py-10 text-sm text-zinc-500 dark:border-zinc-800">No request evidence matches these filters.</p>
      </template>
    </div>
    <div v-if="nextCursor || error" class="mt-3 flex items-center justify-between gap-4"><p class="text-xs text-red-700 dark:text-red-400" role="alert">{{ error }}</p><Button v-if="nextCursor" variant="outline" class="ml-auto" :disabled="loading || loadingMore" @click="loadMore">{{ loadingMore ? "Loading..." : "Load more requests" }}</Button></div>
  </section>
</template>
