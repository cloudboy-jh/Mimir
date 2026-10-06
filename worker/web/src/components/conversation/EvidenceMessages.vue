<script setup lang="ts">
import { computed } from "vue";
import SafeMarkdown from "./SafeMarkdown.vue";
import { conversationTurns } from "@/lib/conversation";
import type { StructuredBlock, StructuredMessage } from "@/lib/structured-exchange";
const props = defineProps<{ messages: StructuredMessage[]; pairedResults?: Record<string, StructuredBlock[]>; originSession?: string }>();
const turns = computed(() => conversationTurns(props.messages).map(turn => ({
  ...turn,
  reasoning: turn.messages.flatMap(message => message.blocks.filter(block => block.type === "reasoning")),
})));
const linked = computed(() => {
  const results: Record<string, StructuredBlock[]> = Object.create(null);
  const calls = new Set(props.messages.flatMap(message => message.blocks.filter(block => block.type === "tool-call" && block.callId).map(block => block.callId!)));
  for (const message of props.messages) for (const block of message.blocks) {
    if (block.type === "tool-result" && block.callId && calls.has(block.callId)) (results[block.callId] ??= []).push(block);
  }
  return results;
});
const resultGroups = computed(() => {
  const groups: Record<string, StructuredBlock[]> = Object.create(null);
  for (const message of props.messages) for (const block of message.blocks) if (block.type === "tool-call") groups[block.id] = block.callId ? [...new Map([...(linked.value[block.callId] ?? []), ...(props.pairedResults?.[block.callId] ?? [])].map(result => [result.id, result])).values()] : [];
  return groups;
});
function visible(block: StructuredBlock) {
  return !(block.type === "tool-result" && block.callId && linked.value[block.callId]);
}
</script>

