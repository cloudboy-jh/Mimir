<script setup lang="ts">
import { onBeforeUnmount, reactive, ref, watch } from "vue";
import { ArrowDown, ArrowUp, Filter, RotateCw, Search, X } from "lucide-vue-next";
import { useRoute, useRouter } from "vue-router";
import IdentityBadge from "@/components/IdentityBadge.vue";
import { errorMessage, listSessionExchanges, type SessionExchange, type SessionExchangeFilters } from "@/lib/api";
import { shortDate } from "@/lib/format";

const props = defineProps<{ sessionId: string }>();
const route = useRoute();
const router = useRouter();
const exchanges = ref<SessionExchange[]>([]);
const nextCursor = ref<string | null>(null);
const loading = ref(true);
const loadingMore = ref(false);
const error = ref("");
const filtersOpen = ref(false);
const draft = reactive({ q: "", model: "", provider: "", app: "", finishReason: "", order: "desc", limit: "25" });
let controller: AbortController | null = null;

function queryValue(key: string) {
  const value = route.query[key];
  return typeof value === "string" ? value : "";
}

function syncDraft() {
  draft.q = queryValue("rq");
  draft.model = queryValue("rmodel");
  draft.provider = queryValue("rprovider");
  draft.app = queryValue("rapp");
  draft.finishReason = queryValue("rfinish");
  draft.order = queryValue("rorder") || "desc";
  draft.limit = queryValue("rlimit") || "25";
  filtersOpen.value ||= [draft.model, draft.provider, draft.app, draft.finishReason].some(Boolean);
}

