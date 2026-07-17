<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, ChevronDown, Database, FileCode2, GitBranch, TriangleAlert } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import { exchangesForSession, sessionById } from "@/lib/mock";
import { compactNumber, duration, shortDate } from "@/lib/format";

const route = useRoute();
const session = computed(() => sessionById(String(route.params.id)));
const sessionExchanges = computed(() => exchangesForSession(String(route.params.id)));
type LiveStatus = {
  session_id: string;
  outcome: "landed" | "discarded" | "abandoned" | "unresolved";
  outcome_src: string | null;
  outcome_reason: string | null;
  outcome_updated_at: string | null;
  capture: { status: string; saved_exchanges: number; failed_exchanges: number; pending_exchanges: number; last_saved_at: string | null };
  receipt: { label: string; detail: string };
};
const liveStatus = ref<LiveStatus | null>(null);
const liveLoading = ref(false);
const liveError = ref("");
let statusLoad = 0;
const captureTotal = computed(() => {
  const capture = session.value?.capture;
  return capture ? capture.saved_exchanges + capture.failed_exchanges + capture.pending_exchanges : 0;
});

watch(() => String(route.params.id), async (id, _previous, onCleanup) => {
  const load = ++statusLoad;
  const controller = new AbortController();
  onCleanup(() => controller.abort());
  liveStatus.value = null;
  liveError.value = "";
  if (sessionById(id)) return;
  liveLoading.value = true;
  try {
    for (const delay of [0, 500, 1_000, 1_500, 2_000]) {
      if (delay) await new Promise((resolve) => setTimeout(resolve, delay));
      if (load !== statusLoad) return;
      const response = await fetch(`/dashboard/api/sessions/${encodeURIComponent(id)}/status`, { cache: "no-store", headers: { accept: "application/json" }, signal: controller.signal });
      if (!response.ok) throw new Error(response.status === 403 ? "Cloudflare Access could not verify this session." : "This session could not be loaded.");
      const status = await response.json() as LiveStatus;
      if (load !== statusLoad) return;
      liveStatus.value = status;
      if (status.capture.pending_exchanges === 0) break;
    }
  } catch (error) {
    if (controller.signal.aborted) return;
    liveStatus.value = null;
    liveError.value = error instanceof Error ? error.message : "This session could not be loaded.";
  } finally {
    if (load === statusLoad) liveLoading.value = false;
  }
}, { immediate: true });
</script>

