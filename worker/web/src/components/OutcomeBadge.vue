<script setup lang="ts">
import { computed } from "vue";
import type { Outcome } from "@/lib/api";
import { outcomeMeta } from "@/lib/outcomes";

const props = defineProps<{ outcome: Outcome }>();
const meta = computed(() => outcomeMeta[props.outcome]);
</script>

<template>
  <span class="inline-flex h-6 items-center gap-1.5 rounded-[4px] border px-2 text-xs font-medium" :class="meta.badge"><component :is="meta.icon" class="size-3.5" />{{ meta.label }}</span>
</template>
