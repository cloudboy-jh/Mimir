<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { AlertTriangle, ArrowLeft, BookOpen, ChevronDown, Download, FileText, GitCommitHorizontal, PanelRight, RotateCw } from "lucide-vue-next";
import { DropdownMenuContent, DropdownMenuItem, DropdownMenuPortal, DropdownMenuRoot, DropdownMenuSeparator, DropdownMenuTrigger } from "reka-ui";
import RequestTimeline from "@/components/session/RequestTimeline.vue";
import SessionConversation from "@/components/session/SessionConversation.vue";
import SessionDetailsSidebar from "@/components/session/SessionDetailsSidebar.vue";
import SessionSummaryPanel from "@/components/session/SessionSummaryPanel.vue";
import SessionHeader from "@/components/session/SessionHeader.vue";
import SessionSummary from "@/components/session/SessionSummary.vue";
import { ApiError, connectSessionLive, errorMessage, getSession, getSessionObjectState, listSessionExchanges, type LiveSessionTurn, type SessionDetail, type SessionExchange, type SessionLiveness, type SessionLiveMessage, type SessionTitleUpdate } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { markdownExportLimit, sessionMarkdown } from "@/lib/markdown";
import { loadSessionNoteSettings, OBSIDIAN_URI_EXCHANGE_LIMIT, prepareObsidianVault, writeAndOpenSessionNote } from "@/lib/session-notes";

const route = useRoute();
const router = useRouter();
const detail = ref<SessionDetail | null>(null);
const loading = ref(true);
const error = ref("");
const liveError = ref("");
const liveness = ref<SessionLiveness>("finalized");
const liveTurns = ref<LiveSessionTurn[]>([]);
let controller: AbortController | null = null;
let loadVersion = 0;
let liveSocket: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
const hasSessionObject = ref(false);
const timelineRevision = ref(0);
const evidenceTab = ref<"conversation" | "requests">("conversation");
const summaryOpen = ref(false);
const sidebarSection = ref<"details" | "changes">("details");
const panelReturnFocus = ref<HTMLElement>();
function openPanel(section: "summary" | "changes" | "details", event?: Event) {
  if (section === "summary") {
    const target = event?.currentTarget ?? document.activeElement;
    if (!summaryOpen.value || event) panelReturnFocus.value = target instanceof HTMLElement ? target : undefined;
    summaryOpen.value = true;
  } else {
    sidebarSection.value = section;
    panelReturnFocus.value = document.getElementById(`session-sidebar-tab-${section}`) ?? undefined;
    summaryOpen.value = false;
    void nextTick(() => {
      panelReturnFocus.value?.focus();
      document.getElementById("session-sidebar")?.scrollIntoView({ block: "nearest" });
    });
  }
}
async function revealEvidenceHash() {
  if (!detail.value || !route.hash) return;
  const targets: Record<string, "summary" | "changes" | "details"> = {
    "#session-summary-heading": "summary",
    "#changes-heading": "changes",
    "#outcome-evidence-heading": "changes",
    "#outcome-heading": "details",
    "#session-details-heading": "details",
    "#session-usage-heading": "details",
    "#session-capture-heading": "details",
  };
  const section = targets[route.hash];
  if (section) openPanel(section);
  else summaryOpen.value = false;
  await nextTick();
  document.getElementById(route.hash.slice(1))?.scrollIntoView({ block: "start" });
}
async function navigateEvidence(hash: string) {
  if (hash === "#session-activity") {
    summaryOpen.value = false;
    await router.replace({ query: { ...route.query, view: "requests" }, hash });
  } else await router.replace({ query: { ...route.query }, hash });
  await revealEvidenceHash();
}
function selectEvidenceTab(tab: "conversation" | "requests") {
  evidenceTab.value = tab;
  return router.replace({ query: { ...route.query, view: tab }, hash: `#session-panel-${tab}` });
}
function moveEvidenceTab(event: KeyboardEvent) {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  void selectEvidenceTab(event.key === "Home" ? "conversation" : event.key === "End" ? "requests" : evidenceTab.value === "conversation" ? "requests" : "conversation");
  void nextTick(() => document.getElementById(`session-tab-${evidenceTab.value}`)?.focus());
}

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
  liveError.value = "";
  detail.value = null;
  summaryOpen.value = false;
  sidebarSection.value = "details";
  evidenceTab.value = route.query.view === "conversation" ? "conversation" : route.query.view === "requests" || route.hash === "#session-activity" ? "requests" : "conversation";
  stopLive();
  try {
    for (const delay of [0, 500, 1_000, 1_500, 2_000]) {
      if (delay) await wait(delay, controller.signal);
      const result = await getSession(String(route.params.id), controller.signal);
      if (version !== loadVersion) return;
      detail.value = result;
      liveness.value = result.session.state === "inactive" ? "finalized" : "disconnected";
      if (delay && result.capture.pending_exchanges === 0) timelineRevision.value += 1;
      loading.value = false;
      if (result.capture.pending_exchanges === 0) break;
    }
    if (version === loadVersion && detail.value) {
      await revealEvidenceHash();
      await startLive(detail.value.session.id);
    }
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorMessage(cause, "This session could not be loaded.");
  } finally {
    if (version === loadVersion && !controller.signal.aborted) loading.value = false;
  }
}

