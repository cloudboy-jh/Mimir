<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { ArrowRight, RotateCw } from "lucide-vue-next";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import IdentityBadge from "@/components/IdentityBadge.vue";
import { errorMessage, getOverview, listSessions, type Overview, type Session } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { compactNumber, relativeDate } from "@/lib/format";

const overview = ref<Overview | null>(null);
const sessions = ref<Session[]>([]);
const loading = ref(true);
const error = ref("");
let controller: AbortController | null = null;

async function load(silent = false) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  if (!silent) loading.value = true;
  error.value = "";
  try {
    [overview.value, sessions.value] = await Promise.all([getOverview(active.signal), listSessions(active.signal)]);
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "Overview data could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

onMounted(() => load());
useAutoRefresh(() => load(true));
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section>
    <div class="mb-7"><h1 class="text-[28px] font-semibold tracking-[-0.025em]">Overview</h1><p class="mt-1.5 max-w-2xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">A compact read on recent memory activity.</p></div>
    <div v-if="loading" aria-busy="true"><div class="grid grid-cols-2 border-y border-zinc-200 md:grid-cols-4 dark:border-zinc-800"><div v-for="index in 4" :key="index" class="px-5 py-5"><div class="h-3 w-20 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-3 h-6 w-14 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div><div class="mt-9 h-64 animate-pulse bg-zinc-100 motion-reduce:animate-none dark:bg-zinc-900" /></div>
    <div v-else-if="error" class="border-y border-zinc-200 py-16 text-center dark:border-zinc-800"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Overview unavailable</p><p class="mt-1 text-sm text-zinc-500">{{ error }}</p><button class="mt-4 inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load()"><RotateCw class="size-4" />Retry</button></div>
    <template v-else-if="overview">
      <dl class="grid grid-cols-2 border-y border-zinc-200 md:grid-cols-4 dark:border-zinc-800"><div v-for="item in [{ label: 'Sessions', value: overview.totals.sessions }, { label: 'Model requests', value: overview.totals.requests }, { label: 'Saved exchanges', value: overview.totals.saved_exchanges }, { label: 'Capture failures', value: overview.totals.capture_failures }]" :key="item.label" class="border-r border-zinc-200 px-5 py-5 first:pl-0 last:border-r-0 dark:border-zinc-800"><dt class="text-xs text-zinc-500">{{ item.label }}</dt><dd class="mt-1 text-xl font-semibold tracking-[-0.025em]">{{ compactNumber(item.value) }}</dd></div></dl>
      <div class="grid gap-10 pt-9 xl:grid-cols-[minmax(0,1.4fr)_minmax(300px,.6fr)]">
        <div><div class="mb-3 flex items-center justify-between"><h2 class="text-sm font-semibold">Recent sessions</h2><RouterLink to="/sessions" class="text-xs font-medium text-teal-700 hover:underline dark:text-teal-400">View all</RouterLink></div><div class="border-t border-zinc-200 dark:border-zinc-800"><RouterLink v-for="session in sessions.slice(0, 4)" :key="session.id" :to="`/sessions/${session.id}`" class="group grid gap-3 border-b border-zinc-200 py-4 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 sm:grid-cols-[minmax(0,1fr)_120px_140px_24px] sm:items-center dark:border-zinc-800"><div><p class="text-sm font-medium text-zinc-900 group-hover:text-teal-700 dark:text-zinc-100 dark:group-hover:text-teal-400">{{ session.intent || "Untitled session" }}</p><p class="mt-1.5 text-xs text-zinc-500">{{ session.repo || "No repository" }} · {{ relativeDate(session.started_at) }}</p></div><OutcomeBadge :outcome="session.outcome" /><p class="text-xs text-zinc-600 dark:text-zinc-400"><span class="font-medium capitalize text-zinc-800 dark:text-zinc-200">{{ session.capture.status }}</span> · {{ session.capture.saved_exchanges }} saved</p><ArrowRight class="hidden size-4 text-zinc-400 sm:block" /></RouterLink><div v-if="!sessions.length" class="border-b border-zinc-200 py-12 text-center dark:border-zinc-800"><p class="text-sm font-medium">No sessions captured yet</p><p class="mt-1 text-sm text-zinc-500">Recent work sessions will appear here.</p></div></div></div>
        <aside class="space-y-8"><section><h2 class="mb-3 text-sm font-semibold">Top models</h2><div class="border-y border-zinc-200 dark:border-zinc-800"><div v-for="model in overview.models" :key="model.name" class="flex items-center justify-between border-b border-zinc-200 py-3 last:border-b-0 dark:border-zinc-800"><IdentityBadge :label="model.name" /><span class="font-mono text-xs text-zinc-500">{{ model.requests }}</span></div><p v-if="!overview.models.length" class="py-4 text-sm text-zinc-500">No model data yet.</p></div></section><section><h2 class="mb-3 text-sm font-semibold">Top apps</h2><div class="border-y border-zinc-200 dark:border-zinc-800"><div v-for="app in overview.apps" :key="app.name" class="flex items-center justify-between border-b border-zinc-200 py-3 last:border-b-0 dark:border-zinc-800"><IdentityBadge :label="app.name" /><span class="font-mono text-xs text-zinc-500">{{ app.requests }}</span></div><p v-if="!overview.apps.length" class="py-4 text-sm text-zinc-500">No app data yet.</p></div></section></aside>
      </div>
    </template>
  </section>
</template>
