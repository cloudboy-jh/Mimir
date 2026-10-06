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
  <div v-if="supportingSessions.length || files.length || errors.length" class="min-w-0 space-y-3">
    <details v-if="supportingSessions.length" class="min-w-0 border-t border-zinc-200 pt-3 dark:border-zinc-800">
      <summary class="flex cursor-pointer list-none items-center gap-2 rounded-[3px] text-sm font-semibold text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-zinc-100"><GitBranch class="size-3.5 text-zinc-600 dark:text-zinc-400" aria-hidden="true" />Supporting sessions <span class="font-mono text-xs font-normal text-zinc-600 dark:text-zinc-400">{{ supportingSessions.length }}</span><ChevronDown class="ml-auto size-3.5 text-zinc-600 dark:text-zinc-400" aria-hidden="true" /></summary>
      <ol class="mt-2 divide-y divide-zinc-200 dark:divide-zinc-800">
        <li v-for="run in visibleRuns" :key="run.session.id">
          <RouterLink :to="`/sessions/${run.session.id}`" class="block min-w-0 py-2 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 dark:hover:bg-zinc-900" :style="run.depth ? { paddingLeft: `${Math.min(run.depth, 3) * 0.75}rem` } : undefined">
            <p class="flex min-w-0 items-start gap-1.5"><GitBranch class="mt-0.5 size-3.5 shrink-0 text-zinc-600 dark:text-zinc-400" aria-hidden="true" /><span class="break-words text-xs font-medium leading-5 text-zinc-800 dark:text-zinc-200">{{ displayTitle(run.session) }}</span></p>
            <SessionModelStack class="mt-1.5" :app="run.session.harness" :primary="run.session.model_primary" :models="run.session.models" />
            <span class="mt-1 block break-all font-mono text-[11px] text-zinc-600 dark:text-zinc-400">{{ run.session.id }}</span>
          </RouterLink>
        </li>
      </ol>
      <button v-if="supportingSessions.length > RUN_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllRuns = !showAllRuns">{{ showAllRuns ? "Show fewer sessions" : `Show all ${supportingSessions.length} sessions` }}</button>
    </details>
    <details v-if="files.length" class="min-w-0 border-t border-zinc-200 pt-3 dark:border-zinc-800">
      <summary class="flex cursor-pointer list-none items-center gap-2 rounded-[3px] text-sm font-semibold text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-zinc-100">Referenced files <span class="font-mono text-xs font-normal text-zinc-600 dark:text-zinc-400">{{ files.length }}</span><ChevronDown class="ml-auto size-3.5 text-zinc-600 dark:text-zinc-400" aria-hidden="true" /></summary>
      <SessionFiles v-if="files.length" class="mt-3" :files="files" compact />
    </details>
    <details v-if="errors.length" class="min-w-0 border-t border-zinc-200 pt-3 dark:border-zinc-800">
      <summary class="flex cursor-pointer list-none items-center gap-2 rounded-[3px] text-sm font-semibold text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-zinc-100"><TriangleAlert class="size-3.5 text-zinc-600 dark:text-zinc-400" aria-hidden="true" />Errors <span class="font-mono text-xs font-normal text-zinc-600 dark:text-zinc-400">{{ errors.length }}</span><ChevronDown class="ml-auto size-3.5 text-zinc-600 dark:text-zinc-400" aria-hidden="true" /></summary>
      <ul v-if="errors.length" class="mt-2 divide-y divide-zinc-200 dark:divide-zinc-800">
        <li v-for="item in visibleErrors" :key="item.signature" class="py-2">
          <details>
            <summary class="flex cursor-pointer list-none items-baseline justify-between gap-3 rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden">
              <span class="min-w-0 truncate text-xs leading-5 text-zinc-800 dark:text-zinc-200">{{ item.signature }}</span>
              <span class="flex shrink-0 items-center gap-1.5 font-mono text-[11px] text-zinc-600 dark:text-zinc-400">{{ item.count }}×<ChevronDown class="size-3.5" aria-hidden="true" /></span>
            </summary>
            <div class="mt-2 space-y-1.5 text-[11px] leading-4 text-zinc-600 dark:text-zinc-400">
              <p class="max-h-40 overflow-y-auto whitespace-pre-wrap break-words font-mono">{{ item.signature }}</p>
              <p v-if="item.last_seen_at">Last seen {{ relativeDate(item.last_seen_at) }}</p>
              <p v-else>Recorded before error tracking; no timing data.</p>
              <RouterLink v-if="item.latest_exchange_id" :to="{ path: `/requests/${item.latest_exchange_id}`, query: { session: sessionId } }" class="inline-block font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">Newest matching request</RouterLink>
            </div>
          </details>
        </li>
      </ul>
      <button v-if="errors.length > ERROR_PREVIEW" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAllErrors = !showAllErrors">{{ showAllErrors ? "Show fewer errors" : `Show all ${errors.length} errors` }}</button>
    </details>
  </div>
</template>
