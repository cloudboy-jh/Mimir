<script setup lang="ts">
import { computed } from "vue";
import type { SessionSummary } from "@/lib/api";
import { shortDate } from "@/lib/format";

const props = defineProps<{ summary: SessionSummary | null; active: boolean }>();
const sections = computed(() => {
  const summary = props.summary;
  if (summary?.status !== "ready") return [];
  return [
    { key: "goal", title: "Goal", items: summary.goal ? [summary.goal] : [] },
    { key: "actions", title: "Actions", items: summary.actions },
    { key: "result", title: "Result", items: summary.result ? [summary.result] : [] },
    { key: "verification", title: "Verification", items: summary.verification },
    { key: "unresolved", title: "Unresolved", items: summary.unresolved },
  ].filter((section) => section.items.length);
});
</script>

<template>
  <section aria-labelledby="session-summary-heading" class="border-b border-zinc-200 py-4 dark:border-zinc-800">
    <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <h2 id="session-summary-heading" class="text-sm font-semibold">Summary</h2>
      <span v-if="summary?.partial" class="text-xs text-zinc-600 dark:text-zinc-400">Built from incomplete evidence</span>
      <span v-if="summary?.updated_at" class="text-xs text-zinc-600 dark:text-zinc-400">Updated {{ shortDate(summary.updated_at) }}</span>
    </div>
    <dl v-if="sections.length" class="mt-3 grid gap-x-8 gap-y-4 text-sm leading-6 md:grid-cols-2">
      <div v-for="section in sections" :key="section.key" class="min-w-0" :class="section.key === 'actions' ? 'md:row-span-2' : ''">
        <dt class="text-xs font-medium text-zinc-600 dark:text-zinc-400">{{ section.title }}</dt>
        <dd class="mt-1 text-zinc-800 dark:text-zinc-200">
          <p v-if="section.items.length === 1" class="max-w-[72ch] break-words">{{ section.items[0] }}</p>
          <ul v-else class="max-w-[72ch] list-disc space-y-1 pl-5"><li v-for="(item, index) in section.items" :key="index" class="break-words">{{ item }}</li></ul>
        </dd>
      </div>
    </dl>
    <p v-else class="mt-2 text-sm text-zinc-600 dark:text-zinc-400">{{ summary?.status === "ready" ? "The summary found nothing to report." : summary?.status === "unavailable" ? "No summary could be built from the saved evidence." : active ? "The summary is built when the session ends." : "The summary is being built." }}</p>
  </section>
</template>
