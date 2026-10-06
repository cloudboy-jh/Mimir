<script setup lang="ts">
import { computed } from "vue";
import IdentityBadge from "@/components/IdentityBadge.vue";
import type { SessionModel } from "@/lib/api";
import { harnessLabel } from "@/lib/format";

const props = withDefaults(defineProps<{ app: string | null; primary: string | null; models?: SessionModel[] }>(), { models: () => [] });

const orderedModels = computed<SessionModel[]>(() => {
  if (props.models.length) return props.models;
  return props.primary ? [{ name: props.primary, request_count: 0, first_seen_at: null, last_seen_at: null }] : [];
});
const primaryModel = computed(() => orderedModels.value[0] ?? null);
const secondaryModels = computed(() => orderedModels.value.slice(1));
const modelSummary = computed(() => secondaryModels.value.map((model) => model.name).join(", "));
</script>

<template>
  <div class="min-w-0 space-y-1.5">
    <IdentityBadge :label="harnessLabel(app)" />
    <div class="flex min-w-0 items-center gap-2">
      <IdentityBadge :label="primaryModel?.name || primary || 'Unknown model'" />
      <span v-if="secondaryModels.length" class="shrink-0 text-[11px] font-medium text-zinc-500 dark:text-zinc-400" :title="modelSummary">+{{ secondaryModels.length }} {{ secondaryModels.length === 1 ? "model" : "models" }}</span>
    </div>
  </div>
</template>
