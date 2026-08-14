<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, Download, RotateCw } from "lucide-vue-next";
import RequestTimeline from "@/components/session/RequestTimeline.vue";
import SessionCapture from "@/components/session/SessionCapture.vue";
import SessionEvidenceSidebar from "@/components/session/SessionEvidenceSidebar.vue";
import SessionHeader from "@/components/session/SessionHeader.vue";
import SessionOutcome from "@/components/session/SessionOutcome.vue";
import SessionChanges from "@/components/session/SessionChanges.vue";
import SessionSummary from "@/components/session/SessionSummary.vue";
import LiveSessionTurns from "@/components/session/LiveSessionTurns.vue";
import { ApiError, connectSessionLive, currentOutcomeEvidence, errorMessage, getSession, getSessionObjectState, listSessionExchanges, type LiveSessionTurn, type SessionDetail, type SessionExchange, type SessionLiveness, type SessionLiveMessage, type SessionTitleUpdate } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { markdownExportLimit, sessionMarkdown } from "@/lib/markdown";

const route = useRoute();
const detail = ref<SessionDetail | null>(null);
const loading = ref(true);
const error = ref("");
const liveness = ref<SessionLiveness>("finalized");
const liveTurns = ref<LiveSessionTurn[]>([]);
let controller: AbortController | null = null;
let loadVersion = 0;
let liveSocket: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
const hasSessionObject = ref(false);

function wait(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener("abort", () => { clearTimeout(timer); reject(new DOMException("Aborted", "AbortError")); }, { once: true });
  });
}

async function load() {
  const version = ++loadVersion;
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  detail.value = null;
  stopLive();
  try {
    for (const delay of [0, 500, 1_000, 1_500, 2_000]) {
      if (delay) await wait(delay, controller.signal);
      const result = await getSession(String(route.params.id), controller.signal);
      if (version !== loadVersion) return;
      detail.value = result;
      loading.value = false;
      if (result.capture.pending_exchanges === 0) break;
    }
    if (version === loadVersion && detail.value) await startLive(detail.value.session.id);
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorMessage(cause, "This session could not be loaded.");
  } finally {
    if (version === loadVersion && !controller.signal.aborted) loading.value = false;
  }
}

async function refreshDetail() {
  if (!detail.value) return;
  const id = detail.value.session.id;
  const version = loadVersion;
  try {
    const result = await getSession(id);
    if (version === loadVersion && String(route.params.id) === id) detail.value = result;
  } catch (cause) {
    error.value = errorMessage(cause, "The updated session could not be loaded.");
  }
}

function appendTurn(turn: LiveSessionTurn) {
  if (turn.exchange_id && liveTurns.value.some((existing) => existing.exchange_id === turn.exchange_id)) return;
  liveTurns.value.push(turn);
}

function applyLiveMessage(message: SessionLiveMessage) {
  if (message.type === "snapshot") {
    liveness.value = message.state.liveness;
    liveTurns.value = message.turns;
    if (message.state.liveness === "finalized") closeLiveConnection("session finalized");
    return;
  }
  if (message.type === "event") {
    liveness.value = "active";
    if (message.event.kind === "turn" && message.event.turn) appendTurn({ ...message.event.turn, ts: message.event.ts });
    return;
  }
  if (message.type === "reopened") {
    liveness.value = "active";
    if (detail.value) {
      detail.value.session.state = "active";
      detail.value.session.ended_at = null;
    }
    return;
  }
  liveness.value = "finalized";
  if (detail.value) {
    detail.value.session.state = "inactive";
    detail.value.session.ended_at = message.ended_at;
  }
  closeLiveConnection("session finalized");
  void refreshDetail();
}

function connectLive(id: string) {
  if (liveness.value === "finalized") return;
  closeLiveConnection("reconnecting");
  const socket = connectSessionLive(id, applyLiveMessage);
  liveSocket = socket;
  socket?.addEventListener("close", () => {
    if (liveSocket !== socket) return;
    liveSocket = null;
    if (hasSessionObject.value && liveness.value !== "finalized" && detail.value?.session.id === id) {
      reconnectTimer = setTimeout(() => connectLive(id), 2_000);
    }
  }, { once: true });
}

async function startLive(id: string) {
  liveTurns.value = [];
  try {
    const state = await getSessionObjectState(id);
    if (detail.value?.session.id !== id) return;
    hasSessionObject.value = true;
    liveness.value = state.liveness;
    if (state.liveness !== "finalized") connectLive(id);
  } catch (cause) {
    if (!(cause instanceof ApiError && cause.status === 404)) error.value = errorMessage(cause, "Live session state could not be loaded.");
    hasSessionObject.value = false;
    liveness.value = detail.value?.session.state === "inactive" ? "finalized" : "disconnected";
  }
}

function closeLiveConnection(reason: string) {
  clearTimeout(reconnectTimer);
  reconnectTimer = undefined;
  const socket = liveSocket;
  liveSocket = null;
  socket?.close(1000, reason);
}

