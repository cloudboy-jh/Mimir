<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import EvidenceMessages from "@/components/conversation/EvidenceMessages.vue";
import Select from "@/components/ui/Select.vue";
import { errorMessage, getConversationExchange, listSessionExchanges, type LogEnvelope, type SessionDetail, type SessionExchange } from "@/lib/api";
import { conversationSections, projectConversation, type ConversationCapture } from "@/lib/conversation";
import { shortDate } from "@/lib/format";
import { displayTitle } from "@/lib/sessions";

const props = defineProps<{ sessionId: string; supportingSessions?: SessionDetail["supporting_sessions"]; refreshKey?: number }>();
const pageSize = 8;
const scope = ref("");
const exchanges = ref<SessionExchange[]>([]);
const envelopes = ref<Record<string, LogEnvelope>>(Object.create(null));
const failures = ref<Record<string, string>>(Object.create(null));
const pending = ref<Record<string, boolean>>(Object.create(null));
const loading = ref(false);
const loadingMore = ref(false);
const error = ref("");
const nextCursor = ref<string | null>(null);
const container = ref<HTMLElement>();
const pageIndex = ref(0);
let pageCursors: Array<string | undefined> = [undefined];
const priorCaptures = ref<ConversationCapture[]>([]);
let controller = new AbortController();
let observer: IntersectionObserver | undefined;
let generation = 0;
let inflight = 0;
let queue: string[] = [];
const scopeOptions = computed(() => [
  { value: "", label: "This session only" },
  { value: "tree", label: "Session and all sub-agents" },
  ...(props.supportingSessions ?? []).map((session) => ({ value: session.id, label: `Sub-agent: ${displayTitle(session)}` })),
]);
const entries = computed(() => projectConversation([...priorCaptures.value, ...exchanges.value.map((exchange) => ({ exchange, envelope: envelopes.value[exchange.id], error: failures.value[exchange.id], loading: pending.value[exchange.id] }))]).slice(priorCaptures.value.length));
const visibleEntries = computed(() => conversationSections(entries.value).filter((entry) => entry.exchange.request_kind === "primary" || entry.exchange.request_kind === "compaction"));
const sessionNames = computed(() => Object.fromEntries((props.supportingSessions ?? []).map((session) => [session.id, displayTitle(session)])));
function filters(cursor?: string) {
  // Index all kinds so compaction remains visible as a marker without loading its archive.
  return { order: "asc" as const, limit: pageSize, cursor, session: scope.value === "tree" ? undefined : scope.value || props.sessionId };
}
async function observeCaptures() {
  await nextTick();
  observer?.disconnect();
  if (!container.value) return;
  observer = new IntersectionObserver((observations) => {
    for (const observation of observations) if (observation.isIntersecting) {
      const id = (observation.target as HTMLElement).dataset.exchange;
      if (id) enqueue(id);
      observer?.unobserve(observation.target);
    }
  }, { rootMargin: "300px" });
  for (const node of container.value.querySelectorAll<HTMLElement>("[data-exchange]")) observer.observe(node);
}
function enqueue(id: string) {
  if (envelopes.value[id] || failures.value[id] || pending.value[id] || queue.includes(id)) return;
  const exchange = exchanges.value.find((exchange) => exchange.id === id);
  if (exchange?.capture_status !== "saved" || exchange.request_kind !== "primary") return;
  queue.push(id);
  pump();
}
function pump() {
  while (inflight < 2 && queue.length) {
    const id = queue.shift()!;
    const version = generation;
    pending.value[id] = true;
    inflight++;
    void getConversationExchange(id, controller.signal).then((result) => {
      if (version === generation) envelopes.value[id] = result.envelope;
    }).catch((cause) => {
      if (version === generation && !controller.signal.aborted) failures.value[id] = errorMessage(cause, "This part of the conversation could not be loaded.");
    }).finally(() => {
      if (version !== generation) return;
      delete pending.value[id];
      inflight--;
      pump();
    });
  }
}
function retryCapture(id: string) {
  delete failures.value[id];
  enqueue(id);
}
async function load() {
  if (loading.value || loadingMore.value) return;
  const version = generation;
  loading.value = true;
  error.value = "";
  try {
    const page = await listSessionExchanges(props.sessionId, filters(pageCursors[pageIndex.value]), controller.signal);
    if (version !== generation) return;
    exchanges.value = page.exchanges;
    nextCursor.value = page.next_cursor;
    const retained: Record<string, LogEnvelope> = Object.create(null);
    for (const exchange of page.exchanges) if (exchange.capture_status === "saved" && envelopes.value[exchange.id]) retained[exchange.id] = envelopes.value[exchange.id]!;
    envelopes.value = retained;
    await observeCaptures();
  } catch (cause) {
    if (version === generation && !controller.signal.aborted) error.value = errorMessage(cause, "Conversation requests could not be loaded.");
  } finally { if (version === generation) loading.value = false; }
}
async function changePage(direction: -1 | 1) {
  if (loading.value || loadingMore.value || direction === 1 && !nextCursor.value || direction === -1 && pageIndex.value === 0) return;
  const version = generation;
  const cursor = direction === 1 ? nextCursor.value! : pageCursors[pageIndex.value - 1];
  loadingMore.value = true;
  error.value = "";
  try {
    const page = await listSessionExchanges(props.sessionId, filters(cursor), controller.signal);
    if (version !== generation) return;
    const checkpoint = new Map<string, ConversationCapture>();
    if (direction === 1) for (const exchange of exchanges.value) if (exchange.request_kind === "primary") checkpoint.set(exchange.session_id, { exchange, envelope: envelopes.value[exchange.id] });
    // At most eight adjacent branch captures are retained, never the full archive history.
    priorCaptures.value = [...checkpoint.values()];
    controller.abort();
    controller = new AbortController();
    generation++;
    queue = [];
    inflight = 0;
    pending.value = Object.create(null);
    failures.value = Object.create(null);
    envelopes.value = Object.create(null);
    if (direction === 1) pageCursors[pageIndex.value + 1] = cursor;
    pageIndex.value += direction;
    exchanges.value = page.exchanges;
    nextCursor.value = page.next_cursor;
    await observeCaptures();
    container.value?.scrollIntoView({ block: "start" });
  } catch (cause) {
    if (version === generation && !controller.signal.aborted) error.value = errorMessage(cause, "The next conversation page could not be loaded. The current page is still shown.");
  } finally { loadingMore.value = false; }
}
function reset() {
  controller.abort();
  controller = new AbortController();
  generation++;
  observer?.disconnect();
  queue = [];
  inflight = 0;
  pending.value = Object.create(null);
  failures.value = Object.create(null);
  envelopes.value = Object.create(null);
  exchanges.value = [];
  nextCursor.value = null;
  pageIndex.value = 0;
  pageCursors = [undefined];
  priorCaptures.value = [];
  loading.value = false;
  loadingMore.value = false;
  void load();
}
watch(() => props.sessionId, () => { scope.value = ""; reset(); }, { immediate: true });
watch(scope, reset);
watch(() => props.refreshKey, () => { void load(); });
onBeforeUnmount(() => { generation++; controller.abort(); observer?.disconnect(); });
</script>

