<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";
import type { SessionDetail } from "@/lib/api";

const props = withDefaults(defineProps<{ session: SessionDetail["session"]; summary?: SessionDetail["summary"]; full?: boolean }>(), { full: false });
defineEmits<{ open: []; navigate: [hash: string] }>();
// The outcome reason is authored evidence. A status-only reconstruction is not
// a useful narrative and belongs in the evidence panel, not above the reader.
const preview = computed(() => props.session.outcome_reason?.trim() || "");
const sections = computed(() => {
  const summary = props.summary;
  if (!summary) return [];
  return [
    { key: "goal", title: "Goal", items: summary.goal ? [summary.goal] : [] },
    { key: "actions", title: "Recorded actions", items: summary.actions },
    { key: "result", title: "Recorded result", items: summary.result ? [summary.result] : [] },
    { key: "verification", title: "Observed verification", items: summary.verification },
    { key: "unresolved", title: "Unresolved evidence", items: summary.unresolved },
  ].filter(section => section.items.length);
});
</script>

<template>
  <section v-if="!full && preview" aria-label="Session summary" class="flex flex-wrap items-baseline gap-x-3 gap-y-1 py-4">
    <p class="max-w-[72ch] line-clamp-2 break-words text-sm leading-6 text-zinc-700 dark:text-zinc-300">{{ preview }}</p>
    <button type="button" class="shrink-0 rounded-[3px] text-xs font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="$emit('open')">Summary evidence</button>
  </section>
  <section v-else-if="full" aria-labelledby="session-summary-heading" class="min-w-0">
    <div class="flex flex-wrap items-baseline gap-3">
      <h2 id="session-summary-heading" class="text-base font-semibold">Summary evidence</h2>
      <span v-if="summary?.partial" class="text-xs text-zinc-600 dark:text-zinc-400">Incomplete evidence</span>
    </div>
    <p class="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-400">From recorded outcomes and bounded saved evidence. Observed verification does not establish capture status.</p>
    <dl v-if="sections.length" class="mt-6 space-y-6">
      <div v-for="section in sections" :key="section.key">
        <dt class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">{{ section.title }}</dt>
        <dd class="mt-2 space-y-2 text-sm leading-6 text-zinc-700 dark:text-zinc-300">
          <p v-for="(item, index) in section.items" :key="index" class="break-words">
            {{ item }}
            <template v-for="(link, linkIndex) in summary?.evidence?.filter(evidence => evidence.section === section.key && evidence.index === index) ?? []" :key="linkIndex">
              <a v-if="link.href.startsWith('#')" :href="link.href" class="ml-1 text-xs text-teal-700 underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click.prevent="$emit('navigate', link.href)">{{ link.label }}</a>
              <RouterLink v-else :to="link.href" class="ml-1 text-xs text-teal-700 underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ link.label }}</RouterLink>
            </template>
          </p>
        </dd>
      </div>
    </dl>
    <p v-else class="mt-6 text-sm text-zinc-600 dark:text-zinc-400">No summary evidence is available for this session.</p>
  </section>
</template>
