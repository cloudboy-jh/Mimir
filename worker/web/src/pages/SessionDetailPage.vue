<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, ChevronDown, Database, FileCode2, GitBranch, RotateCw, TriangleAlert } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import { errorMessage, getSession, setSessionOutcome, type Outcome, type SessionDetail } from "@/lib/api";
import { compactNumber, duration, shortDate } from "@/lib/format";

const route = useRoute();
const detail = ref<SessionDetail | null>(null);
const loading = ref(true);
const error = ref("");
const outcome = ref<Outcome>("unresolved");
const reason = ref("");
const saving = ref(false);
const saveError = ref("");
let controller: AbortController | null = null;
let loadVersion = 0;

const session = computed(() => detail.value ? { ...detail.value.session, capture: detail.value.capture, files: detail.value.files, errors: detail.value.errors } : null);
const captureTotal = computed(() => detail.value ? detail.value.capture.saved_exchanges + detail.value.capture.failed_exchanges + detail.value.capture.pending_exchanges : 0);

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
      outcome.value = result.session.outcome;
      reason.value = result.session.outcome_reason ?? "";
      loading.value = false;
      if (result.capture.pending_exchanges === 0) break;
    }
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorMessage(cause, "This session could not be loaded.");
  } finally {
    if (version === loadVersion && !controller.signal.aborted) loading.value = false;
  }
}

async function saveOutcome() {
  if (!session.value || saving.value) return;
  saving.value = true;
  saveError.value = "";
  try {
    await setSessionOutcome(session.value.id, outcome.value, reason.value);
    const result = await getSession(session.value.id);
    detail.value = result;
    outcome.value = result.session.outcome;
    reason.value = result.session.outcome_reason ?? "";
  } catch (cause) {
    saveError.value = errorMessage(cause, "The outcome could not be saved.");
  } finally {
    saving.value = false;
  }
}

watch(() => String(route.params.id), load, { immediate: true });
</script>

