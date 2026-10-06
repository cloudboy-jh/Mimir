<script setup lang="ts">
import { computed, ref, useId, watch } from "vue";
import { parsePatch, splitDiffLines } from "@/lib/diff";
import { highlightDiffLine } from "@/lib/diff-highlight";
import Select from "@/components/ui/Select.vue";
const props = defineProps<{ patch: string }>();
const id = useId();
const query = ref("");
const mode = ref<"unified" | "split">("unified");
const selected = ref(0);
const collapsed = ref(new Set<number>());
const files = computed(() => parsePatch(props.patch).map((file, index) => {
  const html = new Map(file.lines.map((line) => [line, highlightDiffLine(file.file, line)]));
  return { ...file, index, html, rows: splitDiffLines(file.lines) };
}));
const fileOptions = computed(() => files.value.map((file) => ({ value: String(file.index), label: file.file, description: `${file.oldFile && file.oldFile !== file.file ? `${file.oldFile} · ` : ""}+${file.added} −${file.removed}` })));
const matching = computed(() => files.value.filter((file) => `${file.file} ${file.oldFile ?? ""}`.toLowerCase().includes(query.value.toLowerCase())));
const shown = computed(() => matching.value.filter((file) => file.index === selected.value));
watch(() => props.patch, () => { query.value = ""; selected.value = 0; collapsed.value = new Set(); });
watch(matching, (items) => { if (!items.some((file) => file.index === selected.value)) selected.value = items[0]?.index ?? 0; });
function jump(index: number) {
  selected.value = index;
  collapsed.value.delete(index);
  document.getElementById(`${id}-file-${index}`)?.scrollIntoView({ block: "nearest" });
}
function toggle(index: number) {
  const next = new Set(collapsed.value);
  if (next.has(index)) next.delete(index); else next.add(index);
  collapsed.value = next;
}
const tone = (type?: string) => type === "add" ? "bg-emerald-50 dark:bg-emerald-950/30" : type === "del" ? "bg-red-50 dark:bg-red-950/30" : type === "meta" ? "bg-stone-200/50 text-zinc-600 dark:bg-stone-900 dark:text-zinc-400" : "";
</script>