function stopLive() {
  hasSessionObject.value = false;
  closeLiveConnection("view changed");
}

async function refreshLiveness() {
  const id = detail.value?.session.id;
  if (!id || !hasSessionObject.value) return;
  try {
    const state = await getSessionObjectState(id);
    liveness.value = state.liveness;
    if (state.liveness === "finalized") closeLiveConnection("session finalized");
  } catch {
    liveness.value = "disconnected";
  }
}

function applyTitleUpdate(update: SessionTitleUpdate) {
  if (detail.value?.session.id === update.id) Object.assign(detail.value.session, update);
}

const commitEvidence = computed(() => {
  if (!detail.value) return null;
  const evidence = currentOutcomeEvidence(detail.value.outcome_events, detail.value.session.outcome);
  return evidence?.commit ? evidence : null;
});

const exporting = ref(false);
const exportError = ref("");

async function exportMarkdown() {
  if (!detail.value || exporting.value) return;
  exporting.value = true;
  exportError.value = "";
  try {
    const id = detail.value.session.id;
    const exchanges: SessionExchange[] = [];
    let cursor: string | null = null;
    do {
      const page: { exchanges: SessionExchange[]; next_cursor: string | null } = await listSessionExchanges(id, { order: "asc", limit: 100, cursor: cursor ?? undefined });
      exchanges.push(...page.exchanges);
      cursor = page.next_cursor;
    } while (cursor && exchanges.length < markdownExportLimit());
    const markdown = sessionMarkdown(detail.value, exchanges.slice(0, markdownExportLimit()));
    const url = URL.createObjectURL(new Blob([markdown], { type: "text/markdown" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `mimir-session-${id}.md`;
    anchor.click();
    URL.revokeObjectURL(url);
  } catch (cause) {
    exportError.value = errorMessage(cause, "The export could not be created.");
  } finally {
    exporting.value = false;
  }
}

watch(() => String(route.params.id), load, { immediate: true });
useAutoRefresh(refreshLiveness);
onBeforeUnmount(() => { controller?.abort(); stopLive(); });
</script>

<template>
  <section v-if="detail">
    <div class="mb-6 flex items-center justify-between gap-4">
      <RouterLink to="/sessions" class="inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-500 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" />Sessions</RouterLink>
      <div class="flex items-center gap-3">
        <span v-if="exportError" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ exportError }}</span>
        <button type="button" :disabled="exporting" class="inline-flex h-8.5 items-center gap-2 rounded-[5px] border border-zinc-300 px-3 text-[13px] font-medium text-zinc-700 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-wait disabled:opacity-60 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-900" @click="exportMarkdown"><Download class="size-3.5" />{{ exporting ? "Exporting…" : "Download as Markdown" }}</button>
      </div>
    </div>
    <SessionHeader :session="detail.session" :liveness="liveness" @saved="applyTitleUpdate" />
    <SessionSummary :session="detail.session" />
    <div class="grid gap-8 border-b border-zinc-200 py-6 lg:grid-cols-[minmax(0,1.5fr)_minmax(260px,.5fr)] dark:border-zinc-800">
      <SessionOutcome :detail="detail" @saved="refreshDetail" />
      <SessionCapture :capture="detail.capture" />
    </div>
    <div class="grid gap-x-8 gap-y-7 pt-8 xl:grid-cols-[minmax(0,1fr)_360px] xl:grid-rows-[auto_1fr] xl:items-start">
      <SessionChanges class="xl:col-start-2 xl:row-start-1" :session-id="detail.session.id" :evidence="currentOutcomeEvidence(detail.outcome_events, detail.session.outcome)" :source-ref="detail.session.source_ref" />
      <div class="grid gap-8 xl:col-start-1 xl:row-span-2 xl:row-start-1">
        <LiveSessionTurns v-if="hasSessionObject" :turns="liveTurns" :liveness="liveness" />
        <RequestTimeline :session-id="detail.session.id" />
      </div>
      <SessionEvidenceSidebar class="xl:col-start-2 xl:row-start-2" :supporting-sessions="detail.supporting_sessions" :files="detail.files" :errors="detail.errors" :evidence="commitEvidence" />
    </div>
  </section>
  <section v-else-if="loading" aria-busy="true" class="mx-auto max-w-3xl py-16"><div class="h-4 w-28 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-5 h-8 w-72 max-w-full animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-8 h-28 animate-pulse border-y border-zinc-200 bg-zinc-100 motion-reduce:animate-none dark:border-zinc-800 dark:bg-zinc-900" /></section>
  <section v-else class="py-20 text-center"><h1 class="text-xl font-semibold">Session unavailable</h1><p class="mx-auto mt-2 max-w-md text-sm text-zinc-500 dark:text-zinc-400">{{ error }}</p><div class="mt-4 flex justify-center gap-4"><button class="inline-flex items-center gap-2 text-sm font-medium text-teal-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button><RouterLink to="/sessions" class="text-sm font-medium text-teal-700 dark:text-teal-400">Return to sessions</RouterLink></div></section>
</template>