<template>
  <div class="space-y-7">
    <template v-for="turn in turns" :key="turn.id">
      <article v-if="turn.messages.some(message => message.blocks.some(visible))" :data-role="turn.role" class="min-w-0" :class="turn.role === 'user' ? 'max-w-[76ch] rounded-[5px] bg-stone-100 px-4 py-4 sm:px-5 dark:bg-zinc-900' : 'py-1'">
        <header class="mb-3 flex flex-wrap items-baseline gap-2 text-[13px] font-semibold text-zinc-800 dark:text-zinc-200">
          <span class="capitalize">{{ turn.role === 'user' ? 'You' : turn.role.replaceAll('_', ' ') }}</span>
          <span v-if="turn.name" class="break-words font-mono text-xs font-normal text-zinc-600 dark:text-zinc-400">{{ turn.name }}</span>
        </header>
        <div class="min-w-0 space-y-4">
          <p v-if="turn.role === 'assistant' && turn.messages.every(message => message.blocks.every(block => block.type === 'reasoning'))" class="text-sm text-zinc-600 dark:text-zinc-400">No answer was captured for this turn.</p>
          <div v-for="message in turn.messages" :id="message.id" :key="message.id" class="min-w-0 space-y-4 scroll-mt-4">
            <template v-for="block in message.blocks" :key="block.id">
              <div v-if="block.type === 'text'" :id="block.id" class="scroll-mt-4"><SafeMarkdown :text="block.text" /></div>
              <template v-else-if="block.type === 'reasoning'" />
              <details v-else-if="block.type === 'tool-call'" :id="block.id" data-tool-call class="max-w-[86ch] scroll-mt-4 rounded-[5px] border px-3 py-2" :class="resultGroups[block.id]?.some(result => result.failed) ? 'border-red-300 bg-red-50 dark:border-red-900 dark:bg-red-950/30' : 'border-zinc-200 dark:border-zinc-800'" :open="resultGroups[block.id]?.some(result => result.failed)">
                <summary class="cursor-pointer text-[13px] font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600"><span class="font-mono">{{ block.title || 'Tool call' }}</span><span class="font-normal" :class="resultGroups[block.id]?.some(result => result.failed) ? 'text-red-700 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-400'"> · {{ resultGroups[block.id]?.some(result => result.failed) ? 'Failed' : resultGroups[block.id]?.some(result => result.outputCaptured === false) ? 'Output not captured' : resultGroups[block.id]?.length ? 'Complete' : 'Input only' }}</span></summary>
                <div class="mt-3 border-t border-zinc-200 pt-3 dark:border-zinc-800">
                  <p class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Input</p>
                  <pre class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-[13px] leading-6 text-zinc-800 dark:text-zinc-200" tabindex="0">{{ block.text }}</pre>
                  <div v-for="result in resultGroups[block.id]" :key="result.id" class="mt-4">
                    <p :class="result.failed ? 'text-red-700 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-400'" class="text-xs font-medium">{{ result.failed ? 'Tool error' : 'Output' }}</p>
                    <pre class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-[13px] leading-6 text-zinc-800 dark:text-zinc-200" tabindex="0">{{ result.text }}</pre>
                    <RouterLink v-if="result.exchangeId !== block.exchangeId || result.side !== block.side" :to="{ path: `/requests/${result.exchangeId}`, query: { side: result.side, ...(originSession ? { session: originSession, view: 'conversation' } : {}) }, hash: '#request-evidence-panel' }" class="mt-2 inline-block text-xs text-teal-700 underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">Result source</RouterLink>
                  </div>
                </div>
              </details>
              <details v-else-if="block.type === 'tool-result' && visible(block)" :id="block.id" :open="block.failed" class="max-w-[86ch] scroll-mt-4 rounded-[5px] border px-3 py-2" :class="block.failed ? 'border-red-300 bg-red-50 dark:border-red-900 dark:bg-red-950/30' : 'border-zinc-200 dark:border-zinc-800'">
                <summary class="cursor-pointer text-[13px] font-medium focus-visible:outline-2 focus-visible:outline-teal-600" :class="block.failed ? 'text-red-700 dark:text-red-400' : ''">{{ block.title || 'Tool result' }}{{ block.failed ? ' · Failed' : '' }}</summary>
                <pre class="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-[13px] leading-6" tabindex="0">{{ block.text }}</pre>
              </details>
              <span v-else-if="block.type === 'tool-result'" :id="block.id" class="scroll-mt-4" />
              <div v-else-if="block.type === 'error'" :id="block.id" role="alert" class="scroll-mt-4 rounded-[5px] border border-red-300 bg-red-50 p-3 text-red-800 dark:border-red-900 dark:bg-red-950/30 dark:text-red-300"><p class="mb-1 text-sm font-semibold">Error</p><pre class="whitespace-pre-wrap break-words font-mono text-[13px] leading-6">{{ block.text }}</pre></div>
              <details v-else :id="block.id" class="scroll-mt-4">
                <summary class="cursor-pointer text-[13px] font-medium text-zinc-600 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-zinc-400">{{ block.title || (block.type === 'context' ? 'Context change' : 'Additional data') }}</summary>
                <pre class="mt-3 max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-[13px] leading-6" tabindex="0">{{ block.text }}</pre>
              </details>
            </template>
          </div>
          <details v-if="turn.reasoning.length" data-reasoning class="max-w-[76ch]">
            <summary class="w-fit cursor-pointer rounded-[3px] text-[13px] text-zinc-600 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100">Saved reasoning<span v-if="turn.reasoning.length > 1"> · {{ turn.reasoning.length }} fragments</span></summary>
            <div class="mt-3 space-y-4">
              <div v-for="block in turn.reasoning" :id="block.id" :key="block.id" class="scroll-mt-4"><SafeMarkdown :text="block.text" /></div>
            </div>
          </details>
        </div>
      </article>
      <template v-else>
        <div v-for="message in turn.messages" :id="message.id" :key="message.id" class="scroll-mt-4"><span v-for="block in message.blocks" :id="block.id" :key="block.id" /></div>
      </template>
    </template>
  </div>
</template>
