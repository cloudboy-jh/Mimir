<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { LogEnvelope } from "@/lib/api";
import { structuredEvidence } from "@/lib/structured-exchange";

const props = defineProps<{ envelope: LogEnvelope; side: "request" | "response" }>();
const mode = ref<"structured" | "raw">("structured");
const structured = computed(() => structuredEvidence(props.envelope, props.side));
const raw = computed(() => JSON.stringify(props.side === "request" ? props.envelope.request : props.envelope.response, null, 2));

watch([() => props.side, structured], () => { mode.value = structured.value.recognized ? "structured" : "raw"; }, { immediate: true });
</script>

<template>
  <div>
    <div class="mb-3 flex items-center justify-between gap-3">
      <p class="text-xs text-zinc-500 dark:text-zinc-400">{{ structured.recognized ? "Readable conversation evidence" : "This payload does not match a recognized message format." }}</p>
      <div class="inline-flex rounded-[5px] border border-zinc-300 p-0.5 dark:border-zinc-700" aria-label="Evidence format">
        <button v-for="option in ['structured', 'raw'] as const" :key="option" type="button" class="rounded-[3px] px-2.5 py-1 text-xs font-medium capitalize focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600" :class="mode === option ? 'bg-zinc-900 text-zinc-50 dark:bg-zinc-100 dark:text-zinc-950' : 'text-zinc-500 hover:text-zinc-900 dark:text-zinc-400 dark:hover:text-zinc-100'" :disabled="option === 'structured' && !structured.recognized" @click="mode = option">{{ option }}</button>
      </div>
    </div>
    <div v-if="mode === 'structured'" class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
      <article v-for="(message, index) in structured.messages" :key="index" class="grid gap-3 py-4 sm:grid-cols-[100px_minmax(0,1fr)]">
        <div><span class="text-xs font-semibold capitalize text-zinc-700 dark:text-zinc-300">{{ message.role.replaceAll('_', ' ') }}</span><p v-if="message.name" class="mt-1 font-mono text-[11px] text-zinc-500">{{ message.name }}</p></div>
        <div class="min-w-0 space-y-3">
          <div v-for="(block, blockIndex) in message.blocks" :key="blockIndex" :class="block.type === 'tool-call' || block.type === 'tool-result' ? 'rounded-[5px] border border-zinc-200 bg-stone-50 p-3 dark:border-zinc-800 dark:bg-zinc-900' : block.type === 'error' ? 'rounded-[5px] border border-red-300 bg-red-50 p-3 text-red-900 dark:border-red-900 dark:bg-red-950/40 dark:text-red-200' : ''">
            <p v-if="block.title" class="mb-1.5 font-mono text-[11px] font-medium text-zinc-500">{{ block.title }}</p>
            <pre v-if="block.type !== 'text'" class="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs leading-5">{{ block.text }}</pre>
            <p v-else class="whitespace-pre-wrap break-words text-sm leading-6 text-zinc-800 dark:text-zinc-200">{{ block.text }}</p>
          </div>
        </div>
      </article>
    </div>
    <pre v-else class="max-h-[65vh] overflow-auto rounded-[7px] border border-zinc-200 bg-zinc-950 p-4 font-mono text-xs leading-6 text-zinc-200 dark:border-zinc-800" tabindex="0">{{ raw }}</pre>
  </div>
</template>
