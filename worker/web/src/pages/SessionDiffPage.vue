<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, ExternalLink, RotateCw } from "lucide-vue-next";
import { errorMessage, getSession, getSessionDiff, getSessionGitArtifactPatch, type SessionDetail } from "@/lib/api";
import { displayTitle } from "@/lib/sessions";
import { gitArtifactCommitUrl } from "@/lib/git";
import DiffViewer from "@/components/session/DiffViewer.vue";
const route = useRoute();
const detail = ref<SessionDetail | null>(null), patch = ref<string | null>(null), loading = ref(true), error = ref("");
const rawOpen = ref(false);
let controller: AbortController | null = null;
const commit = computed(() => typeof route.params.commit === "string" ? route.params.commit : "");
const artifact = computed(() => detail.value?.git_artifacts.find((item) => item.commit_sha === commit.value) ?? null);
async function load() {
  controller?.abort();
  const requestController = new AbortController(); controller = requestController;
  loading.value = true; error.value = ""; detail.value = null; patch.value = null;
  try {
    const id = String(route.params.id);
    const [session, diff] = await Promise.all([getSession(id, requestController.signal), commit.value ? getSessionGitArtifactPatch(id, commit.value, requestController.signal) : getSessionDiff(id, requestController.signal)]);
    if (!requestController.signal.aborted) { detail.value = session; patch.value = diff; }
  } catch (cause) { if (!requestController.signal.aborted) error.value = errorMessage(cause, "The complete patch could not be loaded."); }
  finally { if (!requestController.signal.aborted) loading.value = false; }
}
watch(() => [String(route.params.id), commit.value], load, { immediate: true });
onUnmounted(() => controller?.abort());
</script>

<template>
  <section v-if="detail && patch !== null" class="min-w-0 pb-12">
    <RouterLink :to="`/sessions/${detail.session.id}`" class="mb-4 inline-flex items-center gap-1.5 text-[13px] text-zinc-600 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" />Session</RouterLink>
    <header class="border-b border-zinc-200 pb-4 dark:border-zinc-800">
      <h1 class="text-2xl font-semibold tracking-[-0.025em]">{{ commit ? 'Commit patch' : 'Legacy outcome patch' }}</h1>
      <p class="mt-1 text-sm text-zinc-600 dark:text-zinc-400">{{ artifact?.subject || displayTitle(detail.session) }}</p>
      <p class="mt-2 break-all font-mono text-xs text-zinc-600 dark:text-zinc-400">{{ commit || detail.session.id }}<template v-if="artifact?.ref"> · {{ artifact.ref }}</template></p>
      <p class="mt-2 text-xs text-zinc-600 dark:text-zinc-400">Exact stored capture, not a remotely fetched source tree. A saved patch does not establish the work outcome.</p>
      <a v-if="artifact && gitArtifactCommitUrl(artifact)" :href="gitArtifactCommitUrl(artifact)!" target="_blank" rel="noreferrer noopener" class="mt-2 inline-flex items-center gap-1 text-xs text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">Remote commit<ExternalLink class="size-3" /><span class="sr-only">(new tab)</span></a>
    </header>
    <DiffViewer :patch="patch" />
    <details class="mt-4 border-t border-zinc-200 pt-3 text-xs text-zinc-600 dark:border-zinc-800 dark:text-zinc-400" @toggle="rawOpen = ($event.target as HTMLDetailsElement).open"><summary class="cursor-pointer focus-visible:outline-2 focus-visible:outline-teal-600">Raw stored patch (including binary payloads)</summary><pre v-if="rawOpen" class="mt-3 max-h-[32rem] overflow-auto border border-zinc-200 p-3 font-mono text-xs dark:border-zinc-800"><code>{{ patch }}</code></pre></details>
    <details v-if="artifact" class="mt-4 border-t border-zinc-200 pt-3 text-xs text-zinc-600 dark:border-zinc-800 dark:text-zinc-400"><summary class="cursor-pointer focus-visible:outline-2 focus-visible:outline-teal-600">Capture provenance</summary><dl class="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2"><dt>Source</dt><dd>{{ artifact.provenance }}</dd><dt>Saved</dt><dd>{{ artifact.saved_at || 'Not recorded' }}</dd><dt>Digest</dt><dd class="break-all font-mono">{{ artifact.patch_sha256 }}</dd><dt>Bytes</dt><dd>{{ artifact.patch_bytes.toLocaleString() }}</dd></dl></details>
  </section>
  <section v-else-if="loading" aria-busy="true" class="py-16"><div class="h-9 w-48 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-8 h-96 animate-pulse bg-zinc-100 motion-reduce:animate-none dark:bg-zinc-900" /></section>
  <section v-else class="py-20 text-center"><h1 class="text-xl font-semibold">Patch unavailable</h1><p class="mx-auto mt-2 max-w-md text-sm text-zinc-500">{{ error || 'No complete patch was recorded for this capture.' }}</p><div class="mt-4 flex justify-center gap-4"><button class="inline-flex items-center gap-2 text-sm text-teal-700 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button><RouterLink :to="`/sessions/${String(route.params.id)}`" class="text-sm text-teal-700 dark:text-teal-400">Return to session</RouterLink></div></section>
</template>
