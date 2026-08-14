<script setup lang="ts">
import type { SessionDetail } from "@/lib/api";
defineProps<{ session: SessionDetail["session"] }>();
</script>

<template>
  <section aria-labelledby="session-summary-heading" class="border-b border-zinc-200 py-6 dark:border-zinc-800">
    <h2 id="session-summary-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Summary</h2>
    <p v-if="session.summary_status === 'ready' && session.summary_text" class="mt-3 max-w-[75ch] text-sm leading-6 text-zinc-700 dark:text-zinc-300">{{ session.summary_text }}</p>
    <p v-else-if="session.state === 'active' || session.summary_status === 'pending'" class="mt-3 text-sm text-zinc-500 dark:text-zinc-400">A final summary will be generated when this session ends.</p>
    <p v-else class="mt-3 text-sm text-zinc-500 dark:text-zinc-400">A summary could not be generated from the recorded evidence.</p>
  </section>
</template>
