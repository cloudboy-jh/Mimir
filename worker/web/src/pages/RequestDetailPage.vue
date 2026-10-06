<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, RotateCw } from "lucide-vue-next";
import BrandIcon from "@/components/BrandIcon.vue";
import RequestEvidence from "@/components/request/RequestEvidence.vue";
import { errorMessage, getExchange, type Exchange, type LogEnvelope } from "@/lib/api";
import { outputSpeed, shortDate } from "@/lib/format";

const route = useRoute();
const exchange = ref<Exchange | null>(null);
const envelope = ref<LogEnvelope | null>(null);
const loading = ref(true);
const error = ref("");
const originSession = computed(() => typeof route.query.session === "string" ? route.query.session : "");
const debugOpen = ref(false);
const debugJson = computed(() => debugOpen.value && envelope.value ? JSON.stringify(envelope.value, null, 2) : "");
const backTarget = computed(() => originSession.value
  ? { path: `/sessions/${originSession.value}`, query: { view: route.query.view === "conversation" ? "conversation" : "requests" }, hash: route.query.view === "conversation" ? "#session-panel-conversation" : "#session-activity" }
  : "/requests");
const backLabel = computed(() => originSession.value ? route.query.view === "conversation" ? "Session conversation" : "Session requests" : "Requests");
let controller: AbortController | null = null;

async function load() {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loading.value = true;
  error.value = "";
  exchange.value = null;
  envelope.value = null;
  debugOpen.value = false;
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

async function focusSide() {
  await nextTick();
  const side = route.query.side === "response" ? "response" : "request";
  if ((!route.hash || route.hash === "#request-evidence-panel") && (route.query.side || route.hash)) document.getElementById(`request-${side}`)?.focus();
}

watch(() => String(route.params.id), load, { immediate: true });
watch([() => route.query.side, envelope], () => { void focusSide(); });
onMounted(() => { void focusSide(); });
</script>

<template>
  <section v-if="exchange && envelope">
    <RouterLink :to="backTarget" class="mb-4 inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-600 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" aria-hidden="true" />{{ backLabel }}</RouterLink>
    <header class="flex flex-wrap items-start justify-between gap-3 border-b border-zinc-200 pb-4 dark:border-zinc-800">
      <div class="min-w-0">
        <h1 class="flex items-center gap-2 break-all text-lg font-semibold"><BrandIcon :label="exchange.model" />{{ exchange.model }}</h1>
        <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">{{ exchange.provider || 'Unknown provider' }} · {{ exchange.harness || 'Unknown app' }} · {{ shortDate(exchange.ts) }}</p>
        <dl class="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs">
          <div class="flex gap-1.5"><dt class="text-zinc-600 dark:text-zinc-400">Latency</dt><dd class="font-mono">{{ (exchange.latency_ms / 1000).toFixed(2) }}s</dd></div>
          <div class="flex gap-1.5"><dt class="text-zinc-600 dark:text-zinc-400">Tokens</dt><dd class="font-mono">{{ exchange.input_tokens.toLocaleString() }} in / {{ exchange.output_tokens.toLocaleString() }} out</dd></div>
          <div class="flex gap-1.5"><dt class="text-zinc-600 dark:text-zinc-400">Speed</dt><dd class="font-mono">{{ outputSpeed(exchange.output_tokens, exchange.latency_ms) }}</dd></div>
          <div class="flex gap-1.5"><dt class="text-zinc-600 dark:text-zinc-400">Finish</dt><dd>{{ exchange.finish_reason || 'Unknown' }}</dd></div>
        </dl>
      </div>
      <RouterLink v-if="!originSession" :to="`/sessions/${exchange.session_id}`" class="text-xs font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">Session</RouterLink>
    </header>
    <div id="request-evidence-panel" class="grid min-w-0 gap-6 pt-5 lg:grid-cols-2 lg:gap-8">
      <section id="request-request" aria-labelledby="request-input-heading" tabindex="-1" class="min-w-0 scroll-mt-20 rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-teal-600">
        <h2 id="request-input-heading" class="border-b border-zinc-200 pb-2 text-sm font-semibold dark:border-zinc-800">Input</h2>
        <RequestEvidence :envelope="envelope" side="request" :origin-session="originSession" />
      </section>
      <section id="request-response" aria-labelledby="request-output-heading" tabindex="-1" class="min-w-0 scroll-mt-20 rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-teal-600">
        <h2 id="request-output-heading" class="border-b border-zinc-200 pb-2 text-sm font-semibold dark:border-zinc-800">Output</h2>
        <RequestEvidence :envelope="envelope" side="response" :origin-session="originSession" />
      </section>
    </div>
    <details class="mt-6 border-t border-zinc-200 pt-3 dark:border-zinc-800" @toggle="debugOpen = ($event.target as HTMLDetailsElement).open">
      <summary class="w-fit cursor-pointer rounded-[3px] text-xs text-zinc-600 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400">Debug JSON</summary>
      <p class="mt-3 break-all font-mono text-xs text-zinc-600 dark:text-zinc-400">{{ exchange.id }}</p>
      <pre v-if="debugOpen" class="mt-2 max-h-[65vh] overflow-auto whitespace-pre-wrap break-words rounded-[5px] border border-zinc-200 bg-stone-50 p-4 font-mono text-xs leading-5 dark:border-zinc-800 dark:bg-zinc-900" tabindex="0">{{ debugJson }}</pre>
    </details>
  </section>
  <section v-else-if="loading" aria-busy="true" class="py-16"><div class="h-4 w-32 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-5 h-9 w-64 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-8 h-80 animate-pulse bg-zinc-100 motion-reduce:animate-none dark:bg-zinc-900" /></section>
  <section v-else class="py-20 text-center"><h1 class="text-xl font-semibold">Request unavailable</h1><p class="mx-auto mt-2 max-w-md text-sm text-zinc-500 dark:text-zinc-400">{{ error }}</p><div class="mt-4 flex justify-center gap-4"><button class="inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button><RouterLink to="/requests" class="text-sm font-medium text-teal-700 dark:text-teal-400">Return to requests</RouterLink></div></section>
</template>
