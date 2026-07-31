<script setup lang="ts">
import BrandIcon from "./BrandIcon.vue";
withDefaults(defineProps<{ label: string; truncate?: boolean }>(), { truncate: true });
</script>

<template>
  <span class="inline-flex min-w-0 items-center gap-2 text-zinc-700 dark:text-zinc-300">
    <BrandIcon :label="label" />
    <span :class="truncate ? 'truncate' : 'break-all'" :title="label">{{ label }}</span>
  </span>
</template>