async function refreshDetail(refreshTimeline = false) {
  if (!detail.value) return;
  const id = detail.value.session.id;
  const version = loadVersion;
  try {
    const result = await getSession(id);
    if (version === loadVersion && String(route.params.id) === id) {
      detail.value = result;
      error.value = "";
      if (refreshTimeline) timelineRevision.value += 1;
    }
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
  void refreshDetail(true);
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
      liveness.value = "disconnected";
      reconnectTimer = setTimeout(() => connectLive(id), 2_000);
    }
  }, { once: true });
}

async function startLive(id: string) {
  liveTurns.value = [];
  liveError.value = "";
  try {
    const state = await getSessionObjectState(id);
    if (detail.value?.session.id !== id) return;
    hasSessionObject.value = true;
    liveness.value = state.liveness;
    if (state.liveness !== "finalized") connectLive(id);
  } catch (cause) {
    if (!(cause instanceof ApiError && cause.status === 404)) liveError.value = errorMessage(cause, "Live session state could not be loaded.");
    hasSessionObject.value = false;
    liveness.value = detail.value?.session.state === "inactive" ? "finalized" : "disconnected";
  }
}

function retryBanner() {
  if (error.value) void refreshDetail();
  else if (liveError.value && detail.value) void startLive(detail.value.session.id);
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

const exporting = ref(false);
const exportError = ref("");
const noteMessage = ref("");
const obsidianConfigured = ref(false);

async function refreshObsidianState() {
  try {
    obsidianConfigured.value = Boolean(await loadSessionNoteSettings());
  } catch {
    obsidianConfigured.value = false;
  }
}

async function renderSessionMarkdown(limit = markdownExportLimit()): Promise<string> {
  if (!detail.value) throw new Error("The session is not available.");
  const id = detail.value.session.id;
  const exchanges: SessionExchange[] = [];
  let cursor: string | null = null;
  do {
    const page: { exchanges: SessionExchange[]; next_cursor: string | null } = await listSessionExchanges(id, { order: "asc", limit: Math.min(100, limit), cursor: cursor ?? undefined });
    exchanges.push(...page.exchanges);
    cursor = page.next_cursor;
  } while (cursor && exchanges.length < limit);
  const sourceURL = new URL(window.location.href);
  sourceURL.hash = "";
  sourceURL.search = "";
  return sessionMarkdown(detail.value, exchanges.slice(0, limit), sourceURL.toString());
}

async function exportMarkdown() {
  if (!detail.value || exporting.value) return;
  exporting.value = true;
  exportError.value = "";
  noteMessage.value = "";
  try {
    const markdown = await renderSessionMarkdown();
    const url = URL.createObjectURL(new Blob([markdown], { type: "text/markdown" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `mimir-session-${detail.value.session.id}.md`;
    anchor.click();
    URL.revokeObjectURL(url);
    noteMessage.value = "Markdown download created.";
  } catch (cause) {
    exportError.value = errorMessage(cause, "The export could not be created.");
  } finally {
    exporting.value = false;
  }
}

async function openSessionNote() {
  if (!detail.value || exporting.value) return;
  exporting.value = true;
  exportError.value = "";
  noteMessage.value = "";
  try {
    const settings = await prepareObsidianVault();
    const markdown = await renderSessionMarkdown(settings.vault ? markdownExportLimit() : OBSIDIAN_URI_EXCHANGE_LIMIT);
    const result = await writeAndOpenSessionNote(detail.value.session, markdown, settings);
    if (result.created === null) noteMessage.value = `Sent ${result.relativePath} to Obsidian${result.truncated ? " with bounded request evidence." : "."}`;
    else noteMessage.value = result.created ? `Created ${result.relativePath} and asked Obsidian to open it.` : `Opened existing note ${result.relativePath}.`;
  } catch (cause) {
    exportError.value = errorMessage(cause, "The session note could not be opened.");
  } finally {
    exporting.value = false;
  }
}

async function openOrConfigureObsidian() {
  if (obsidianConfigured.value) await openSessionNote();
  else await router.push({ path: "/settings", hash: "#session-notes" });
}

void refreshObsidianState();

watch(() => String(route.params.id), load, { immediate: true });
watch([() => route.query.view, () => route.hash], () => {
  evidenceTab.value = route.query.view === "conversation" ? "conversation" : route.query.view === "requests" || route.hash === "#session-activity" ? "requests" : "conversation";
});
watch(() => route.hash, revealEvidenceHash);
useAutoRefresh(refreshLiveness);
onBeforeUnmount(() => { controller?.abort(); stopLive(); });
</script>

<template>
  <section v-if="detail" class="mx-auto min-w-0 max-w-7xl pb-8">
    <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
      <RouterLink to="/sessions" class="inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-600 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" />Sessions</RouterLink>
      <div class="flex min-w-0 flex-wrap items-center justify-end gap-2">
        <span v-if="exportError" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ exportError }}</span>
        <span v-else-if="noteMessage" class="max-w-md text-right text-xs text-zinc-600 dark:text-zinc-400" role="status">{{ noteMessage }}</span>
        <DropdownMenuRoot @update:open="refreshObsidianState">
          <DropdownMenuTrigger as-child>
            <button type="button" :disabled="exporting" class="group inline-flex h-8.5 items-stretch overflow-hidden rounded-[5px] border border-zinc-300 bg-white text-[13px] font-medium text-zinc-700 transition-colors duration-150 ease-out hover:border-zinc-400 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-wait disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-950 dark:text-zinc-300 dark:hover:border-zinc-600 dark:hover:bg-zinc-900"><span class="inline-flex items-center gap-2 px-3"><FileText class="size-3.5 text-zinc-600 dark:text-zinc-400" aria-hidden="true" />{{ exporting ? "Preparing…" : "Session note" }}</span><span class="grid w-7 place-items-center border-l border-zinc-200 text-zinc-600 transition-colors group-hover:bg-zinc-100 dark:border-zinc-800 dark:text-zinc-400 dark:group-hover:bg-zinc-800"><ChevronDown class="size-3.5" aria-hidden="true" /></span></button>
          </DropdownMenuTrigger>
          <DropdownMenuPortal>
            <DropdownMenuContent align="end" :side-offset="6" class="z-50 min-w-64 rounded-[7px] border border-zinc-200 bg-white p-1.5 shadow-[0_18px_50px_rgba(0,0,0,0.18)] focus:outline-none dark:border-zinc-700 dark:bg-zinc-900">
              <DropdownMenuItem class="flex cursor-pointer items-start gap-3 rounded-[4px] px-2.5 py-2.5 text-zinc-700 outline-none select-none data-[highlighted]:bg-stone-100 data-[highlighted]:text-zinc-950 dark:text-zinc-300 dark:data-[highlighted]:bg-zinc-800 dark:data-[highlighted]:text-zinc-50" @select="openOrConfigureObsidian"><BookOpen class="mt-0.5 size-4 shrink-0 text-zinc-600 dark:text-zinc-400" aria-hidden="true" /><span><span class="block text-[13px] font-medium">{{ obsidianConfigured ? "Open in Obsidian" : "Set up Obsidian" }}</span><span class="mt-0.5 block text-[11px] font-normal text-zinc-600 dark:text-zinc-400">{{ obsidianConfigured ? "Create or reopen this project note" : "Choose how Mimir sends session notes" }}</span></span></DropdownMenuItem>
              <DropdownMenuSeparator class="my-1 h-px bg-zinc-200 dark:bg-zinc-700" />
              <DropdownMenuItem class="flex cursor-pointer items-start gap-3 rounded-[4px] px-2.5 py-2.5 text-zinc-700 outline-none select-none data-[highlighted]:bg-stone-100 data-[highlighted]:text-zinc-950 dark:text-zinc-300 dark:data-[highlighted]:bg-zinc-800 dark:data-[highlighted]:text-zinc-50" @select="exportMarkdown"><Download class="mt-0.5 size-4 shrink-0 text-zinc-600 dark:text-zinc-400" aria-hidden="true" /><span><span class="block text-[13px] font-medium">Download Markdown</span><span class="mt-0.5 block text-[11px] font-normal text-zinc-600 dark:text-zinc-400">Save the complete session note locally</span></span></DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenuPortal>
        </DropdownMenuRoot>
      </div>
    </div>
    <div v-if="error || liveError" class="mb-5 flex items-start gap-2.5 border-y border-amber-300 bg-amber-50 px-3 py-2.5 text-[13px] text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200" role="alert">
      <AlertTriangle class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <p class="min-w-0 flex-1">{{ error || liveError }} The last available session data is still shown.</p>
      <button type="button" class="shrink-0 font-medium underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600" @click="retryBanner">Retry</button>
    </div>
    <SessionHeader :session="detail.session" :capture="detail.capture" :liveness="liveness" @saved="applyTitleUpdate" />
    <SessionSummary :session="detail.session" :summary="detail.summary" @open="openPanel('summary')" />
    <div class="grid min-w-0 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
    <div class="min-w-0">
      <div class="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-zinc-200 dark:border-zinc-800">
        <div class="flex gap-4 sm:gap-5" role="tablist" aria-label="Session evidence view" @keydown="moveEvidenceTab">
          <button v-for="tab in ['conversation', 'requests'] as const" :id="`session-tab-${tab}`" :key="tab" type="button" role="tab" :aria-selected="evidenceTab === tab" :aria-controls="`session-panel-${tab}`" :tabindex="evidenceTab === tab ? 0 : -1" class="border-b-2 py-3 text-sm font-medium capitalize focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600" :class="evidenceTab === tab ? 'border-teal-700 text-zinc-950 dark:border-teal-400 dark:text-zinc-100' : 'border-transparent text-zinc-600 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100'" @click="selectEvidenceTab(tab)">{{ tab }}</button>
        </div>
        <div class="flex items-center gap-1">
          <button type="button" :aria-label="`Changes (${detail.git_artifacts.length} commits)`" aria-controls="session-sidebar" class="inline-flex h-8.5 items-center gap-1 rounded-[5px] px-1.5 text-xs text-zinc-600 hover:bg-stone-200 focus-visible:outline-2 focus-visible:outline-teal-600 lg:hidden dark:text-zinc-400 dark:hover:bg-zinc-900" @click="openPanel('changes', $event)"><GitCommitHorizontal class="size-3.5" aria-hidden="true" />Changes<span v-if="detail.git_artifacts.length" class="font-mono">{{ detail.git_artifacts.length }}</span></button>
          <button type="button" aria-controls="session-sidebar" class="inline-flex h-8.5 items-center gap-1 rounded-[5px] px-1.5 text-xs text-zinc-600 hover:bg-stone-200 focus-visible:outline-2 focus-visible:outline-teal-600 lg:hidden dark:text-zinc-400 dark:hover:bg-zinc-900" @click="openPanel('details', $event)"><PanelRight class="size-3.5" aria-hidden="true" />Details</button>
        <button v-if="detail.summary && !detail.session.outcome_reason?.trim()" type="button" aria-haspopup="dialog" class="rounded-[3px] py-2 text-xs text-zinc-600 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100" @click="openPanel('summary', $event)">Summary evidence</button>
        </div>
      </div>
      <div id="session-panel-conversation" v-show="evidenceTab === 'conversation'" role="tabpanel" aria-labelledby="session-tab-conversation" tabindex="0">
        <SessionConversation :session-id="detail.session.id" :supporting-sessions="detail.supporting_sessions" :refresh-key="timelineRevision" />
      </div>
      <div id="session-panel-requests" v-show="evidenceTab === 'requests'" role="tabpanel" aria-labelledby="session-tab-requests" tabindex="0">
        <RequestTimeline :session-id="detail.session.id" :supporting-sessions="detail.supporting_sessions" :refresh-key="timelineRevision" />
      </div>
    </div>
      <SessionDetailsSidebar v-model="sidebarSection" :detail="detail" :live-turns="liveTurns" :liveness="liveness" @saved="refreshDetail" />
    </div>
    <SessionSummaryPanel v-model="summaryOpen" :detail="detail" :return-focus="panelReturnFocus" @navigate="navigateEvidence" />
  </section>
  <section v-else-if="loading" aria-busy="true" class="min-w-0 py-8"><div class="h-4 w-28 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-4 h-8 w-72 max-w-full animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-6 h-28 animate-pulse border-y border-zinc-200 bg-zinc-100 motion-reduce:animate-none dark:border-zinc-800 dark:bg-zinc-900" /></section>
  <section v-else class="py-12 text-center"><h1 class="text-xl font-semibold">Session unavailable</h1><p class="mx-auto mt-2 max-w-md text-sm text-zinc-600 dark:text-zinc-400">{{ error }}</p><div class="mt-4 flex justify-center gap-4"><button class="inline-flex items-center gap-2 text-sm font-medium text-teal-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button><RouterLink to="/sessions" class="text-sm font-medium text-teal-700 dark:text-teal-400">Return to sessions</RouterLink></div></section>
</template>
