<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { AlertTriangle, ArrowLeft, RotateCw } from "lucide-vue-next";
import CommitPatchDialog from "@/components/session/CommitPatchDialog.vue";
import SessionHeader from "@/components/session/SessionHeader.vue";
import SessionSidebar from "@/components/session/SessionSidebar.vue";
import SessionSummary from "@/components/session/SessionSummary.vue";
import SessionTurns from "@/components/session/SessionTurns.vue";
import { errorMessage, getSession, type SessionDetail, type TitleUpdate } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";

const route = useRoute();
const router = useRouter();
const detail = ref<SessionDetail | null>(null);
const loading = ref(true);
const error = ref("");
let controller: AbortController | null = null;

const sessionId = computed(() => String(route.params.id));
const openCommit = computed(() => {
  const sha = typeof route.query.commit === "string" ? route.query.commit : "";
  return (sha && detail.value?.commits.find((commit) => commit.commit_sha === sha)) || null;
});

async function load(silent = false) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  if (!silent) {
    loading.value = true;
    detail.value = null;
  }
  error.value = "";
  try {
    detail.value = await getSession(sessionId.value, active.signal);
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "This session could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

function setCommit(sha: string | null) {
  const query = { ...route.query };
  if (sha) query.commit = sha;
  else delete query.commit;
  void router.replace({ query });
}

function applyTitle(update: TitleUpdate) {
  if (detail.value?.session.id === update.id) Object.assign(detail.value.session, update);
}

watch(sessionId, () => load(), { immediate: true });
// Only a running session changes underneath the reader.
useAutoRefresh(() => (detail.value?.session.state === "active" ? load(true) : undefined));
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section v-if="detail" class="mx-auto min-w-0 max-w-7xl pb-8">
    <RouterLink to="/sessions" class="mb-3 inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-600 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" aria-hidden="true" />Sessions</RouterLink>
    <div v-if="error" class="mb-5 flex items-start gap-2.5 border-y border-amber-300 bg-amber-50 px-3 py-2.5 text-[13px] text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200" role="alert">
      <AlertTriangle class="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <p class="min-w-0 flex-1">{{ error }} The last loaded session data is still shown.</p>
      <button type="button" class="shrink-0 font-medium underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600" @click="load(true)">Retry</button>
    </div>
    <SessionHeader :session="detail.session" :receipt="detail.receipt" @saved="applyTitle" />
    <SessionSummary :summary="detail.summary" :active="detail.session.state === 'active'" />
    <div class="mt-2 grid min-w-0 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
      <SessionTurns :session-id="detail.session.id" :supporting-sessions="detail.supporting_sessions" class="min-w-0" />
      <SessionSidebar :detail="detail" class="lg:mt-4" @open-commit="setCommit" />
    </div>
    <CommitPatchDialog :commit="openCommit" @close="setCommit(null)" />
  </section>
  <section v-else-if="loading" aria-busy="true" aria-label="Loading session" class="min-w-0 py-8"><div class="h-4 w-28 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-4 h-8 w-72 max-w-full animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-6 h-28 animate-pulse border-y border-zinc-200 bg-zinc-100 motion-reduce:animate-none dark:border-zinc-800 dark:bg-zinc-900" /></section>
  <section v-else class="py-12 text-center"><h1 class="text-xl font-semibold">Session unavailable</h1><p class="mx-auto mt-2 max-w-md text-sm text-zinc-600 dark:text-zinc-400">{{ error }}</p><div class="mt-4 flex justify-center gap-4"><button type="button" class="inline-flex items-center gap-2 text-sm font-medium text-teal-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="load()"><RotateCw class="size-4" aria-hidden="true" />Retry</button><RouterLink to="/sessions" class="text-sm font-medium text-teal-700 dark:text-teal-400">Return to sessions</RouterLink></div></section>
</template>
