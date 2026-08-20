<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { FileCode2 } from "lucide-vue-next";

const REFERENCED_PREVIEW = 8;

const props = defineProps<{ files: string[] }>();

// Session files are mentions from captured traffic, not an edit inventory.
const referenced = computed(() => props.files);

const showAllReferenced = ref(false);
const visibleReferenced = computed(() => showAllReferenced.value ? referenced.value : referenced.value.slice(0, REFERENCED_PREVIEW));

watch(() => props.files, () => { showAllReferenced.value = false; });
</script>

<template>
  <div>
    <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><FileCode2 class="size-4" aria-hidden="true" />Referenced files <span v-if="referenced.length" class="font-mono text-[11px] font-normal text-zinc-500">{{ referenced.length }}</span></h2>

    <template v-if="referenced.length">
      <p class="mb-2 text-[11px] leading-4 text-zinc-500">Mentioned in captured traffic. Kept separate from files recorded in commit patches.</p>
      <ul class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="file in visibleReferenced" :key="file" class="truncate py-2.5 font-mono text-xs text-zinc-600 dark:text-zinc-400" :title="file">{{ file }}</li>
      </ul>
      <button v-if="referenced.length > REFERENCED_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllReferenced = !showAllReferenced">{{ showAllReferenced ? "Show fewer files" : `Show all ${referenced.length} files` }}</button>
    </template>

    <p v-if="!referenced.length" class="text-sm text-zinc-500">No additional file references detected.</p>
  </div>
</template>