<template>
  <section v-if="session">
    <RouterLink to="/sessions" class="mb-6 inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-500 hover:text-zinc-950 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" />Sessions</RouterLink>
    <div class="border-b border-zinc-200 pb-6 dark:border-zinc-800">
      <div class="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
        <div class="max-w-4xl">
          <div class="mb-3 flex flex-wrap items-center gap-2">
            <OutcomeBadge :outcome="session.outcome" />
            <span v-if="session.state === 'active'" class="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-700 dark:text-emerald-400"><span class="size-1.5 rounded-full bg-emerald-500" />Active</span>
          </div>
          <h1 class="text-2xl font-semibold leading-tight tracking-[-0.025em] text-zinc-950 sm:text-[28px] dark:text-zinc-50">{{ session.intent }}</h1>
          <div class="mt-4 flex flex-wrap gap-x-4 gap-y-2 text-[13px] text-zinc-500 dark:text-zinc-400"><strong class="font-medium text-zinc-800 dark:text-zinc-200">{{ session.repo }}</strong><span class="inline-flex items-center gap-1"><GitBranch class="size-3.5" />{{ session.source_ref }}</span><span>{{ shortDate(session.started_at) }}</span><span class="font-mono text-xs">{{ session.id }}</span></div>
        </div>
        <div class="space-y-2"><IdentityBadge :label="session.harness" /><IdentityBadge :label="session.model_primary" /></div>
      </div>
    </div>

    <dl class="grid grid-cols-2 border-b border-zinc-200 md:grid-cols-4 dark:border-zinc-800"><div class="border-r border-zinc-200 py-5 pr-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Duration</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ duration(session.started_at, session.ended_at) }}</dd></div><div class="border-r border-zinc-200 px-5 py-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Requests</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ session.request_count }}</dd></div><div class="border-r border-zinc-200 py-5 pr-5 md:px-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Input tokens</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_in) }}</dd></div><div class="py-5 pl-5"><dt class="text-xs text-zinc-500">Output tokens</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_out) }}</dd></div></dl>

    <div class="grid gap-6 border-b border-zinc-200 py-6 md:grid-cols-2 dark:border-zinc-800">
      <section aria-labelledby="capture-heading">
        <h2 id="capture-heading" class="flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><Database class="size-4" />Capture</h2>
        <p class="mt-2 text-sm text-zinc-700 dark:text-zinc-300"><strong class="font-medium capitalize">{{ session.capture.status }}</strong> · {{ captureTotal }} {{ captureTotal === 1 ? "exchange" : "exchanges" }} in this session</p>
        <details class="group mt-2 text-xs text-zinc-500 dark:text-zinc-400">
          <summary class="inline-flex cursor-pointer list-none items-center gap-1 font-medium text-teal-700 hover:text-teal-900 focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-teal-400 dark:hover:text-teal-300">
            Capture details
            <ChevronDown class="size-3.5 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none" />
          </summary>
          <dl class="mt-3 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4">
            <div><dt>Saved</dt><dd class="mt-0.5 font-mono text-zinc-900 dark:text-zinc-100">{{ session.capture.saved_exchanges }}</dd></div>
            <div><dt>Pending</dt><dd class="mt-0.5 font-mono text-zinc-900 dark:text-zinc-100">{{ session.capture.pending_exchanges }}</dd></div>
            <div><dt>Failed</dt><dd class="mt-0.5 font-mono text-zinc-900 dark:text-zinc-100">{{ session.capture.failed_exchanges }}</dd></div>
            <div><dt>Last saved</dt><dd class="mt-0.5 text-zinc-900 dark:text-zinc-100">{{ session.capture.last_saved_at ? shortDate(session.capture.last_saved_at) : "Never" }}</dd></div>
          </dl>
        </details>
      </section>
      <section aria-labelledby="outcome-heading">
        <h2 id="outcome-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Work outcome</h2>
        <p v-if="session.outcome_reason" class="mt-2 text-sm leading-6 text-zinc-700 dark:text-zinc-300">{{ session.outcome_reason }}</p>
        <p class="mt-1 text-xs text-zinc-500"><template v-if="session.outcome_src">Set by {{ session.outcome_src }}<template v-if="session.outcome_updated_at"> · {{ shortDate(session.outcome_updated_at) }}</template></template><template v-else>No outcome evidence recorded.</template></p>
      </section>
    </div>

    <div class="grid gap-8 pt-8 xl:grid-cols-[minmax(0,1fr)_320px]">
      <div><h2 class="mb-4 text-sm font-semibold text-zinc-900 dark:text-zinc-100">Request timeline</h2><div class="border-t border-zinc-200 dark:border-zinc-800"><RouterLink v-for="exchange in sessionExchanges" :key="exchange.id" :to="`/requests/${exchange.id}`" class="grid gap-3 border-b border-zinc-200 py-4 hover:bg-stone-50 sm:grid-cols-[120px_minmax(0,1fr)_100px] sm:px-3 dark:border-zinc-800 dark:hover:bg-zinc-900"><time class="font-mono text-xs text-zinc-500">{{ shortDate(exchange.ts) }}</time><div><div class="flex flex-wrap items-center gap-3"><IdentityBadge :label="exchange.provider" /><IdentityBadge :label="exchange.model" /></div><p class="mt-2 line-clamp-1 text-xs text-zinc-500">{{ JSON.parse(exchange.request).messages[0].content }}</p></div><div class="font-mono text-xs text-zinc-500 sm:text-right">{{ exchange.input_tokens.toLocaleString() }} in</div></RouterLink><p v-if="!sessionExchanges.length" class="py-10 text-sm text-zinc-500">No request evidence is included in this mock session.</p></div></div>
      <aside class="space-y-7">
        <div><h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><FileCode2 class="size-4" />Files</h2><ul class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800"><li v-for="file in session.files" :key="file" class="py-2.5 font-mono text-xs text-zinc-600 dark:text-zinc-400">{{ file }}</li></ul></div>
        <div>
          <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><TriangleAlert class="size-4" />Errors</h2>
          <ul v-if="session.errors.length" class="space-y-2"><li v-for="error in session.errors" :key="error" class="rounded-[5px] border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs leading-5 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">{{ error }}</li></ul>
          <p v-else class="text-sm text-zinc-500">No errors detected.</p>
        </div>
      </aside>
    </div>
  </section>
  <section v-else-if="liveStatus" class="mx-auto max-w-3xl">
    <RouterLink to="/sessions" class="mb-6 inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-500 hover:text-zinc-950 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" />Sessions</RouterLink>
    <div class="border-b border-zinc-200 pb-6 dark:border-zinc-800">
      <p class="text-sm font-medium text-zinc-500 dark:text-zinc-400">Capture receipt</p>
      <h1 class="mt-2 text-2xl font-semibold tracking-[-0.025em] text-zinc-950 dark:text-zinc-50">{{ liveStatus.receipt.label }}</h1>
      <p class="mt-2 text-sm text-zinc-600 dark:text-zinc-300">{{ liveStatus.receipt.detail }}</p>
    </div>
    <div class="grid gap-6 border-b border-zinc-200 py-6 sm:grid-cols-2 dark:border-zinc-800">
      <section aria-labelledby="live-capture-heading">
        <h2 id="live-capture-heading" class="flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><Database class="size-4" />Capture</h2>
        <p class="mt-2 text-sm text-zinc-700 dark:text-zinc-300"><strong class="font-medium capitalize">{{ liveStatus.capture.status }}</strong> · Last saved {{ liveStatus.capture.last_saved_at ? shortDate(liveStatus.capture.last_saved_at) : "Never" }}</p>
        <dl class="mt-4 grid grid-cols-3 gap-4 text-xs text-zinc-500 dark:text-zinc-400">
          <div><dt>Saved</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ liveStatus.capture.saved_exchanges }}</dd></div>
          <div><dt>Pending</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ liveStatus.capture.pending_exchanges }}</dd></div>
          <div><dt>Failed</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ liveStatus.capture.failed_exchanges }}</dd></div>
        </dl>
      </section>
      <section aria-labelledby="live-outcome-heading">
        <h2 id="live-outcome-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Work outcome</h2>
        <div class="mt-2"><OutcomeBadge :outcome="liveStatus.outcome" /></div>
        <p v-if="liveStatus.outcome_reason" class="mt-3 text-sm leading-6 text-zinc-700 dark:text-zinc-300">{{ liveStatus.outcome_reason }}</p>
        <p class="mt-2 text-xs text-zinc-500"><template v-if="liveStatus.outcome_src">Set by {{ liveStatus.outcome_src }}<template v-if="liveStatus.outcome_updated_at"> · {{ shortDate(liveStatus.outcome_updated_at) }}</template></template><template v-else>No outcome evidence recorded.</template></p>
      </section>
    </div>
    <details class="group py-5 text-xs text-zinc-500 dark:text-zinc-400">
      <summary class="inline-flex cursor-pointer list-none items-center gap-1 font-medium text-teal-700 hover:text-teal-900 focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-teal-400 dark:hover:text-teal-300">Session details<ChevronDown class="size-3.5 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none" /></summary>
      <p class="mt-3 font-mono text-zinc-700 dark:text-zinc-300">{{ liveStatus.session_id }}</p>
    </details>
  </section>
  <section v-else-if="liveLoading" aria-busy="true" class="mx-auto max-w-3xl py-16"><div class="h-4 w-28 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-5 h-8 w-72 max-w-full animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-8 h-28 animate-pulse border-y border-zinc-200 bg-zinc-100 motion-reduce:animate-none dark:border-zinc-800 dark:bg-zinc-900" /></section>
  <section v-else class="py-20 text-center"><h1 class="text-xl font-semibold">Session unavailable</h1><p v-if="liveError" class="mx-auto mt-2 max-w-md text-sm text-zinc-500 dark:text-zinc-400">{{ liveError }}</p><RouterLink to="/sessions" class="mt-4 inline-block text-sm font-medium text-teal-700 dark:text-teal-400">Return to sessions</RouterLink></section>
</template>
