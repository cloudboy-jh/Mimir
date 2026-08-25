<script setup lang="ts">
import { computed, ref } from "vue";
import { ChevronDown, GitBranch, TriangleAlert } from "lucide-vue-next";
import SessionFiles from "@/components/session/SessionFiles.vue";
import SessionModelStack from "@/components/session/SessionModelStack.vue";
import type { SessionDetail } from "@/lib/api";
import { relativeDate } from "@/lib/format";
import { displayTitle } from "@/lib/sessions";

const ERROR_PREVIEW = 5;
const RUN_PREVIEW = 5;

const props = defineProps<{ sessionId: string; supportingSessions: SessionDetail["supporting_sessions"]; files: string[]; errors: SessionDetail["errors"] }>();

type SupportingSession = SessionDetail["supporting_sessions"][number];
type TreeNode = { session: SupportingSession; children: TreeNode[] };

function buildTree(sessions: SupportingSession[], rootId: string): TreeNode[] {
  const byParent = new Map<string | null, SupportingSession[]>();
  for (const session of sessions) {
    const parent = session.parent_session_id;
    const list = byParent.get(parent) ?? [];
    list.push(session);
    byParent.set(parent, list);
  }
  const childrenOf = (parent: string | null): TreeNode[] =>
    (byParent.get(parent) ?? []).map((session) => ({ session, children: childrenOf(session.id) }));
  return childrenOf(rootId);
}

function flattenTree(nodes: TreeNode[], depth = 0, rows: Array<{ session: SupportingSession; depth: number }> = []): Array<{ session: SupportingSession; depth: number }> {
  for (const node of nodes) {
    rows.push({ session: node.session, depth });
    flattenTree(node.children, depth + 1, rows);
  }
  return rows;
}

const showAllErrors = ref(false);
const showAllRuns = ref(false);
const visibleErrors = computed(() => showAllErrors.value ? props.errors : props.errors.slice(0, ERROR_PREVIEW));
const tree = computed(() => buildTree(props.supportingSessions, props.sessionId));
const flatRuns = computed(() => flattenTree(tree.value));
const visibleRuns = computed(() => showAllRuns.value ? flatRuns.value : flatRuns.value.slice(0, RUN_PREVIEW));
</script>

<template>
  <aside class="space-y-8" aria-label="Session evidence and sub-agent sessions">
    <div v-if="supportingSessions.length">
      <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><GitBranch class="size-4 text-zinc-500" aria-hidden="true" />Sub-agent sessions ({{ supportingSessions.length }})</h2>
      <ol class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="run in visibleRuns" :key="run.session.id">
          <RouterLink :to="`/sessions/${run.session.id}`" class="block py-3 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 dark:hover:bg-zinc-900" :style="run.depth ? { paddingLeft: `${run.depth * 1.25}rem` } : undefined">
            <p class="flex min-w-0 items-start gap-1.5"><span v-if="run.depth" class="mt-1 h-px w-3 shrink-0 bg-zinc-300 dark:bg-zinc-700" aria-hidden="true"></span><GitBranch class="mt-0.5 size-3.5 shrink-0 text-zinc-400" :class="run.depth ? 'text-zinc-400' : 'text-zinc-500'" aria-hidden="true" /><span class="line-clamp-2 text-xs font-medium text-zinc-800 dark:text-zinc-200">{{ displayTitle(run.session) }}</span></p>
            <SessionModelStack class="mt-2" :app="run.session.harness" :primary="run.session.model_primary" :models="run.session.models" />
            <span class="mt-1.5 block truncate font-mono text-[11px] text-zinc-500">{{ run.session.id }}</span>
          </RouterLink>
        </li>
      </ol>
      <button v-if="supportingSessions.length > RUN_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllRuns = !showAllRuns">{{ showAllRuns ? "Show fewer sessions" : `Show all ${supportingSessions.length} sessions` }}</button>
    </div>
    <div>
      <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><TriangleAlert class="size-4" aria-hidden="true" />Errors<span class="font-mono text-[11px] font-normal text-zinc-500">{{ errors.length }}</span></h2>
      <ul v-if="errors.length" class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="item in visibleErrors" :key="item.signature" class="py-2.5">
          <details class="group">
            <summary class="flex cursor-pointer list-none items-baseline justify-between gap-3 rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden">
              <span class="min-w-0 truncate text-xs leading-5 text-zinc-800 dark:text-zinc-200">{{ item.signature }}</span>
              <span class="flex shrink-0 items-center gap-1.5 font-mono text-[11px] text-zinc-500">{{ item.count }}×<ChevronDown class="size-3.5 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none" aria-hidden="true" /></span>
            </summary>
            <div class="mt-2 space-y-1.5 text-[11px] leading-4 text-zinc-500">
              <p class="max-h-40 overflow-y-auto whitespace-pre-wrap break-words font-mono text-zinc-600 dark:text-zinc-400">{{ item.signature }}</p>
              <p v-if="item.last_seen_at">Last seen {{ relativeDate(item.last_seen_at) }}</p>
              <p v-else>Recorded before error tracking; no timing data.</p>
              <RouterLink v-if="item.latest_exchange_id" :to="{ path: `/requests/${item.latest_exchange_id}`, query: { session: sessionId } }" class="inline-block font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">Newest matching request</RouterLink>
            </div>
          </details>
        </li>
      </ul>
      <p v-else class="text-sm text-zinc-500">No errors detected.</p>
      <button v-if="errors.length > ERROR_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllErrors = !showAllErrors">{{ showAllErrors ? "Show fewer errors" : `Show all ${errors.length} errors` }}</button>
    </div>
    <SessionFiles :files="files" />
  </aside>
</template>
