<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, Download, RotateCw } from "lucide-vue-next";
import RequestTimeline from "@/components/session/RequestTimeline.vue";
import SessionCapture from "@/components/session/SessionCapture.vue";
import SessionEvidenceSidebar from "@/components/session/SessionEvidenceSidebar.vue";
import SessionHeader from "@/components/session/SessionHeader.vue";
import SessionOutcome from "@/components/session/SessionOutcome.vue";
import { errorMessage, getSession, listSessionExchanges, parseOutcomeEvidence, type SessionDetail, type SessionExchange } from "@/lib/api";
import { markdownExportLimit, sessionMarkdown } from "@/lib/markdown";

const route = useRoute();
const detail = ref<SessionDetail | null>(null);
const loading = ref(true);
const error = ref("");
let controller: AbortController | null = null;
let loadVersion = 0;

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
  try {
    for (const delay of [0, 500, 1_000, 1_500, 2_000]) {
      if (delay) await wait(delay, controller.signal);
      const result = await getSession(String(route.params.id), controller.signal);
      if (version !== loadVersion) return;
      detail.value = result;
      loading.value = false;
      if (result.capture.pending_exchanges === 0) break;
    }
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

// Only the newest recorded evidence describes the current outcome. Falling back
// to older commits would show a diff that no longer matches the session result.
const commitEvidence = computed(() => {
  for (const event of detail.value?.outcome_events ?? []) {
    const parsed = parseOutcomeEvidence(event.evidence_json);
    if (parsed) return parsed.commit ? parsed : null;
  }
  return null;
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
    <SessionHeader :session="detail.session" />
    <div class="grid gap-6 border-b border-zinc-200 py-6 md:grid-cols-2 dark:border-zinc-800">
      <SessionCapture :capture="detail.capture" />
      <SessionOutcome :detail="detail" @saved="refreshDetail" />
    </div>
    <div class="grid gap-8 pt-8 xl:grid-cols-[minmax(0,1fr)_320px]">
      <RequestTimeline :session-id="detail.session.id" />
      <SessionEvidenceSidebar :supporting-sessions="detail.supporting_sessions" :files="detail.files" :errors="detail.errors" :evidence="commitEvidence" />
    </div>
  </section>
  <section v-else-if="loading" aria-busy="true" class="mx-auto max-w-3xl py-16"><div class="h-4 w-28 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-5 h-8 w-72 max-w-full animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-8 h-28 animate-pulse border-y border-zinc-200 bg-zinc-100 motion-reduce:animate-none dark:border-zinc-800 dark:bg-zinc-900" /></section>
  <section v-else class="py-20 text-center"><h1 class="text-xl font-semibold">Session unavailable</h1><p class="mx-auto mt-2 max-w-md text-sm text-zinc-500 dark:text-zinc-400">{{ error }}</p><div class="mt-4 flex justify-center gap-4"><button class="inline-flex items-center gap-2 text-sm font-medium text-teal-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button><RouterLink to="/sessions" class="text-sm font-medium text-teal-700 dark:text-teal-400">Return to sessions</RouterLink></div></section>
</template>
