<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { ArrowRight, RotateCw, Search } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import { errorMessage, listExchanges, type Exchange } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { compactNumber, outputSpeed, shortDate } from "@/lib/format";

const exchanges = ref<Exchange[]>([]);
const query = ref("");
const provider = ref("");
const app = ref("");
const nextCursor = ref<string | null>(null);
const loading = ref(true);
const loadingMore = ref(false);
const error = ref("");
const providers = ref<string[]>([]);
const apps = ref<string[]>([]);
let controller: AbortController | null = null;

const filtered = computed(() => {
  const needle = query.value.trim().toLowerCase();
  if (!needle) return exchanges.value;
  return exchanges.value.filter((exchange) => [exchange.model, exchange.repo, exchange.provider, exchange.harness, exchange.session_id].filter(Boolean).join(" ").toLowerCase().includes(needle));
});

function captureFacets(rows: Exchange[]) {
  providers.value = [...new Set([...providers.value, ...rows.flatMap((row) => row.provider ? [row.provider] : [])])].sort();
  apps.value = [...new Set([...apps.value, ...rows.flatMap((row) => row.harness ? [row.harness] : [])])].sort();
}

async function load(reset = true) {
  if (reset) {
    controller?.abort();
    controller = new AbortController();
    loading.value = true;
    error.value = "";
  } else {
    loadingMore.value = true;
  }
  const active = controller ?? new AbortController();
  controller = active;
  try {
    const result = await listExchanges({ cursor: reset ? undefined : nextCursor.value ?? undefined, provider: provider.value, app: app.value }, active.signal);
    exchanges.value = reset ? result.exchanges : [...exchanges.value, ...result.exchanges];
    captureFacets(result.exchanges);
    nextCursor.value = result.next_cursor;
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "Requests could not be loaded.");
  } finally {
    if (!active.signal.aborted) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
}

async function refreshLatest() {
  if (loading.value || loadingMore.value) return;
  const active = controller ?? new AbortController();
  controller = active;
  try {
    const result = await listExchanges({ provider: provider.value, app: app.value }, active.signal);
    const latestIds = new Set(result.exchanges.map((exchange) => exchange.id));
    const hadMultiplePages = exchanges.value.length > 50;
    exchanges.value = [...result.exchanges, ...exchanges.value.filter((exchange) => !latestIds.has(exchange.id))];
    captureFacets(result.exchanges);
    if (!hadMultiplePages) nextCursor.value = result.next_cursor;
    error.value = "";
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "Requests could not be refreshed.");
  }
}