<template>
  <section v-if="session">
    <RouterLink to="/sessions" class="mb-6 inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-500 hover:text-zinc-950 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" />Sessions</RouterLink>
    <div class="border-b border-zinc-200 pb-6 dark:border-zinc-800"><div class="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between"><div class="max-w-4xl"><div class="mb-3 flex flex-wrap items-center gap-2"><OutcomeBadge :outcome="session.outcome" /><span v-if="session.state === 'active'" class="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-700 dark:text-emerald-400"><span class="size-1.5 rounded-full bg-emerald-500" />Active</span></div><h1 class="text-2xl font-semibold leading-tight tracking-[-0.025em] text-zinc-950 sm:text-[28px] dark:text-zinc-50">{{ session.intent || "Untitled session" }}</h1><div class="mt-4 flex flex-wrap gap-x-4 gap-y-2 text-[13px] text-zinc-500 dark:text-zinc-400"><strong class="font-medium text-zinc-800 dark:text-zinc-200">{{ session.repo || "No repository" }}</strong><span v-if="session.source_ref" class="inline-flex items-center gap-1"><GitBranch class="size-3.5" />{{ session.source_ref }}</span><span>{{ shortDate(session.started_at) }}</span><span class="font-mono text-xs">{{ session.id }}</span></div></div><div class="space-y-2"><IdentityBadge :label="session.harness || 'Unknown app'" /><IdentityBadge :label="session.model_primary || 'Unknown model'" /></div></div></div>

    <dl class="grid grid-cols-2 border-b border-zinc-200 md:grid-cols-4 dark:border-zinc-800"><div class="border-r border-zinc-200 py-5 pr-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Duration</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ duration(session.started_at, session.ended_at) }}</dd></div><div class="border-r border-zinc-200 px-5 py-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Requests</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ session.request_count }}</dd></div><div class="border-r border-zinc-200 py-5 pr-5 md:px-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Input tokens</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_in) }}</dd></div><div class="py-5 pl-5"><dt class="text-xs text-zinc-500">Output tokens</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_out) }}</dd></div></dl>

    <div class="grid gap-6 border-b border-zinc-200 py-6 md:grid-cols-2 dark:border-zinc-800">
      <section aria-labelledby="capture-heading"><h2 id="capture-heading" class="flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><Database class="size-4" />Capture</h2><p class="mt-2 text-sm text-zinc-700 dark:text-zinc-300"><strong class="font-medium capitalize">{{ session.capture.status }}</strong> · {{ captureTotal }} {{ captureTotal === 1 ? "exchange" : "exchanges" }} in this session</p><details class="group mt-2 text-xs text-zinc-500 dark:text-zinc-400"><summary class="inline-flex cursor-pointer list-none items-center gap-1 font-medium text-teal-700 hover:text-teal-900 focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-teal-400 dark:hover:text-teal-300">Capture details<ChevronDown class="size-3.5 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none" /></summary><dl class="mt-3 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4"><div><dt>Saved</dt><dd class="mt-0.5 font-mono text-zinc-900 dark:text-zinc-100">{{ session.capture.saved_exchanges }}</dd></div><div><dt>Pending</dt><dd class="mt-0.5 font-mono text-zinc-900 dark:text-zinc-100">{{ session.capture.pending_exchanges }}</dd></div><div><dt>Failed</dt><dd class="mt-0.5 font-mono text-zinc-900 dark:text-zinc-100">{{ session.capture.failed_exchanges }}</dd></div><div><dt>Last saved</dt><dd class="mt-0.5 text-zinc-900 dark:text-zinc-100">{{ session.capture.last_saved_at ? shortDate(session.capture.last_saved_at) : "Never" }}</dd></div></dl></details></section>
      <section aria-labelledby="outcome-heading"><h2 id="outcome-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Work outcome</h2><form class="mt-3 space-y-3" @submit.prevent="saveOutcome"><label class="block"><span class="sr-only">Outcome</span><select v-model="outcome" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900"><option value="unresolved">Unresolved</option><option value="landed">Landed</option><option value="discarded">Discarded</option><option value="abandoned">Abandoned</option></select></label><label class="block"><span class="sr-only">Outcome reason</span><textarea v-model="reason" maxlength="2000" rows="2" placeholder="Why did this work land or stop?" class="w-full resize-y rounded-[5px] border border-zinc-300 bg-white px-2.5 py-2 text-[13px] leading-5 placeholder:text-zinc-500 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label><div class="flex items-center gap-3"><button type="submit" :disabled="saving" class="h-8.5 rounded-[5px] bg-zinc-900 px-3 text-[13px] font-medium text-zinc-50 hover:bg-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-wait disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300">{{ saving ? "Saving..." : "Save outcome" }}</button><span v-if="saveError" class="text-xs text-red-700 dark:text-red-400">{{ saveError }}</span><span v-else-if="session.outcome_src" class="text-xs text-zinc-500">Set by {{ session.outcome_src }}<template v-if="session.outcome_updated_at"> · {{ shortDate(session.outcome_updated_at) }}</template></span></div></form></section>
    </div>

    <div class="grid gap-8 pt-8 xl:grid-cols-[minmax(0,1fr)_320px]">
      <div><h2 class="mb-4 text-sm font-semibold text-zinc-900 dark:text-zinc-100">Request timeline</h2><div class="border-t border-zinc-200 dark:border-zinc-800"><RouterLink v-for="exchange in detail?.exchanges" :key="exchange.id" :to="`/requests/${exchange.id}`" class="grid gap-3 border-b border-zinc-200 py-4 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 sm:grid-cols-[120px_minmax(0,1fr)_100px] sm:px-3 dark:border-zinc-800 dark:hover:bg-zinc-900"><time class="font-mono text-xs text-zinc-500">{{ shortDate(exchange.ts) }}</time><div><div class="flex flex-wrap items-center gap-3"><IdentityBadge :label="exchange.provider || 'Unknown provider'" /><IdentityBadge :label="exchange.model" /></div><p class="mt-2 line-clamp-1 text-xs text-zinc-500">{{ exchange.request_excerpt || exchange.id }}</p></div><div class="font-mono text-xs text-zinc-500 sm:text-right">{{ exchange.input_tokens.toLocaleString() }} in</div></RouterLink><p v-if="!detail?.exchanges.length" class="py-10 text-sm text-zinc-500">No request evidence was captured for this session.</p></div></div>
      <aside class="space-y-7"><div><h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><FileCode2 class="size-4" />Files</h2><ul v-if="session.files.length" class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800"><li v-for="file in session.files" :key="file" class="py-2.5 font-mono text-xs text-zinc-600 dark:text-zinc-400">{{ file }}</li></ul><p v-else class="text-sm text-zinc-500">No files detected.</p></div><div><h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><TriangleAlert class="size-4" />Errors</h2><ul v-if="session.errors.length" class="space-y-2"><li v-for="item in session.errors" :key="item" class="rounded-[5px] border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs leading-5 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">{{ item }}</li></ul><p v-else class="text-sm text-zinc-500">No errors detected.</p></div></aside>
    </div>
  </section>
  <section v-else-if="loading" aria-busy="true" class="mx-auto max-w-3xl py-16"><div class="h-4 w-28 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-5 h-8 w-72 max-w-full animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-8 h-28 animate-pulse border-y border-zinc-200 bg-zinc-100 motion-reduce:animate-none dark:border-zinc-800 dark:bg-zinc-900" /></section>
  <section v-else class="py-20 text-center"><h1 class="text-xl font-semibold">Session unavailable</h1><p class="mx-auto mt-2 max-w-md text-sm text-zinc-500 dark:text-zinc-400">{{ error }}</p><div class="mt-4 flex justify-center gap-4"><button class="inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button><RouterLink to="/sessions" class="text-sm font-medium text-teal-700 dark:text-teal-400">Return to sessions</RouterLink></div></section>
</template>
