<script setup lang="ts">
import type { SessionLiveness } from "@/lib/api";

defineProps<{ liveness: SessionLiveness; announce?: boolean }>();

const labels: Record<SessionLiveness, string> = {
  active: "Active",
  disconnected: "Disconnected",
  finalized: "Finalized",
};
</script>

<template>
  <span class="inline-flex items-center gap-1.5 text-xs font-medium" :class="{
    'text-emerald-700 dark:text-emerald-400': liveness === 'active',
    'text-amber-700 dark:text-amber-400': liveness === 'disconnected',
    'text-zinc-500 dark:text-zinc-400': liveness === 'finalized',
  }" :role="announce ? 'status' : undefined">
    <span class="size-1.5 shrink-0 rounded-full" :class="{
      'bg-emerald-500': liveness === 'active',
      'bg-amber-500': liveness === 'disconnected',
      'border border-zinc-400 bg-transparent dark:border-zinc-500': liveness === 'finalized',
    }" aria-hidden="true" />
    {{ labels[liveness] }}
  </span>
</template>
