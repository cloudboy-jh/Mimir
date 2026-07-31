<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { FileCode2 } from "lucide-vue-next";
import { parsePatch } from "@/lib/diff";
import type { OutcomeEvidence } from "@/lib/api";

const REFERENCED_PREVIEW = 8;

const props = defineProps<{ files: string[]; evidence: OutcomeEvidence | null }>();

const diffs = computed(() => props.evidence?.patch ? parsePatch(props.evidence.patch) : []);
// Changed files come from the commit patch; referenced files are heuristic
// mentions in captured traffic. Never imply a mention was an edit.
const referenced = computed(() => {
  const changed = new Set(diffs.value.map((file) => file.file));
  return props.files.filter((file) => !changed.has(file));
});

const showAllReferenced = ref(false);
const visibleReferenced = computed(() => showAllReferenced.value ? referenced.value : referenced.value.slice(0, REFERENCED_PREVIEW));

watch(() => props.evidence?.commit, () => { showAllReferenced.value = false; });
</script>

<template>
  <div>
    <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><FileCode2 class="size-4" />Touched files <span v-if="referenced.length" class="font-mono text-[11px] font-normal text-zinc-500">{{ referenced.length }}</span></h2>

    <template v-if="referenced.length">
      <p class="mb-2 text-[11px] leading-4 text-zinc-500">Read or edited during the session. Not part of the result diff.</p>
      <ul class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="file in visibleReferenced" :key="file" class="truncate py-2.5 font-mono text-xs text-zinc-600 dark:text-zinc-400" :title="file">{{ file }}</li>
      </ul>
      <button v-if="referenced.length > REFERENCED_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllReferenced = !showAllReferenced">{{ showAllReferenced ? "Show fewer files" : `Show all ${referenced.length} files` }}</button>
    </template>

    <p v-if="!referenced.length" class="text-sm text-zinc-500">No additional files detected.</p>
  </div>
</template>
