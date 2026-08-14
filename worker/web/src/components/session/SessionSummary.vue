<script setup lang="ts">
import type { SessionDetail } from "@/lib/api";
defineProps<{ session: SessionDetail["session"] }>();
</script>

<template>
  <section aria-labelledby="session-summary-heading">
    <h2 id="session-summary-heading" class="text-base font-semibold text-zinc-900 dark:text-zinc-100">Summary</h2>
    <div v-if="session.intent" class="mt-3 max-w-[72ch]">
      <p class="text-sm font-medium leading-6 text-zinc-900 dark:text-zinc-100">{{ session.intent }}</p>
    </div>
    <div class="mt-3 max-w-[72ch]">
      <p v-if="session.summary_status === 'ready' && session.summary_text" class="text-sm leading-6 text-zinc-600 dark:text-zinc-400">{{ session.summary_text }}</p>
      <p v-else-if="session.state === 'active' || session.summary_status === 'pending'" class="text-sm text-zinc-500 dark:text-zinc-400">Pending until the session finishes and its evidence is saved.</p>
      <p v-else class="text-sm text-zinc-500 dark:text-zinc-400">No summary could be reconstructed from the saved evidence.</p>
    </div>
  </section>
</template>
