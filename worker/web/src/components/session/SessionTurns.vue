<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useTemplateRef, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import SafeMarkdown from "@/components/conversation/SafeMarkdown.vue";
import Select from "@/components/ui/Select.vue";
import { errorMessage, listTurns, type Session, type Turn } from "@/lib/api";
import { shortDate } from "@/lib/format";
import { orderOptions } from "@/lib/options";

const PAGE_SIZE = 8;
const props = defineProps<{ sessionId: string; supportingSessions: Session[] }>();
const route = useRoute();
const router = useRouter();
const order = computed<"asc" | "desc">(() => (route.query.order === "desc" ? "desc" : "asc"));
const turns = ref<Turn[]>([]);
const loading = ref(false);
const error = ref("");
const nextCursor = ref<string | null>(null);
const container = useTemplateRef<HTMLElement>("container");
// Cursors of the pages already visited, so Previous returns without refetching history.
const pageCursors = ref<Array<string | undefined>>([undefined]);
const pageIndex = computed(() => pageCursors.value.length - 1);
const sessionNames = computed(() => new Map(props.supportingSessions.map((session) => [session.id, session.display_title])));
let controller: AbortController | null = null;

async function load(scroll = false) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loading.value = true;
  error.value = "";
  try {
    const page = await listTurns(props.sessionId, { order: order.value, limit: PAGE_SIZE, cursor: pageCursors.value.at(-1) }, active.signal);
    turns.value = page.turns;
    nextCursor.value = page.next_cursor;
    if (scroll) container.value?.scrollIntoView({ block: "start" });
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "The conversation could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

function changePage(direction: -1 | 1) {
  if (direction === 1 && nextCursor.value) pageCursors.value = [...pageCursors.value, nextCursor.value];
  else if (direction === -1 && pageIndex.value > 0) pageCursors.value = pageCursors.value.slice(0, -1);
  else return;
  void load(true);
}

function setOrder(value: string) {
  const query = { ...route.query };
  if (value === "desc") query.order = "desc";
  else delete query.order;
  void router.replace({ query });
}

watch([() => props.sessionId, order], () => {
  pageCursors.value = [undefined];
  turns.value = [];
  void load();
}, { immediate: true });
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section ref="container" aria-labelledby="conversation-heading" class="scroll-mt-20">
    <div class="flex items-center gap-3 border-b border-zinc-200 py-2 dark:border-zinc-800">
      <h2 id="conversation-heading" class="text-sm font-semibold">Conversation</h2>
      <Select :model-value="order" :options="orderOptions" label="Conversation order" class="ml-auto w-36 shrink-0" @update:model-value="setOrder" />
      <button type="button" :disabled="loading" class="h-8.5 shrink-0 rounded-[3px] px-2 text-[13px] font-medium text-teal-700 underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:opacity-50 dark:text-teal-400" @click="load()">Refresh</button>
    </div>
    <p v-if="error" role="alert" class="my-4 text-sm text-red-700 dark:text-red-400">{{ error }}<button type="button" class="ml-3 underline focus-visible:outline-2 focus-visible:outline-teal-600" @click="load()">Retry</button></p>
    <div v-if="loading && !turns.length" aria-busy="true" aria-label="Loading conversation" class="space-y-4 py-6"><div v-for="row in 3" :key="row" class="h-20 animate-pulse bg-zinc-100 motion-reduce:animate-none dark:bg-zinc-900" /></div>
    <p v-else-if="!turns.length && !error" class="py-8 text-sm text-zinc-600 dark:text-zinc-400">No saved conversation for this session.</p>

    <article v-for="turn in turns" :key="turn.exchange_id" class="border-b border-zinc-200 py-6 dark:border-zinc-800">
      <header class="mb-4 flex flex-wrap gap-x-3 gap-y-1 text-xs text-zinc-600 dark:text-zinc-400">
        <time :datetime="turn.ts" class="font-mono">{{ shortDate(turn.ts) }}</time>
        <RouterLink v-if="turn.session_id !== sessionId" :to="`/sessions/${turn.session_id}`" class="text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">Sub-agent: {{ sessionNames.get(turn.session_id) ?? turn.session_id }}</RouterLink>
        <span v-if="turn.model" class="font-mono">{{ turn.model }}</span>
        <span v-if="turn.truncated">Truncated</span>
      </header>

      <details v-if="turn.request_kind === 'compaction'" class="text-[13px] text-zinc-600 dark:text-zinc-400">
        <summary class="w-fit cursor-pointer rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600">Context compaction</summary>
        <div v-if="turn.assistant" class="mt-3"><SafeMarkdown :text="turn.assistant" /></div>
      </details>
      <p v-else-if="turn.capture_status !== 'saved' && !turn.user && !turn.assistant && !turn.tools.length" class="text-sm text-zinc-600 dark:text-zinc-400">{{ turn.capture_status === "accepted" ? "Conversation pending." : `Conversation unavailable (${turn.capture_status}).` }}</p>
      <div v-else class="min-w-0 space-y-5">
        <div v-if="turn.user" class="max-w-[76ch] rounded-[5px] bg-stone-100 px-4 py-4 sm:px-5 dark:bg-zinc-900">
          <p class="mb-2 text-[13px] font-semibold text-zinc-800 dark:text-zinc-200">You</p>
          <SafeMarkdown :text="turn.user" />
        </div>
        <details v-if="turn.reasoning" class="max-w-[76ch]">
          <summary class="w-fit cursor-pointer rounded-[3px] text-[13px] text-zinc-600 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100">Reasoning</summary>
          <div class="mt-3"><SafeMarkdown :text="turn.reasoning" /></div>
        </details>
        <details v-for="(tool, index) in turn.tools" :key="index" :open="tool.error" class="max-w-[86ch] rounded-[5px] border px-3 py-2" :class="tool.error ? 'border-red-300 bg-red-50 dark:border-red-900 dark:bg-red-950/30' : 'border-zinc-200 dark:border-zinc-800'">
          <summary class="cursor-pointer text-[13px] font-medium focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600"><span class="font-mono">{{ tool.name }}</span><span class="font-normal" :class="tool.error ? 'text-red-700 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-400'"> · {{ tool.error ? "Failed" : tool.output === null ? "Input only" : "Complete" }}</span></summary>
          <div class="mt-3 space-y-4 border-t border-zinc-200 pt-3 dark:border-zinc-800">
            <div v-if="tool.input !== null">
              <p class="text-xs font-medium text-zinc-600 dark:text-zinc-400">Input</p>
              <pre class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-[13px] leading-6 text-zinc-800 dark:text-zinc-200" tabindex="0">{{ tool.input }}</pre>
            </div>
            <div v-if="tool.output !== null">
              <p class="text-xs font-medium" :class="tool.error ? 'text-red-700 dark:text-red-400' : 'text-zinc-600 dark:text-zinc-400'">{{ tool.error ? "Tool error" : "Output" }}</p>
              <pre class="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-words font-mono text-[13px] leading-6 text-zinc-800 dark:text-zinc-200" tabindex="0">{{ tool.output }}</pre>
            </div>
          </div>
        </details>
        <div v-if="turn.assistant">
          <p class="mb-2 text-[13px] font-semibold text-zinc-800 dark:text-zinc-200">Assistant</p>
          <SafeMarkdown :text="turn.assistant" />
        </div>
        <p v-else-if="!turn.tools.length" class="text-sm text-zinc-600 dark:text-zinc-400">No answer was captured for this turn.</p>
      </div>
    </article>

    <div class="flex flex-wrap items-center justify-between gap-3 pt-5 text-[13px] text-zinc-600 dark:text-zinc-400">
      <p>Page {{ pageIndex + 1 }}<span v-if="loading && turns.length"> · Refreshing…</span></p>
      <div class="flex gap-3">
        <button v-if="pageIndex > 0" type="button" :disabled="loading" class="rounded-[5px] border border-zinc-300 px-3 py-2 text-zinc-700 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-teal-600 disabled:opacity-50 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-900" @click="changePage(-1)">Previous</button>
        <button v-if="nextCursor" type="button" :disabled="loading" class="rounded-[5px] border border-zinc-300 px-3 py-2 font-medium text-zinc-700 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-teal-600 disabled:opacity-50 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-900" @click="changePage(1)">Next</button>
      </div>
    </div>
  </section>
</template>
