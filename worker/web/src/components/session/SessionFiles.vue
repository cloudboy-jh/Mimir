<script setup lang="ts">
import { computed } from "vue";
import { FileCode2, GitCommitHorizontal } from "lucide-vue-next";
import { parsePatch } from "@/lib/diff";
import type { OutcomeEvidence } from "@/lib/api";

const props = defineProps<{ files: string[]; evidence: OutcomeEvidence | null }>();
const diffs = computed(() => props.evidence?.patch ? parsePatch(props.evidence.patch) : []);
const totals = computed(() => ({
  added: diffs.value.reduce((sum, file) => sum + file.added, 0),
  removed: diffs.value.reduce((sum, file) => sum + file.removed, 0),
}));
</script>

<template>
  <div>
    <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><FileCode2 class="size-4" />Files</h2>
    <div v-if="evidence?.commit" class="mb-3 border-y border-zinc-200 py-2.5 dark:border-zinc-800">
      <p class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-zinc-600 dark:text-zinc-400"><GitCommitHorizontal class="size-3.5 text-zinc-500" /><span class="font-mono text-zinc-800 dark:text-zinc-200">{{ evidence.commit.slice(0, 7) }}</span><span v-if="evidence.base_commit">on <span class="font-mono">{{ evidence.base_commit.slice(0, 7) }}</span></span><span v-if="evidence.provenance">· {{ evidence.provenance }}</span></p>
    </div>
    <template v-if="diffs.length">
      <p class="mb-2 font-mono text-[11px] text-zinc-500">{{ diffs.length }} {{ diffs.length === 1 ? "file" : "files" }} · <span class="text-emerald-700 dark:text-emerald-400">+{{ totals.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ totals.removed }}</span></p>
      <ul class="space-y-2">
        <li v-for="file in diffs" :key="file.file" class="rounded-[5px] border border-zinc-200 dark:border-zinc-800">
          <details class="group">
            <summary class="flex cursor-pointer list-none items-baseline justify-between gap-3 px-2.5 py-2 [&::-webkit-details-marker]:hidden">
              <span class="min-w-0 truncate font-mono text-xs text-zinc-700 dark:text-zinc-300">{{ file.file }}</span>
              <span class="shrink-0 font-mono text-[11px]"><span class="text-emerald-700 dark:text-emerald-400">+{{ file.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ file.removed }}</span></span>
            </summary>
            <div class="overflow-x-auto border-t border-zinc-200 dark:border-zinc-800"><pre class="px-0 py-1 font-mono text-[11px] leading-4"><div v-for="(line, index) in file.lines" :key="index" class="whitespace-pre px-2.5" :class="line.type === 'add' ? 'bg-emerald-50 text-emerald-900 dark:bg-emerald-950/60 dark:text-emerald-200' : line.type === 'del' ? 'bg-red-50 text-red-900 dark:bg-red-950/60 dark:text-red-200' : line.type === 'meta' ? 'text-zinc-400 dark:text-zinc-500' : 'text-zinc-600 dark:text-zinc-400'">{{ line.text || " " }}</div></pre></div>
          </details>
        </li>
      </ul>
    </template>
    <ul v-if="files.length" class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800"><li v-for="file in files" :key="file" class="py-2.5 font-mono text-xs text-zinc-600 dark:text-zinc-400">{{ file }}</li></ul>
    <p v-if="!files.length && !diffs.length" class="text-sm text-zinc-500">No files detected.</p>
  </div>
</template>
