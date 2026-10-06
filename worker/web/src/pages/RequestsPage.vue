<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowRight, Filter, RotateCw, Search, X } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import Button from "@/components/ui/Button.vue";
import DropdownPanel from "@/components/ui/DropdownPanel.vue";
import Select from "@/components/ui/Select.vue";
import { errorMessage, listExchanges, type Exchange, type ExchangeFilters } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { facetSelectOptions, useFacets } from "@/lib/facets";
import { compactNumber, outputSpeed, shortDate } from "@/lib/format";
import { orderOptions, pageSizeOptions } from "@/lib/options";
import { requestKindOptions, exchangeCaptureOptions, errorFilterOptions } from "@/lib/request-filters";

const route = useRoute();
const router = useRouter();
const exchanges = ref<Exchange[]>([]);
const search = ref("");
const nextCursor = ref<string | null>(null);
const loading = ref(true);
const loadingMore = ref(false);
const error = ref("");
const filtersOpen = ref(false);
const fields = [
  { key: "repo", label: "Repository", facet: "repos", all: "All repositories" },
  { key: "model", label: "Model", facet: "models", all: "All models" },
  { key: "provider", label: "Provider", facet: "providers", all: "All providers" },
  { key: "app", label: "App", facet: "apps", all: "All apps" },
  { key: "finish_reason", label: "Finish reason", facet: "finish_reasons", all: "All finish reasons" },
  { key: "tool", label: "Tool", facet: "tools", all: "All tools" },
] as const;
const secondaryFields = fields.filter((field) => field.key !== "repo");
const keys = [...fields.map((field) => field.key), "session", "request_kind", "capture_status", "errors", "from", "to"] as const;
const labels: Record<string, string> = { ...Object.fromEntries(fields.map((field) => [field.key, field.label])), session: "Session ID", request_kind: "Request kind", capture_status: "Capture", errors: "Errors", from: "From (UTC)", to: "To (UTC)" };
const draft = reactive<Record<string, string>>(Object.fromEntries(keys.map((key) => [key, ""])));
const { facets } = useFacets();
let controller: AbortController | null = null;
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let pageCount = 1;

function queryValue(key: string) {
  const value = route.query[key];
  return typeof value === "string" ? value : "";
}
const activeFilters = computed(() => keys.flatMap((key) => queryValue(key) ? [{ key, label: labels[key], value: queryValue(key) }] : []));
const secondaryFilters = computed(() => activeFilters.value.filter((filter) => filter.key !== "repo"));
const order = computed(() => queryValue("order") || "desc");
const limit = computed(() => queryValue("limit") || "50");
function setParams(patch: Record<string, string>) {
  const query = { ...route.query };
  for (const [key, raw] of Object.entries(patch)) {
    const value = raw.trim();
    if (!value || (key === "order" && value === "desc") || (key === "limit" && value === "50")) delete query[key];
    else query[key] = value;
  }
  delete query.cursor;
  void router.push({ query });
}
function currentFilters(cursor?: string): ExchangeFilters {
  return {
    q: queryValue("q") || undefined, repo: queryValue("repo") || undefined,
    model: queryValue("model") || undefined, provider: queryValue("provider") || undefined,
    app: queryValue("app") || undefined, session: queryValue("session") || undefined,
    finishReason: queryValue("finish_reason") || undefined,
    requestKind: (queryValue("request_kind") || undefined) as ExchangeFilters["requestKind"],
    captureStatus: (queryValue("capture_status") || undefined) as ExchangeFilters["captureStatus"],
    tool: queryValue("tool") || undefined, errors: (queryValue("errors") || undefined) as "true" | undefined,
    from: queryValue("from") || undefined, to: queryValue("to") || undefined,
    order: order.value as "asc" | "desc", limit: Number(limit.value), cursor,
  };
}
async function load(reset = true, silent = false) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  if (reset) { if (!silent) loading.value = true; loadingMore.value = false; }
  else loadingMore.value = true;
  error.value = "";
  try {
    if (reset) {
      const rows: Exchange[] = [];
      let cursor: string | undefined;
      let next: string | null = null;
      let pages = 0;
      for (let index = 0; index < (silent ? pageCount : 1); index++) {
        const result = await listExchanges(currentFilters(cursor), active.signal);
        rows.push(...result.exchanges);
        pages++;
        next = result.next_cursor;
        if (!next) break;
        cursor = next;
      }
      exchanges.value = rows;
      nextCursor.value = next;
      pageCount = pages;
    } else {
      const result = await listExchanges(currentFilters(nextCursor.value ?? undefined), active.signal);
      exchanges.value.push(...result.exchanges);
      nextCursor.value = result.next_cursor;
      pageCount++;
    }
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "Requests could not be loaded.");
  } finally {
    if (!active.signal.aborted) { loading.value = false; loadingMore.value = false; }
  }
}
function commitSearch() {
  clearTimeout(searchTimer);
  if (search.value.trim() !== queryValue("q")) setParams({ q: search.value });
}
function clearFilters() {
  search.value = "";
  clearTimeout(searchTimer);
  setParams(Object.fromEntries([...keys, "q"].map((key) => [key, ""])));
  filtersOpen.value = false;
}
watch(search, () => { clearTimeout(searchTimer); searchTimer = setTimeout(commitSearch, 350); });
watch(filtersOpen, (open) => { if (open) for (const key of keys) draft[key] = queryValue(key); });
watch(() => route.fullPath, () => { search.value = queryValue("q"); void load(); }, { immediate: true });
useAutoRefresh(() => { if (!loading.value && !loadingMore.value) return load(true, true); });
onBeforeUnmount(() => { controller?.abort(); clearTimeout(searchTimer); });
</script>

