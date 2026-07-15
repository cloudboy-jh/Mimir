<script setup lang="ts">
import BrandIcon from "./BrandIcon.vue";
defineProps<{ label: string }>();
</script>

<template>
  <span class="inline-flex min-w-0 items-center gap-2 text-zinc-700 dark:text-zinc-300">
    <BrandIcon :label="label" />
    <span class="truncate">{{ label }}</span>
  </span>
</template>