watch([provider, app], () => load(true));
onMounted(() => load(true));
useAutoRefresh(refreshLatest);
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section>
    <div class="mb-7 flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between"><div><h1 class="text-[28px] font-semibold tracking-[-0.025em] text-zinc-950 dark:text-zinc-50">Requests</h1><p class="mt-1.5 max-w-2xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">Model traffic captured as evidence within Mimir sessions.</p></div><div v-if="!loading" class="font-mono text-xs text-zinc-500">{{ filtered.length }} loaded requests</div></div>
    <div class="mb-4 flex flex-col gap-2 sm:flex-row"><label class="relative block min-w-0 flex-1 sm:max-w-sm"><span class="sr-only">Search loaded requests</span><Search class="pointer-events-none absolute left-2.5 top-2.25 size-4 text-zinc-400" /><input v-model="query" type="search" placeholder="Search loaded requests..." class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white pl-8.5 pr-3 text-[13px] placeholder:text-zinc-500 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label><select v-model="provider" aria-label="Provider" class="h-8.5 rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] dark:border-zinc-700 dark:bg-zinc-900"><option value="">All providers</option><option v-for="name in providers" :key="name" :value="name">{{ name }}</option></select><select v-model="app" aria-label="App" class="h-8.5 rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] dark:border-zinc-700 dark:bg-zinc-900"><option value="">All apps</option><option v-for="name in apps" :key="name" :value="name">{{ name }}</option></select></div>
    <div class="overflow-hidden rounded-[7px] border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
      <div class="overflow-x-auto"><table class="w-full min-w-[1050px] border-collapse text-left"><thead><tr class="border-b border-zinc-200 bg-zinc-50 text-xs font-medium text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900"><th class="px-4 py-2.5 font-medium">Date</th><th class="px-4 py-2.5 font-medium">Model</th><th class="px-4 py-2.5 font-medium">Provider</th><th class="px-4 py-2.5 font-medium">App</th><th class="px-4 py-2.5 font-medium">Repo</th><th class="px-4 py-2.5 text-right font-medium">Input</th><th class="px-4 py-2.5 text-right font-medium">Output</th><th class="px-4 py-2.5 text-right font-medium">Speed</th><th class="px-4 py-2.5 font-medium">Finish</th><th class="w-10" /></tr></thead>
        <tbody v-if="!loading"><tr v-for="exchange in filtered" :key="exchange.id" class="group border-b border-zinc-200 text-[13px] last:border-b-0 hover:bg-stone-50 dark:border-zinc-800 dark:hover:bg-zinc-800/70"><td class="px-4 py-3.5 font-mono text-xs text-zinc-500">{{ shortDate(exchange.ts) }}</td><td class="px-4 py-3.5 font-medium text-zinc-900 dark:text-zinc-100"><IdentityBadge :label="exchange.model" /></td><td class="px-4 py-3.5"><IdentityBadge :label="exchange.provider || 'Unknown'" /></td><td class="px-4 py-3.5"><IdentityBadge :label="exchange.harness || 'Unknown'" /></td><td class="px-4 py-3.5 text-zinc-600 dark:text-zinc-400">{{ exchange.repo || "None" }}</td><td class="px-4 py-3.5 text-right font-mono text-xs">{{ compactNumber(exchange.input_tokens) }}</td><td class="px-4 py-3.5 text-right font-mono text-xs">{{ compactNumber(exchange.output_tokens) }}</td><td class="px-4 py-3.5 text-right font-mono text-xs text-zinc-500">{{ outputSpeed(exchange.output_tokens, exchange.latency_ms) }}</td><td class="px-4 py-3.5 font-mono text-xs text-zinc-500">{{ exchange.finish_reason || "Unknown" }}</td><td class="pr-3"><RouterLink :to="`/requests/${exchange.id}`" :aria-label="`Open ${exchange.model} request`" class="grid size-7 place-items-center rounded-[4px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600"><ArrowRight class="size-4 text-zinc-400 transition-transform group-hover:translate-x-0.5" /></RouterLink></td></tr></tbody>
      </table></div>
      <div v-if="loading" aria-busy="true" class="divide-y divide-zinc-200 dark:divide-zinc-800"><div v-for="index in 6" :key="index" class="grid grid-cols-4 gap-8 px-4 py-5"><span class="h-3 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><span class="h-3 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><span class="h-3 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
      <div v-else-if="error && !exchanges.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Requests unavailable</p><p class="mt-1 text-sm text-zinc-500">{{ error }}</p><button class="mt-4 inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load(true)"><RotateCw class="size-4" />Retry</button></div>
      <div v-else-if="!filtered.length" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">{{ exchanges.length ? "No matching requests" : "No requests captured yet" }}</p><p class="mt-1 text-sm text-zinc-500">{{ exchanges.length ? "Clear a filter or try a broader search." : "Saved model exchanges will appear here." }}</p></div>
    </div>
    <div v-if="nextCursor || error" class="mt-4 flex items-center gap-4"><button v-if="nextCursor" :disabled="loadingMore" class="h-8.5 rounded-[5px] border border-zinc-300 px-3 text-[13px] font-medium hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-wait disabled:opacity-60 dark:border-zinc-700 dark:hover:bg-zinc-800" @click="load(false)">{{ loadingMore ? "Loading..." : "Load more" }}</button><span v-if="error && exchanges.length" class="text-xs text-red-700 dark:text-red-400">{{ error }}</span></div>
  </section>
</template>