<template>
  <section ref="container" aria-label="Saved conversation" data-conversation-reader>
    <div class="flex items-center gap-3 border-b border-zinc-200 py-2 dark:border-zinc-800">
      <Select v-if="supportingSessions?.length" v-model="scope" :options="scopeOptions" label="Conversation scope" class="min-w-0 flex-1 sm:w-64 sm:flex-none" />
      <span v-else class="text-[13px] text-zinc-600 dark:text-zinc-400">This session</span>
      <button type="button" :disabled="loading || loadingMore" class="ml-auto h-8.5 shrink-0 rounded-[3px] px-2 text-[13px] font-medium text-teal-700 underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:opacity-50 dark:text-teal-400" @click="load">Refresh</button>
    </div>
    <div v-if="error" role="alert" class="my-4 text-sm text-red-700 dark:text-red-400">{{ error }}<button type="button" class="ml-3 underline focus-visible:outline-2 focus-visible:outline-teal-600" @click="load">Retry</button></div>
    <div v-if="loading && !exchanges.length" aria-busy="true" aria-label="Loading conversation" class="space-y-4 py-6"><div v-for="row in 3" :key="row" class="h-20 animate-pulse bg-zinc-100 motion-reduce:animate-none dark:bg-zinc-900" /></div>
    <p v-else-if="!visibleEntries.length && !error" class="py-8 text-sm text-zinc-600 dark:text-zinc-400">No conversation on this page.{{ nextCursor ? ' Continue to the next page.' : supportingSessions?.length ? ' Choose a sub-agent or inspect Requests.' : ' Inspect Requests for other recorded activity.' }}</p>
    <article v-for="entry in visibleEntries" :key="entry.exchange.id" :data-exchange="entry.exchange.id" class="border-b border-zinc-200 py-6 dark:border-zinc-800">
      <header class="mb-4 flex flex-wrap items-center justify-between gap-2 text-xs text-zinc-600 dark:text-zinc-400">
        <div class="flex flex-wrap gap-x-3 gap-y-1"><time :datetime="entry.exchange.ts" class="font-mono">{{ shortDate(entry.exchange.ts) }}</time><span v-if="scope === 'tree'">{{ entry.exchange.session_id === sessionId ? 'Main session' : sessionNames[entry.exchange.session_id] || entry.exchange.session_id }}</span><span>{{ entry.exchange.model }}</span></div>
        <details v-if="entry.captures.length > 1" class="relative">
          <summary class="cursor-pointer rounded-[3px] font-medium text-teal-700 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">Sources · {{ entry.captures.length }} requests</summary>
          <ul class="absolute right-0 z-10 mt-2 w-64 rounded-[5px] border border-zinc-200 bg-stone-50 p-3 dark:border-zinc-700 dark:bg-zinc-900"><li v-for="capture in entry.captures" :key="capture.id" class="py-1"><RouterLink :to="{ path: `/requests/${capture.id}`, query: { session: sessionId, view: 'conversation' }, hash: '#request-evidence-panel' }" class="block text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ shortDate(capture.ts) }} · {{ capture.model }}</RouterLink></li></ul>
        </details>
        <RouterLink v-else :to="{ path: `/requests/${entry.exchange.id}`, query: { session: sessionId, view: 'conversation' }, hash: '#request-evidence-panel' }" class="font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">Source</RouterLink>
      </header>
      <p v-if="entry.exchange.request_kind === 'compaction'" class="mt-3 text-xs text-zinc-600 dark:text-zinc-400">Context compaction · {{ entry.exchange.capture_status }}{{ entry.exchange.capture_reason || entry.exchange.failure_code ? `: ${entry.exchange.capture_reason || entry.exchange.failure_code}` : '' }}</p>
      <template v-else-if="entry.envelope">
        <EvidenceMessages :messages="entry.messages" :paired-results="entry.pairedResults" :origin-session="sessionId" />
      </template>
      <div v-if="!entry.envelope && entry.exchange.request_kind === 'primary'" class="mt-4 max-w-[70ch] text-base leading-7 text-zinc-600 dark:text-zinc-400" :aria-busy="entry.loading">
        <p v-if="entry.error" role="alert" class="text-red-700 dark:text-red-400">{{ entry.error }}<button class="ml-2 underline focus-visible:outline-2 focus-visible:outline-teal-600" @click="retryCapture(entry.exchange.id)">Retry conversation</button></p>
        <p v-else-if="entry.exchange.capture_status !== 'saved'" data-capture-unavailable>{{ entry.exchange.capture_status === 'accepted' ? 'Conversation pending.' : `Conversation unavailable (${entry.exchange.capture_status}).` }} {{ entry.exchange.capture_reason || entry.exchange.failure_code }}</p>
        <p v-else-if="entry.loading">Loading conversation…</p>
        <button v-else class="text-teal-700 underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="enqueue(entry.exchange.id)">Load conversation</button>
        <p v-if="entry.exchange.request_excerpt" class="mt-3 whitespace-pre-wrap break-words text-sm"><span class="font-medium">Partial input:</span> {{ entry.exchange.request_excerpt }}</p>
      </div>
    </article>
    <div class="flex flex-wrap items-center justify-between gap-3 pt-5 text-[13px] text-zinc-600 dark:text-zinc-400"><p>Page {{ pageIndex + 1 }} · {{ exchanges.length }} requests<span v-if="loading"> · Refreshing…</span></p><div class="flex gap-3"><button v-if="pageIndex > 0" :disabled="loadingMore || loading" class="rounded-[5px] border border-zinc-300 px-3 py-2 text-zinc-700 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-teal-600 disabled:opacity-50 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-900" @click="changePage(-1)">Previous</button><button v-if="nextCursor" :disabled="loadingMore || loading" class="rounded-[5px] border border-zinc-300 px-3 py-2 font-medium text-zinc-700 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-teal-600 disabled:opacity-50 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-900" @click="changePage(1)">{{ loadingMore ? 'Loading…' : 'Next page' }}</button></div></div>
  </section>
</template>
