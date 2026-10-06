<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { ChevronDown } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import type { LiveSessionTurn, SessionLiveness } from "@/lib/api";
import { compactNumber, shortDate } from "@/lib/format";

const props = defineProps<{ turns: LiveSessionTurn[]; liveness: SessionLiveness }>();
const latest = computed(() => props.turns[props.turns.length - 1]);
const expanded = ref(false);
watch(() => props.liveness, (liveness) => { if (liveness !== "active") expanded.value = false; });
</script>

<template>
  <details v-if="turns.length || liveness !== 'finalized'" :open="expanded" class="group min-w-0 border-b border-zinc-200 pb-2 dark:border-zinc-800" @toggle="expanded = ($event.target as HTMLDetailsElement).open">
    <summary class="flex cursor-pointer list-none flex-wrap items-center gap-x-3 gap-y-1 rounded-[3px] text-xs focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden">
      <span class="font-medium text-zinc-800 dark:text-zinc-200">Incoming activity</span>
      <span class="text-zinc-600 dark:text-zinc-400" aria-live="polite">{{ turns.length }} {{ turns.length === 1 ? 'event' : 'events' }}<template v-if="liveness === 'disconnected'"> · Disconnected</template><template v-else-if="liveness === 'finalized'"> · Finalized</template><template v-else> · Active</template></span>
      <span v-if="liveness === 'active' && latest" class="min-w-0 flex-1 truncate text-zinc-600 dark:text-zinc-400">{{ latest.excerpt || latest.model || "New harness event" }}</span>
      <ChevronDown class="ml-auto size-3.5 shrink-0 text-zinc-600 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none dark:text-zinc-400" aria-hidden="true" />
    </summary>
    <p class="mt-2 text-xs leading-5 text-zinc-600 dark:text-zinc-400">Temporary harness events, not capture receipts. Saved requests appear in the canonical record.</p>
    <p v-if="!turns.length" class="mt-1 text-xs leading-5 text-zinc-600 dark:text-zinc-400">No incoming events are available yet.</p>
    <ol v-else class="mt-2 max-h-64 overflow-y-auto border-t border-zinc-200 dark:border-zinc-800">
      <li v-for="(turn, index) in turns" :key="turn.exchange_id || `${turn.ts}-${index}`" class="grid gap-1.5 border-b border-zinc-200 py-2 sm:grid-cols-[100px_minmax(0,1fr)_auto] dark:border-zinc-800">
        <time class="font-mono text-[11px] text-zinc-600 dark:text-zinc-400" :datetime="turn.ts">{{ shortDate(turn.ts) }}</time>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-1.5">
            <IdentityBadge v-if="turn.provider" :label="turn.provider" />
            <IdentityBadge :label="turn.model || 'Unknown model'" />
            <span v-if="turn.request_kind && turn.request_kind !== 'primary'" class="text-[11px] text-zinc-600 dark:text-zinc-400">{{ turn.request_kind }}</span>
          </div>
          <p v-if="turn.excerpt" class="mt-1 whitespace-pre-wrap break-words text-xs leading-5 text-zinc-600 dark:text-zinc-400">{{ turn.excerpt }}</p>
        </div>
        <span v-if="turn.usage" class="font-mono text-[11px] text-zinc-600 sm:text-right dark:text-zinc-400">{{ compactNumber(turn.usage.input_tokens + turn.usage.output_tokens + (turn.usage.cache_read_tokens ?? 0) + (turn.usage.cache_write_tokens ?? 0)) }} tokens<span v-if="(turn.usage.cache_read_tokens ?? 0) > 0" class="block">{{ compactNumber(turn.usage.cache_read_tokens ?? 0) }} cached</span></span>
      </li>
    </ol>
  </details>
</template>