<template>
  <div class="diff-reader min-w-0">
    <div class="flex flex-wrap items-end gap-3 border-b border-zinc-200 py-3 dark:border-zinc-800">
      <p class="mr-auto text-xs text-zinc-600 dark:text-zinc-400">{{ files.length }} changed {{ files.length === 1 ? 'file' : 'files' }}</p>
      <div role="group" aria-label="Diff layout" class="flex gap-1">
        <button v-for="layout in (['unified', 'split'] as const)" :key="layout" type="button" :aria-pressed="mode === layout" class="h-9 rounded-[5px] border border-zinc-300 px-3 text-xs capitalize focus-visible:outline-2 focus-visible:outline-teal-600 dark:border-zinc-700" :class="mode === layout ? 'bg-zinc-900 text-stone-100 hover:bg-zinc-800 dark:bg-stone-200 dark:text-zinc-900 dark:hover:bg-stone-300' : 'hover:bg-stone-200 dark:hover:bg-stone-800'" @click="mode = layout">{{ layout }}</button>
      </div>
    </div>
    <div v-if="files.length > 1" class="my-3 md:hidden"><Select :model-value="String(selected)" label="Changed file" :options="fileOptions" searchable class="h-9 w-full font-mono text-xs" @update:model-value="query = ''; jump(Number($event))" /></div>
    <div class="grid items-start gap-4 md:grid-cols-[minmax(160px,240px)_minmax(0,1fr)]">
      <nav v-if="files.length" aria-label="Changed files" class="sticky top-20 hidden max-h-[70vh] overflow-auto border-b border-zinc-200 md:block dark:border-zinc-800">
        <label v-if="files.length >= 8" class="sticky top-0 block bg-stone-50 py-2 dark:bg-stone-950" :for="`${id}-search`"><span class="sr-only">Filter changed files</span><input :id="`${id}-search`" v-model="query" type="search" placeholder="Filter files" class="h-8 w-full rounded-[5px] border border-zinc-300 bg-transparent px-2 text-xs focus:outline-2 focus:outline-teal-600 dark:border-zinc-700" /></label>
        <button v-for="file in matching" :key="file.index" type="button" :aria-current="selected === file.index ? 'location' : undefined" class="flex w-full items-baseline justify-between gap-2 border-b border-zinc-200 px-2 py-2.5 text-left text-xs last:border-0 hover:bg-stone-200 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-teal-600 dark:border-zinc-800 dark:hover:bg-stone-900" :class="selected === file.index ? 'bg-stone-200/60 text-teal-800 dark:bg-stone-900 dark:text-teal-300' : ''" @click="jump(file.index)"><span class="min-w-0 break-all font-mono">{{ file.file }}</span><span class="shrink-0 text-zinc-600 dark:text-zinc-400">+{{ file.added }} −{{ file.removed }}</span></button>
      </nav>
      <div class="min-w-0">
        <section v-for="file in shown" :id="`${id}-file-${file.index}`" :key="file.index" class="min-w-0 scroll-mt-20 border-y border-zinc-200 dark:border-zinc-800">
          <button class="flex w-full flex-wrap items-baseline justify-between gap-2 py-2.5 text-left focus-visible:outline-2 focus-visible:outline-teal-600" :aria-expanded="!collapsed.has(file.index)" :aria-controls="`${id}-body-${file.index}`" @click="toggle(file.index)"><span class="min-w-0 break-all font-mono text-xs" :class="files.length > 1 ? 'hidden md:inline' : ''"><span aria-hidden="true">{{ collapsed.has(file.index) ? '▸' : '▾' }}</span> {{ file.file }}</span><span v-if="files.length > 1" class="text-xs md:hidden">{{ collapsed.has(file.index) ? 'Expand file' : 'Collapse file' }}</span><span class="text-xs"><span class="mr-3 text-zinc-600 dark:text-zinc-400">{{ file.status }}<template v-if="file.binary"> · binary</template></span><span class="font-mono text-emerald-700 dark:text-emerald-400">+{{ file.added }}</span> <span class="font-mono text-red-700 dark:text-red-400">−{{ file.removed }}</span></span></button>
          <div v-if="!collapsed.has(file.index)" :id="`${id}-body-${file.index}`">
            <p v-if="file.status === 'renamed' || file.status === 'copied'" class="pb-2 font-mono text-xs text-zinc-600 dark:text-zinc-400">{{ file.oldFile }} → {{ file.newFile }}</p>
            <p v-if="file.oldMode || file.newMode" class="pb-2 font-mono text-xs text-zinc-600 dark:text-zinc-400">Mode {{ file.oldMode || 'none' }} → {{ file.newMode || 'none' }}</p>
            <p v-if="file.binary" class="pb-3 text-xs text-zinc-600 dark:text-zinc-400">Binary content is preserved in the exact patch. No text diff is available.</p>
            <p v-if="!file.binary && !file.added && !file.removed" class="pb-3 text-xs text-zinc-600 dark:text-zinc-400">No text lines changed in this capture.</p>
            <div v-if="file.lines.length" class="overflow-x-auto" tabindex="0" :aria-label="`Diff for ${file.file}`">
              <table v-if="mode === 'unified'" class="w-full border-collapse font-mono text-[13px] leading-6"><caption class="sr-only">Unified diff for {{ file.file }}</caption><tbody><tr v-for="(line, index) in file.lines" :key="index" :class="tone(line.type)"><td class="w-12 select-none px-2 text-right text-zinc-600 dark:text-zinc-400">{{ line.oldLine }}</td><td class="w-12 select-none px-2 text-right text-zinc-600 dark:text-zinc-400">{{ line.newLine }}</td><td class="w-4 select-none text-zinc-600 dark:text-zinc-400">{{ line.type === 'add' ? '+' : line.type === 'del' ? '−' : '' }}</td><td class="whitespace-pre px-2"><code v-html="file.html.get(line)" /></td></tr></tbody></table>
              <table v-else class="w-full min-w-[680px] border-collapse font-mono text-[13px] leading-6"><caption class="sr-only">Split diff for {{ file.file }}: old on the left, new on the right</caption><colgroup><col class="w-12" /><col /><col class="w-12" /><col /></colgroup><thead><tr class="text-zinc-600 dark:text-zinc-400"><th colspan="2" class="text-left font-normal">Before</th><th colspan="2" class="text-left font-normal">After</th></tr></thead><tbody><template v-for="(row, index) in file.rows" :key="index"><tr v-if="row.meta !== undefined" :class="tone('meta')"><td colspan="4" class="whitespace-pre px-2">{{ row.meta }}</td></tr><tr v-else><td class="select-none px-2 text-right text-zinc-600 dark:text-zinc-400" :class="tone(row.left?.type)">{{ row.left?.oldLine }}</td><td class="whitespace-pre px-2" :class="tone(row.left?.type)"><code v-if="row.left" v-html="file.html.get(row.left)" /></td><td class="select-none border-l border-zinc-200 px-2 text-right text-zinc-600 dark:border-zinc-800 dark:text-zinc-400" :class="tone(row.right?.type)">{{ row.right?.newLine }}</td><td class="whitespace-pre px-2" :class="tone(row.right?.type)"><code v-if="row.right" v-html="file.html.get(row.right)" /></td></tr></template></tbody></table>
            </div>
          </div>
        </section>
        <p v-if="!matching.length" class="py-6 text-sm text-zinc-600 dark:text-zinc-400">{{ files.length ? 'No filenames match this search.' : 'This patch contains no changed-file blocks.' }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.diff-reader :deep(.hljs-keyword), .diff-reader :deep(.hljs-selector-tag), .diff-reader :deep(.hljs-literal) { color: oklch(51.1% 0.096 186.391); }
.diff-reader :deep(.hljs-string), .diff-reader :deep(.hljs-attr) { color: oklch(50.8% 0.118 165.612); }
.diff-reader :deep(.hljs-number), .diff-reader :deep(.hljs-title) { color: oklch(55.5% 0.163 48.998); }
.diff-reader :deep(.hljs-comment) { color: oklch(55.2% 0.016 285.938); }
.dark .diff-reader :deep(.hljs-keyword), .dark .diff-reader :deep(.hljs-selector-tag), .dark .diff-reader :deep(.hljs-literal) { color: oklch(77.7% 0.152 181.912); }
.dark .diff-reader :deep(.hljs-string), .dark .diff-reader :deep(.hljs-attr) { color: oklch(76.5% 0.177 163.223); }
.dark .diff-reader :deep(.hljs-number), .dark .diff-reader :deep(.hljs-title) { color: oklch(82.8% 0.189 84.429); }
.dark .diff-reader :deep(.hljs-comment) { color: oklch(70.5% 0.015 286.067); }
</style>
