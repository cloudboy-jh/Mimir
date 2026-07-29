<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ExternalLink, FileCode2, GitCommitHorizontal } from "lucide-vue-next";
import { parsePatch } from "@/lib/diff";
import { commitUrl, shortCommit } from "@/lib/git";
import type { OutcomeEvidence } from "@/lib/api";

const CHANGED_PREVIEW = 6;
const REFERENCED_PREVIEW = 8;

const props = defineProps<{ files: string[]; evidence: OutcomeEvidence | null }>();

const diffs = computed(() => props.evidence?.patch ? parsePatch(props.evidence.patch) : []);
const totals = computed(() => ({
  added: diffs.value.reduce((sum, file) => sum + file.added, 0),
  removed: diffs.value.reduce((sum, file) => sum + file.removed, 0),
}));
// Changed files come from the commit patch; referenced files are heuristic
// mentions in captured traffic. Never imply a mention was an edit.
const referenced = computed(() => {
  const changed = new Set(diffs.value.map((file) => file.file));
  return props.files.filter((file) => !changed.has(file));
});

const showAllChanged = ref(false);
const showAllReferenced = ref(false);
const visibleDiffs = computed(() => showAllChanged.value ? diffs.value : diffs.value.slice(0, CHANGED_PREVIEW));
const visibleReferenced = computed(() => showAllReferenced.value ? referenced.value : referenced.value.slice(0, REFERENCED_PREVIEW));
const commitHref = computed(() => commitUrl(props.evidence));

watch(() => props.evidence?.commit, () => { showAllChanged.value = false; showAllReferenced.value = false; });
</script>

<template>
  <div>
    <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><FileCode2 class="size-4" />Files</h2>

    <div v-if="evidence?.commit" class="border-y border-zinc-200 py-2.5 dark:border-zinc-800">
      <p class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-zinc-600 dark:text-zinc-400">
        <GitCommitHorizontal class="size-3.5 text-zinc-500" aria-hidden="true" />
        <a v-if="commitHref" :href="commitHref" target="_blank" rel="noreferrer noopener" class="inline-flex items-center gap-1 font-mono text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ shortCommit(evidence.commit) }}<ExternalLink class="size-3" aria-hidden="true" /></a>
        <span v-else class="font-mono text-zinc-800 dark:text-zinc-200">{{ shortCommit(evidence.commit) }}</span>
        <span v-if="evidence.ref" class="font-mono">{{ evidence.ref }}</span>
        <span v-if="evidence.base_commit">on <span class="font-mono">{{ shortCommit(evidence.base_commit) }}</span></span>
        <span v-if="evidence.provenance">· {{ evidence.provenance }}</span>
      </p>
      <p v-if="diffs.length" class="mt-1.5 font-mono text-[11px] text-zinc-500">{{ diffs.length }} {{ diffs.length === 1 ? "file" : "files" }} changed · <span class="text-emerald-700 dark:text-emerald-400">+{{ totals.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ totals.removed }}</span></p>
      <p v-else class="mt-1.5 text-[11px] text-zinc-500">Diff unavailable for this commit.</p>
    </div>

    <ul v-if="visibleDiffs.length" class="mt-3 space-y-2">
      <li v-for="file in visibleDiffs" :key="file.file" class="rounded-[5px] border border-zinc-200 dark:border-zinc-800">
        <details>
          <summary class="flex cursor-pointer list-none items-baseline justify-between gap-3 px-2.5 py-2 [&::-webkit-details-marker]:hidden">
            <span class="min-w-0 truncate font-mono text-xs text-zinc-700 dark:text-zinc-300" :title="file.file">{{ file.file }}</span>
            <span class="shrink-0 font-mono text-[11px]"><span class="text-emerald-700 dark:text-emerald-400">+{{ file.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ file.removed }}</span></span>
          </summary>
          <div class="max-h-72 overflow-auto border-t border-zinc-200 dark:border-zinc-800"><pre class="py-1 font-mono text-[11px] leading-4"><div v-for="(line, index) in file.lines" :key="index" class="whitespace-pre px-2.5" :class="line.type === 'add' ? 'bg-emerald-50 text-emerald-900 dark:bg-emerald-950/60 dark:text-emerald-200' : line.type === 'del' ? 'bg-red-50 text-red-900 dark:bg-red-950/60 dark:text-red-200' : line.type === 'meta' ? 'text-zinc-400 dark:text-zinc-500' : 'text-zinc-600 dark:text-zinc-400'">{{ line.text || " " }}</div></pre></div>
        </details>
      </li>
    </ul>
    <button v-if="diffs.length > CHANGED_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllChanged = !showAllChanged">{{ showAllChanged ? "Show fewer changed files" : `Show all ${diffs.length} changed files` }}</button>

    <template v-if="referenced.length">
      <h3 class="mb-2 mt-5 text-xs font-medium text-zinc-500">Referenced files ({{ referenced.length }})</h3>
      <ul class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="file in visibleReferenced" :key="file" class="truncate py-2.5 font-mono text-xs text-zinc-600 dark:text-zinc-400" :title="file">{{ file }}</li>
      </ul>
      <button v-if="referenced.length > REFERENCED_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllReferenced = !showAllReferenced">{{ showAllReferenced ? "Show fewer referenced files" : `Show all ${referenced.length} referenced files` }}</button>
    </template>

    <p v-if="!files.length && !diffs.length && !evidence?.commit" class="text-sm text-zinc-500">No files detected.</p>
  </div>
</template>
