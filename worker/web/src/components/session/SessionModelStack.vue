<script setup lang="ts">
import { computed } from "vue";
import IdentityBadge from "@/components/IdentityBadge.vue";
import type { SessionModel } from "@/lib/api";

const props = withDefaults(defineProps<{ app: string | null; primary: string | null; models?: SessionModel[]; mode?: "compact" | "tree" }>(), { models: () => [], mode: "compact" });

const orderedModels = computed<SessionModel[]>(() => {
  if (props.models.length) return props.models;
  return props.primary ? [{ name: props.primary, request_count: 0, first_seen_at: null, last_seen_at: null }] : [];
});
const primaryModel = computed(() => orderedModels.value[0] ?? null);
const secondaryModels = computed(() => orderedModels.value.slice(1));
const modelSummary = computed(() => secondaryModels.value.map((model) => model.name).join(", "));
</script>

<template>
  <div v-if="mode === 'compact'" class="min-w-0 space-y-1.5">
    <IdentityBadge :label="app || 'Unknown app'" />
    <div class="flex min-w-0 items-center gap-2">
      <IdentityBadge :label="primaryModel?.name || primary || 'Unknown model'" />
      <span v-if="secondaryModels.length" class="shrink-0 text-[11px] font-medium text-zinc-500 dark:text-zinc-400" :title="modelSummary">+{{ secondaryModels.length }} {{ secondaryModels.length === 1 ? "model" : "models" }}</span>
    </div>
  </div>

  <div v-else class="min-w-0 text-[13px]">
    <IdentityBadge :label="app || 'Unknown app'" :truncate="false" />
    <ul class="ml-2.5 mt-2 border-l border-zinc-300 dark:border-zinc-700">
      <li v-for="model in orderedModels" :key="model.name" class="relative grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-start gap-3 py-1.5 pl-5 before:absolute before:left-0 before:top-3.5 before:w-3 before:border-t before:border-zinc-300 dark:before:border-zinc-700">
        <IdentityBadge :label="model.name" :truncate="false" />
        <span v-if="model.request_count" class="shrink-0 pt-0.5 font-mono text-[11px] text-zinc-500">{{ model.request_count }} {{ model.request_count === 1 ? "request" : "requests" }}</span>
      </li>
      <li v-if="!orderedModels.length" class="relative py-1.5 pl-5 before:absolute before:left-0 before:top-1/2 before:w-3 before:border-t before:border-zinc-300 dark:before:border-zinc-700"><IdentityBadge label="Unknown model" :truncate="false" /></li>
    </ul>
  </div>
</template>
