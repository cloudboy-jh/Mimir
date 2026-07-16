<script setup lang="ts">
import { CircleCheck, CircleDashed, CircleOff, CircleX } from "lucide-vue-next";
import type { Outcome } from "@/lib/mock";
defineProps<{ outcome: Outcome }>();
const outcomes = {
  landed: { icon: CircleCheck, label: "Landed" },
  discarded: { icon: CircleX, label: "Discarded" },
  abandoned: { icon: CircleOff, label: "Abandoned" },
  unresolved: { icon: CircleDashed, label: "Unresolved" }
};
</script>

<template>
  <span class="inline-flex h-6 items-center gap-1.5 rounded-[4px] border px-2 text-xs font-medium" :class="{
    'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300': outcome === 'landed',
    'border-red-200 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-300': outcome === 'discarded',
    'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-300': outcome === 'abandoned',
    'border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-300': outcome === 'unresolved'
  }"><component :is="outcomes[outcome].icon" class="size-3.5" />{{ outcomes[outcome].label }}</span>
</template>
