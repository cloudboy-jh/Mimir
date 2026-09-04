<script setup lang="ts">
import IdentityBadge from "@/components/IdentityBadge.vue";
import type { LiveSessionTurn, SessionLiveness } from "@/lib/api";
import { compactNumber, shortDate } from "@/lib/format";

defineProps<{ turns: LiveSessionTurn[]; liveness: SessionLiveness }>();
</script>

<template>
  <section v-if="turns.length || liveness !== 'finalized'" aria-labelledby="live-turns-heading">
    <div class="mb-3 flex flex-wrap items-end justify-between gap-3">
      <div>
        <h2 id="live-turns-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Incoming activity</h2>
        <p class="mt-1 max-w-2xl text-xs text-zinc-500">Temporary harness events. Saved requests move into the canonical activity record below.</p>
      </div>
      <span class="text-xs text-zinc-500" aria-live="polite">{{ turns.length }} {{ turns.length === 1 ? 'event' : 'events' }}</span>
    </div>
    <ol class="border-t border-zinc-200 dark:border-zinc-800">
      <li v-for="(turn, index) in turns" :key="turn.exchange_id || `${turn.ts}-${index}`" class="grid gap-2 border-b border-zinc-200 py-3 sm:grid-cols-[120px_minmax(0,1fr)_auto] sm:px-3 dark:border-zinc-800">
        <time class="font-mono text-xs text-zinc-500" :datetime="turn.ts">{{ shortDate(turn.ts) }}</time>
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <IdentityBadge v-if="turn.provider" :label="turn.provider" />
            <IdentityBadge :label="turn.model || 'Unknown model'" />
            <span v-if="turn.request_kind && turn.request_kind !== 'primary'" class="text-[11px] text-zinc-500">{{ turn.request_kind }}</span>
          </div>
          <p v-if="turn.excerpt" class="mt-1.5 line-clamp-2 text-xs leading-5 text-zinc-600 dark:text-zinc-400">{{ turn.excerpt }}</p>
        </div>
        <span v-if="turn.usage" class="font-mono text-xs text-zinc-500 sm:text-right">{{ compactNumber(turn.usage.input_tokens + turn.usage.output_tokens + (turn.usage.cache_read_tokens ?? 0) + (turn.usage.cache_write_tokens ?? 0)) }} tokens<span v-if="(turn.usage.cache_read_tokens ?? 0) > 0" class="block text-[10px]">{{ compactNumber(turn.usage.cache_read_tokens ?? 0) }} cached</span></span>
      </li>
    </ol>
  </section>
</template>
