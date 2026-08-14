<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ExternalLink, GitCommitHorizontal } from "lucide-vue-next";
import type { OutcomeEvidence } from "@/lib/api";
import { parsePatch } from "@/lib/diff";
import { commitUrl, shortCommit } from "@/lib/git";

const PREVIEW_LIMIT = 5;
const props = defineProps<{ sessionId: string; evidence: OutcomeEvidence | null; sourceRef?: string | null }>();
const showAll = ref(false);
const diffs = computed(() => props.evidence?.patch ? parsePatch(props.evidence.patch) : []);
const totals = computed(() => ({
  added: props.evidence?.patch_additions ?? diffs.value.reduce((sum, file) => sum + file.added, 0),
  removed: props.evidence?.patch_deletions ?? diffs.value.reduce((sum, file) => sum + file.removed, 0),
}));
const fileCount = computed(() => props.evidence?.patch_files ?? diffs.value.length);
const hasDiff = computed(() => Boolean(props.evidence?.patch || props.evidence?.patch_r2_key));
const visibleDiffs = computed(() => showAll.value ? diffs.value : diffs.value.slice(0, PREVIEW_LIMIT));
const commitHref = computed(() => commitUrl(props.evidence));
const evidenceHref = computed(() => commitHref.value ?? props.evidence?.url ?? null);

watch(() => props.evidence?.commit, () => { showAll.value = false; });
</script>

<template>
  <section aria-labelledby="changes-heading">
    <div class="flex items-baseline justify-between gap-3 border-b border-zinc-200 pb-2.5 dark:border-zinc-800">
      <h2 id="changes-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Changes</h2>
      <p v-if="hasDiff" class="shrink-0 font-mono text-[11px] text-zinc-500">
        {{ fileCount }} {{ fileCount === 1 ? "file" : "files" }}
        <span class="ml-1 text-emerald-700 dark:text-emerald-400">+{{ totals.added }}</span>
        <span class="ml-0.5 text-red-700 dark:text-red-400">−{{ totals.removed }}</span>
      </p>
    </div>

    <div v-if="evidence?.commit" class="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-zinc-200 py-2.5 text-xs text-zinc-600 dark:border-zinc-800 dark:text-zinc-400">
      <GitCommitHorizontal class="size-3.5 text-zinc-500" aria-hidden="true" />
      <a v-if="commitHref" :href="commitHref" target="_blank" rel="noreferrer noopener" class="inline-flex items-center gap-1 font-mono text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ shortCommit(evidence.commit) }}<ExternalLink class="size-3" aria-hidden="true" /></a>
      <span v-else class="font-mono text-zinc-800 dark:text-zinc-200">{{ shortCommit(evidence.commit) }}</span>
      <span v-if="evidence.ref || sourceRef" class="font-mono">{{ evidence.ref || sourceRef }}</span>
      <span v-if="evidence.base_commit">on {{ shortCommit(evidence.base_commit) }}</span>
      <span v-if="evidence.provenance">via {{ evidence.provenance }}</span>
    </div>
    <p v-else-if="evidenceHref" class="border-b border-zinc-200 py-2.5 text-xs dark:border-zinc-800"><a :href="evidenceHref" target="_blank" rel="noreferrer noopener" class="inline-flex items-center gap-1 break-all text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ evidenceHref }}<ExternalLink class="size-3 shrink-0" aria-hidden="true" /></a></p>
    <p v-else-if="evidence?.note" class="border-b border-zinc-200 py-2.5 text-xs leading-5 text-zinc-600 dark:border-zinc-800 dark:text-zinc-400">{{ evidence.note }}</p>
    <p v-else-if="hasDiff" class="border-b border-zinc-200 py-2.5 text-xs text-zinc-500 dark:border-zinc-800">Recorded patch evidence</p>
    <p v-else class="pt-3 text-sm text-zinc-500">No committed changes recorded.</p>

    <template v-if="evidence?.commit || hasDiff">
      <p v-if="!hasDiff" class="pt-3 text-xs text-zinc-500">Diff unavailable for this commit.</p>
      <ul v-else class="divide-y divide-zinc-200 border-b border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="file in visibleDiffs" :key="file.file" class="min-w-0">
          <details>
            <summary class="flex cursor-pointer list-none items-baseline justify-between gap-3 py-2.5 [&::-webkit-details-marker]:hidden">
              <span class="min-w-0 truncate font-mono text-xs text-zinc-700 dark:text-zinc-300" :title="file.file">{{ file.file }}</span>
              <span class="shrink-0 font-mono text-[11px]"><span class="text-emerald-700 dark:text-emerald-400">+{{ file.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ file.removed }}</span></span>
            </summary>
            <div class="max-h-72 overflow-auto border-t border-zinc-200 dark:border-zinc-800"><pre class="min-w-max py-1 font-mono text-[11px] leading-4"><div v-for="(line, index) in file.lines" :key="index" class="whitespace-pre px-2.5" :class="line.type === 'add' ? 'bg-emerald-50 text-emerald-900 dark:bg-emerald-950/60 dark:text-emerald-200' : line.type === 'del' ? 'bg-red-50 text-red-900 dark:bg-red-950/60 dark:text-red-200' : line.type === 'meta' ? 'text-zinc-400 dark:text-zinc-500' : 'text-zinc-600 dark:text-zinc-400'">{{ line.text || " " }}</div></pre></div>
          </details>
        </li>
      </ul>
      <button v-if="diffs.length > PREVIEW_LIMIT" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAll = !showAll">{{ showAll ? "Show fewer files" : `Show all ${diffs.length} files` }}</button>
      <RouterLink v-if="hasDiff" :to="`/sessions/${sessionId}/diff`" class="mt-3 inline-flex text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">View full diff</RouterLink>
    </template>
  </section>
</template>