function currentFilters(cursor?: string): SessionExchangeFilters {
  return {
    q: queryValue("rq") || undefined,
    model: queryValue("rmodel") || undefined,
    provider: queryValue("rprovider") || undefined,
    app: queryValue("rapp") || undefined,
    finishReason: queryValue("rfinish") || undefined,
    order: (queryValue("rorder") || "desc") as "asc" | "desc",
    limit: Number(queryValue("rlimit") || 25),
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

function applyFilters() {
  const query = { ...route.query } as Record<string, string>;
  for (const key of ["rq", "rmodel", "rprovider", "rapp", "rfinish", "rorder", "rlimit"]) delete query[key];
  const values = { rq: draft.q, rmodel: draft.model, rprovider: draft.provider, rapp: draft.app, rfinish: draft.finishReason, rorder: draft.order, rlimit: draft.limit };
  for (const [key, raw] of Object.entries(values)) {
    const value = raw.trim();
    if (value && !(key === "rorder" && value === "desc") && !(key === "rlimit" && value === "25")) query[key] = value;
  }
  void router.push({ query });
}

function clearFilters() {
  draft.q = "";
  draft.model = "";
  draft.provider = "";
  draft.app = "";
  draft.finishReason = "";
  draft.order = "desc";
  draft.limit = "25";
  applyFilters();
}

watch([() => props.sessionId, () => route.fullPath], () => {
  syncDraft();
  void load();
}, { immediate: true });
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section aria-labelledby="timeline-heading">
    <div class="mb-3 flex flex-wrap items-end justify-between gap-3"><div><h2 id="timeline-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Request timeline</h2><p class="mt-1 text-xs text-zinc-500">{{ exchanges.length }} loaded · {{ draft.order === 'desc' ? 'Newest first' : 'Oldest first' }}</p></div><button type="button" class="inline-flex h-8.5 items-center gap-2 rounded-[5px] border border-zinc-300 px-3 text-[13px] font-medium hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:border-zinc-700 dark:hover:bg-zinc-900" :aria-expanded="filtersOpen" @click="filtersOpen = !filtersOpen"><Filter class="size-3.5" />Filters</button></div>
    <form class="mb-3 border-y border-zinc-200 py-3 dark:border-zinc-800" @submit.prevent="applyFilters">
      <div class="flex flex-col gap-2 sm:flex-row">
        <label class="relative min-w-0 flex-1"><span class="sr-only">Search request evidence</span><Search class="pointer-events-none absolute left-2.5 top-2.25 size-4 text-zinc-400" /><input v-model="draft.q" type="search" placeholder="Search request excerpt or ID" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white pl-8.5 pr-3 text-[13px] focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
        <label><span class="sr-only">Timeline order</span><select v-model="draft.order" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900"><option value="desc">Newest first</option><option value="asc">Oldest first</option></select></label>
        <label><span class="sr-only">Requests per page</span><select v-model="draft.limit" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900"><option value="10">10 rows</option><option value="25">25 rows</option><option value="50">50 rows</option><option value="100">100 rows</option></select></label>
        <button type="submit" class="h-8.5 rounded-[5px] bg-zinc-900 px-3 text-[13px] font-medium text-zinc-50 hover:bg-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:bg-zinc-100 dark:text-zinc-900">Apply</button>
      </div>
      <div v-if="filtersOpen" class="mt-3 grid gap-3 border-t border-zinc-200 pt-3 sm:grid-cols-2 lg:grid-cols-4 dark:border-zinc-800">
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Model<input v-model="draft.model" placeholder="Exact model" class="mt-1 h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Provider<input v-model="draft.provider" placeholder="Exact provider" class="mt-1 h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">App<input v-model="draft.app" placeholder="Exact app" class="mt-1 h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Finish reason<input v-model="draft.finishReason" placeholder="Exact finish reason" class="mt-1 h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
        <button type="button" class="inline-flex items-center gap-1.5 text-xs font-medium text-zinc-500 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:hover:text-zinc-100" @click="clearFilters"><X class="size-3.5" />Clear request filters</button>
      </div>
    </form>

    <div class="border-t border-zinc-200 dark:border-zinc-800">
      <div v-if="loading" aria-busy="true" aria-label="Loading request timeline"><div v-for="index in 4" :key="index" class="grid gap-3 border-b border-zinc-200 py-4 sm:grid-cols-[120px_minmax(0,1fr)_100px] sm:px-3 dark:border-zinc-800"><div class="h-3 w-24 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-2/3 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
      <div v-else-if="error && !exchanges.length" class="border-b border-zinc-200 py-10 text-center dark:border-zinc-800"><p class="text-sm text-zinc-600 dark:text-zinc-400">{{ error }}</p><button class="mt-3 inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button></div>
      <template v-else>
        <RouterLink v-for="exchange in exchanges" :key="exchange.id" :to="`/requests/${exchange.id}`" class="grid gap-3 border-b border-zinc-200 py-4 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 sm:grid-cols-[120px_minmax(0,1fr)_100px] sm:px-3 dark:border-zinc-800 dark:hover:bg-zinc-900"><time class="font-mono text-xs text-zinc-500">{{ shortDate(exchange.ts) }}</time><div><div class="flex flex-wrap items-center gap-3"><IdentityBadge :label="exchange.provider || 'Unknown provider'" /><IdentityBadge :label="exchange.model" /><span v-if="exchange.finish_reason" class="text-[11px] text-zinc-500">{{ exchange.finish_reason }}</span></div><p class="mt-2 line-clamp-1 text-xs text-zinc-500">{{ exchange.request_excerpt || exchange.id }}</p></div><div class="flex items-center gap-1 font-mono text-xs text-zinc-500 sm:justify-end"><ArrowDown v-if="draft.order === 'desc'" class="size-3" /><ArrowUp v-else class="size-3" />{{ exchange.input_tokens.toLocaleString() }} in</div></RouterLink>
        <p v-if="!exchanges.length" class="border-b border-zinc-200 py-10 text-sm text-zinc-500 dark:border-zinc-800">No request evidence matches these filters.</p>
      </template>
    </div>
    <div v-if="nextCursor || error" class="mt-3 flex items-center justify-between gap-4"><p class="text-xs text-red-700 dark:text-red-400" role="alert">{{ error }}</p><button v-if="nextCursor" :disabled="loading || loadingMore" class="ml-auto h-8.5 rounded-[5px] border border-zinc-300 px-3 text-[13px] font-medium hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-wait disabled:opacity-60 dark:border-zinc-700 dark:hover:bg-zinc-900" @click="loadMore">{{ loadingMore ? "Loading..." : "Load more requests" }}</button></div>
  </section>
</template>
