<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, RotateCw } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import BrandIcon from "@/components/BrandIcon.vue";
import RequestEvidence from "@/components/request/RequestEvidence.vue";
import { errorMessage, getExchange, type Exchange, type LogEnvelope } from "@/lib/api";
import { outputSpeed, shortDate } from "@/lib/format";

const route = useRoute();
const exchange = ref<Exchange | null>(null);
const envelope = ref<LogEnvelope | null>(null);
const tab = ref<"request" | "response">("request");
const loading = ref(true);
const error = ref("");
const originSession = computed(() => typeof route.query.session === "string" ? route.query.session : "");
const backTarget = computed(() => originSession.value ? `/sessions/${originSession.value}#session-activity` : "/requests");
const backLabel = computed(() => originSession.value ? "Session activity" : "Requests");
let controller: AbortController | null = null;

async function load() {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loading.value = true;
  error.value = "";
  exchange.value = null;
  envelope.value = null;
  try {
    const result = await getExchange(String(route.params.id), active.signal);
    exchange.value = result.exchange;
    envelope.value = result.envelope;
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "This request could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

function moveTab() {
  tab.value = tab.value === "request" ? "response" : "request";
  document.getElementById(`request-tab-${tab.value}`)?.focus();
}

watch(() => String(route.params.id), load, { immediate: true });
</script>

<template>
  <section v-if="exchange && envelope">
    <RouterLink :to="backTarget" class="mb-6 inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-500 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" aria-hidden="true" />{{ backLabel }}</RouterLink>
    <div class="border-b border-zinc-200 pb-6 dark:border-zinc-800"><div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between"><div class="min-w-0"><p class="break-all font-mono text-xs text-zinc-500">{{ exchange.id }}</p><h1 class="mt-2 flex min-w-0 items-center gap-3 break-all text-2xl font-semibold tracking-[-0.025em] sm:text-[28px]"><BrandIcon :label="exchange.model" />{{ exchange.model }}</h1><div class="mt-3 flex flex-wrap items-center gap-4"><IdentityBadge :label="exchange.provider || 'Unknown provider'" /><IdentityBadge :label="exchange.harness || 'Unknown app'" /><span class="text-xs text-zinc-500">{{ shortDate(exchange.ts) }}</span></div></div><RouterLink :to="`/sessions/${exchange.session_id}`" class="text-[13px] font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">View parent session</RouterLink></div></div>
    <dl class="grid grid-cols-2 border-b border-zinc-200 sm:grid-cols-3 md:grid-cols-5 dark:border-zinc-800"><div v-for="item in [{ label: 'Input', value: exchange.input_tokens.toLocaleString() }, { label: 'Output', value: exchange.output_tokens.toLocaleString() }, { label: 'Latency', value: `${(exchange.latency_ms / 1000).toFixed(2)}s` }, { label: 'Speed', value: outputSpeed(exchange.output_tokens, exchange.latency_ms) }, { label: 'Finish', value: exchange.finish_reason || 'Unknown' }]" :key="item.label" class="border-b border-zinc-200 py-4 pr-4 last:border-b-0 sm:border-b-0 dark:border-zinc-800"><dt class="text-xs text-zinc-500">{{ item.label }}</dt><dd class="mt-1 break-all font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ item.value }}</dd></div></dl>
    <div class="pt-8"><div class="mb-4 flex gap-5 border-b border-zinc-200 dark:border-zinc-800" role="tablist" aria-label="Request evidence side" @keydown.left.prevent="moveTab" @keydown.right.prevent="moveTab"><button v-for="name in ['request', 'response'] as const" :id="`request-tab-${name}`" :key="name" type="button" role="tab" :aria-selected="tab === name" aria-controls="request-evidence-panel" :tabindex="tab === name ? 0 : -1" class="relative pb-2.5 text-[13px] font-medium capitalize focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600" :class="tab === name ? 'text-zinc-950 after:absolute after:inset-x-0 after:-bottom-px after:h-px after:bg-teal-700 dark:text-zinc-50' : 'text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200'" @click="tab = name">{{ name }}</button></div><div id="request-evidence-panel" role="tabpanel" :aria-labelledby="`request-tab-${tab}`"><RequestEvidence :envelope="envelope" :side="tab" /></div></div>
  </section>
  <section v-else-if="loading" aria-busy="true" class="py-16"><div class="h-4 w-32 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-5 h-9 w-64 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-8 h-80 animate-pulse bg-zinc-100 motion-reduce:animate-none dark:bg-zinc-900" /></section>
  <section v-else class="py-20 text-center"><h1 class="text-xl font-semibold">Request unavailable</h1><p class="mx-auto mt-2 max-w-md text-sm text-zinc-500 dark:text-zinc-400">{{ error }}</p><div class="mt-4 flex justify-center gap-4"><button class="inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button><RouterLink to="/requests" class="text-sm font-medium text-teal-700 dark:text-teal-400">Return to requests</RouterLink></div></section>
</template>
