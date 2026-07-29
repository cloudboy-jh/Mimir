<script setup lang="ts">
import { computed } from "vue";
import { ChevronDown, Database } from "lucide-vue-next";
import type { SessionDetail } from "@/lib/api";
import { shortDate } from "@/lib/format";

const props = defineProps<{ capture: SessionDetail["capture"] }>();
const captureTotal = computed(() => props.capture.saved_exchanges + props.capture.failed_exchanges + props.capture.pending_exchanges);
</script>

<template>
  <section aria-labelledby="capture-heading"><h2 id="capture-heading" class="flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><Database class="size-4" />Capture</h2><p class="mt-2 text-sm text-zinc-700 dark:text-zinc-300"><strong class="font-medium capitalize">{{ capture.status }}</strong> · {{ captureTotal }} {{ captureTotal === 1 ? "exchange" : "exchanges" }} in this session</p><details class="group mt-2 text-xs text-zinc-500 dark:text-zinc-400"><summary class="inline-flex cursor-pointer list-none items-center gap-1 font-medium text-teal-700 hover:text-teal-900 focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-teal-400 dark:hover:text-teal-300">Capture details<ChevronDown class="size-3.5 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none" /></summary><dl class="mt-3 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4"><div><dt>Saved</dt><dd class="mt-0.5 font-mono text-zinc-900 dark:text-zinc-100">{{ capture.saved_exchanges }}</dd></div><div><dt>Pending</dt><dd class="mt-0.5 font-mono text-zinc-900 dark:text-zinc-100">{{ capture.pending_exchanges }}</dd></div><div><dt>Failed</dt><dd class="mt-0.5 font-mono text-zinc-900 dark:text-zinc-100">{{ capture.failed_exchanges }}</dd></div><div><dt>Last saved</dt><dd class="mt-0.5 text-zinc-900 dark:text-zinc-100">{{ capture.last_saved_at ? shortDate(capture.last_saved_at) : "Never" }}</dd></div></dl></details></section>
</template>
