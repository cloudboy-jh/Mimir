<script setup lang="ts">
import type { CaptureSummary } from "@/lib/api";

defineProps<{ capture: CaptureSummary }>();
</script>

<template>
  <span class="inline-flex flex-wrap items-baseline gap-x-1">
    <strong class="font-medium capitalize" :class="capture.status === 'failed' || capture.status === 'partial' ? 'text-red-700 dark:text-red-400' : capture.status === 'pending' ? 'text-amber-800 dark:text-amber-300' : 'text-zinc-800 dark:text-zinc-200'">{{ capture.status }}</strong>
    <span class="text-zinc-600 dark:text-zinc-400">· {{ capture.saved_exchanges }} saved<template v-if="capture.pending_exchanges"> · {{ capture.pending_exchanges }} pending</template><template v-if="capture.failed_exchanges"> · {{ capture.failed_exchanges }} failed</template></span>
  </span>
</template>
