<script setup lang="ts">
import { computed } from "vue";
import { cn } from "@/lib/utils";

const props = withDefaults(defineProps<{ variant?: "default" | "outline" | "ghost"; size?: "default" | "icon"; class?: string; type?: "button" | "submit" }>(), { variant: "default", size: "default", type: "button" });
const classes = computed(() => cn(
  "inline-flex h-8.5 items-center justify-center gap-2 rounded-[5px] border text-[13px] font-medium transition-colors duration-150 ease-out focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:pointer-events-none disabled:opacity-45 dark:focus-visible:outline-teal-400",
  props.variant === "default" && "border-zinc-900 bg-zinc-900 px-3 text-white hover:bg-zinc-700 dark:border-zinc-100 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-300",
  props.variant === "outline" && "border-zinc-300 bg-white px-3 text-zinc-800 hover:border-zinc-400 hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:border-zinc-600 dark:hover:bg-zinc-800",
  props.variant === "ghost" && "border-transparent bg-transparent px-2.5 text-zinc-600 hover:bg-zinc-100 hover:text-zinc-950 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100",
  props.size === "icon" && "size-8.5 p-0",
  props.class,
));
</script>

<template><button :type="type" :class="classes"><slot /></button></template>