<template>
  <section>
    <div class="mb-4 flex flex-wrap items-baseline justify-between gap-2"><h1 class="text-2xl font-semibold tracking-tight">Requests</h1><p v-if="!loading" class="text-xs text-zinc-600 dark:text-zinc-400">{{ exchanges.length }} {{ exchanges.length === 1 ? 'request' : 'requests' }}</p></div>
    <div class="mb-4">
      <div class="flex flex-wrap items-center gap-2">
        <Select :model-value="queryValue('repo')" label="Repository" :options="facetSelectOptions(facets.repos, queryValue('repo'), 'All repositories')" class="w-full sm:w-56" @update:model-value="setParams({ repo: $event })" />
        <form class="relative min-w-0 flex-1 sm:w-72 sm:flex-none" role="search" @submit.prevent="commitSearch"><label class="sr-only" for="request-search">Search all requests and responses</label><Search class="pointer-events-none absolute left-2.5 top-2.25 size-4 text-zinc-400" aria-hidden="true" /><input id="request-search" v-model="search" type="search" placeholder="Search requests" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-transparent pl-8.5 pr-3 text-[13px] focus-visible:outline-2 focus-visible:outline-teal-600 dark:border-zinc-700" /></form>
        <DropdownPanel v-model:open="filtersOpen" title="Filter requests">
          <template #trigger><Button variant="outline"><Filter class="size-3.5" />Filters<span v-if="secondaryFilters.length" class="font-mono text-[11px]">{{ secondaryFilters.length }}</span></Button></template>
          <form id="request-filters" class="grid gap-3 sm:grid-cols-2" @submit.prevent="setParams({ ...draft }); filtersOpen = false">
            <div v-for="field in secondaryFields" :key="field.key" class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">{{ field.label }}</span><Select v-model="draft[field.key]" :label="field.label" :options="facetSelectOptions(facets[field.facet] ?? [], draft[field.key] ?? '', field.all)" class="w-full font-normal" /></div>
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Request kind</span><Select v-model="draft.request_kind" label="Request kind" :options="requestKindOptions" class="w-full font-normal" /></div>
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Capture</span><Select v-model="draft.capture_status" label="Capture" :options="exchangeCaptureOptions" class="w-full font-normal" /></div>
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Errors</span><Select v-model="draft.errors" label="Errors" :options="errorFilterOptions" class="w-full font-normal" /></div>
            <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Session ID<input v-model="draft.session" type="text" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 font-mono text-xs focus:outline-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
            <label v-for="key in ['from', 'to']" :key="key" class="text-xs font-medium text-zinc-600 dark:text-zinc-400">{{ labels[key] }}<input v-model="draft[key]" type="date" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal focus:outline-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
          </form>
          <template #footer><Button variant="ghost" @click="clearFilters">Clear all</Button><Button variant="outline" @click="filtersOpen = false">Cancel</Button><Button type="submit" form="request-filters">Apply filters</Button></template>
        </DropdownPanel>
      </div>
      <ul v-if="secondaryFilters.length || queryValue('q')" class="mt-2 flex flex-wrap items-center gap-2"><li v-for="filter in secondaryFilters" :key="filter.key"><button class="inline-flex items-center gap-1.5 rounded-[5px] border border-zinc-300 px-2 py-1 text-[11px] focus-visible:outline-2 focus-visible:outline-teal-600 dark:border-zinc-700" @click="setParams({ [filter.key]: '' })">{{ filter.label }}: {{ filter.value }}<X class="size-3" aria-hidden="true" /><span class="sr-only">Remove {{ filter.label }} filter</span></button></li><li><button class="text-xs font-medium text-zinc-600 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-zinc-400" @click="clearFilters">Clear all</button></li></ul>
    </div>
    <div class="overflow-hidden rounded-[7px] border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
      <div class="overflow-x-auto"><table class="w-full border-collapse text-left lg:min-w-[1100px]"><thead><tr class="border-b border-zinc-200 bg-zinc-50 text-xs font-medium text-zinc-600 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400">
        <th class="hidden px-4 py-2.5 font-medium lg:table-cell">Date</th><th class="px-4 py-2.5 font-medium">Request</th><th class="hidden px-4 py-2.5 font-medium xl:table-cell">Provider</th><th class="hidden px-4 py-2.5 font-medium xl:table-cell">App</th><th class="hidden px-4 py-2.5 font-medium lg:table-cell">Repo</th><th class="hidden px-4 py-2.5 font-medium md:table-cell">Kind / capture</th><th class="hidden px-4 py-2.5 text-right font-medium sm:table-cell">Input</th><th class="hidden px-4 py-2.5 text-right font-medium sm:table-cell">Output</th><th class="hidden px-4 py-2.5 text-right font-medium xl:table-cell">Speed</th><th class="hidden px-4 py-2.5 font-medium xl:table-cell">Finish</th><th class="w-10"><span class="sr-only">Open request</span></th>
      </tr></thead>
        <tbody v-if="!loading"><tr v-for="exchange in exchanges" :key="exchange.id" class="group border-b border-zinc-200 text-[13px] last:border-b-0 hover:bg-stone-50 dark:border-zinc-800 dark:hover:bg-zinc-800/70">
          <td class="hidden px-4 py-3.5 font-mono text-xs text-zinc-600 lg:table-cell dark:text-zinc-400">{{ shortDate(exchange.ts) }}</td>
          <td class="max-w-72 px-4 py-3.5"><RouterLink :to="`/requests/${exchange.id}`" :aria-label="`Open ${exchange.model || 'unknown model'} request`" class="block min-w-0 rounded-[3px] font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600">
            <IdentityBadge :label="exchange.model || 'Unknown model'" />
            <p v-if="exchange.request_excerpt" class="mt-1 line-clamp-1 text-xs font-normal text-zinc-600 dark:text-zinc-400">{{ exchange.request_excerpt }}</p>
            <p class="mt-1 flex flex-wrap gap-x-2 text-xs font-normal text-zinc-600 lg:hidden dark:text-zinc-400"><time :datetime="exchange.ts">{{ shortDate(exchange.ts) }}</time><span>{{ exchange.harness }}</span></p>
            <p class="mt-1 text-xs font-normal md:hidden" :class="exchange.capture_status === 'failed' ? 'text-red-700 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-400'">{{ exchange.request_kind || 'primary' }} · {{ exchange.capture_status === 'accepted' ? 'Pending' : exchange.capture_status === 'failed' ? 'Capture failed' : exchange.capture_status || 'saved' }} · {{ compactNumber(exchange.input_tokens) }} in / {{ compactNumber(exchange.output_tokens) }} out</p>
          </RouterLink></td>
          <td class="hidden px-4 py-3.5 xl:table-cell"><IdentityBadge :label="exchange.provider || 'Unknown'" /></td><td class="hidden px-4 py-3.5 xl:table-cell"><IdentityBadge :label="exchange.harness || 'Unknown'" /></td><td class="hidden px-4 py-3.5 text-zinc-600 lg:table-cell dark:text-zinc-400">{{ exchange.repo || "None" }}</td>
          <td class="hidden px-4 py-3.5 md:table-cell"><span class="text-xs">{{ exchange.request_kind || 'primary' }}</span><p class="mt-1 text-xs" :class="exchange.capture_status === 'failed' ? 'text-red-700 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-400'">{{ exchange.capture_status === 'accepted' ? 'Pending' : exchange.capture_status === 'failed' ? 'Capture failed' : exchange.capture_status || 'saved' }}<span v-if="exchange.failure_code" class="font-mono"> · {{ exchange.failure_code }}</span></p></td>
          <td class="hidden px-4 py-3.5 text-right font-mono text-xs sm:table-cell">{{ compactNumber(exchange.input_tokens) }}</td><td class="hidden px-4 py-3.5 text-right font-mono text-xs sm:table-cell">{{ compactNumber(exchange.output_tokens) }}</td><td class="hidden px-4 py-3.5 text-right font-mono text-xs text-zinc-600 xl:table-cell dark:text-zinc-400">{{ outputSpeed(exchange.output_tokens, exchange.latency_ms) }}</td><td class="hidden px-4 py-3.5 font-mono text-xs text-zinc-600 xl:table-cell dark:text-zinc-400">{{ exchange.finish_reason || "Unknown" }}</td>
          <td class="pr-3"><ArrowRight class="size-4 text-zinc-400" aria-hidden="true" /></td>
        </tr></tbody>
      </table></div>
      <div v-if="loading" aria-busy="true" class="divide-y divide-zinc-200 dark:divide-zinc-800"><div v-for="index in 6" :key="index" class="grid grid-cols-4 gap-8 px-4 py-5"><span class="h-3 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><span class="h-3 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><span class="h-3 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
      <div v-else-if="error && !exchanges.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Requests unavailable</p><p class="mt-1 text-sm text-zinc-500">{{ error }}</p><button class="mt-4 inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load(true)"><RotateCw class="size-4" />Retry</button></div>
      <div v-else-if="!exchanges.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">No requests match this view</p><p class="mt-1 text-sm text-zinc-500">Clear filters or try a broader search.</p></div>
    </div>
    <div class="mt-4 flex flex-wrap items-center gap-3">
      <Button v-if="nextCursor" variant="outline" :disabled="loadingMore" @click="load(false)">{{ loadingMore ? "Loading…" : "Load more" }}</Button>
      <span v-if="error && exchanges.length" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ error }}</span>
      <div class="ml-auto flex items-center gap-2"><Select :model-value="order" label="Request order" :options="orderOptions" class="w-36" @update:model-value="setParams({ order: $event })" /><span class="text-xs text-zinc-600 dark:text-zinc-400">Rows</span><Select :model-value="limit" label="Requests per page" :options="pageSizeOptions" class="w-24" @update:model-value="setParams({ limit: $event })" /></div>
    </div>
  </section>
</template>
