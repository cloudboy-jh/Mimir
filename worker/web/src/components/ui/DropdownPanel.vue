<script setup lang="ts">
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from "reka-ui";

const open = defineModel<boolean>("open", { required: true });
withDefaults(defineProps<{ title: string; description?: string; align?: "start" | "center" | "end" }>(), { align: "end" });
</script>

<template>
  <PopoverRoot v-model:open="open">
    <PopoverTrigger as-child><slot name="trigger" /></PopoverTrigger>
    <PopoverPortal>
      <PopoverContent :align="align" :side-offset="8" :collision-padding="16" class="z-50 max-h-[min(42rem,calc(100vh-2rem))] w-[min(34rem,calc(100vw-2rem))] origin-(--reka-popover-content-transform-origin) overflow-y-auto rounded-[7px] border border-zinc-200 bg-white p-4 shadow-[0_18px_50px_rgba(0,0,0,0.18)] focus:outline-none data-[state=closed]:animate-popover-out data-[state=open]:animate-popover-in motion-reduce:animate-none dark:border-zinc-700 dark:bg-zinc-900">
        <div class="mb-4">
          <h2 class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">{{ title }}</h2>
          <p v-if="description" class="mt-1 text-xs leading-4 text-zinc-500 dark:text-zinc-400">{{ description }}</p>
        </div>
        <slot />
        <div v-if="$slots.footer" class="mt-4 flex items-center justify-end gap-2 border-t border-zinc-200 pt-3 dark:border-zinc-800"><slot name="footer" /></div>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>
