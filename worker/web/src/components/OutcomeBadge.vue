<script setup lang="ts">
import { CircleCheck, CircleHelp, CircleOff, CircleX } from "lucide-vue-next";
import type { Outcome } from "@/lib/mock";
defineProps<{ outcome: Outcome }>();
const icons = { promoted: CircleCheck, discarded: CircleX, abandoned: CircleOff, unknown: CircleHelp };
</script>

<template>
  <span class="inline-flex h-6 items-center gap-1.5 rounded-[4px] border px-2 text-xs font-medium capitalize" :class="{
    'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300': outcome === 'promoted',
    'border-red-200 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-300': outcome === 'discarded',
    'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-300': outcome === 'abandoned',
    'border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-300': outcome === 'unknown'
  }"><component :is="icons[outcome]" class="size-3.5" />{{ outcome }}</span>
</template>
