<script setup lang="ts">
import { TooltipArrow, TooltipContent, TooltipPortal, TooltipProvider, TooltipRoot, TooltipTrigger } from "reka-ui";
defineProps<{ label: string }>();
</script>

<template>
  <TooltipProvider :delay-duration="300">
    <TooltipRoot>
      <TooltipTrigger as-child><slot /></TooltipTrigger>
      <TooltipPortal><TooltipContent side="bottom" :side-offset="7" class="z-50 rounded-[4px] bg-zinc-900 px-2 py-1 text-xs text-white shadow-lg dark:bg-zinc-100 dark:text-zinc-950"><TooltipArrow class="fill-zinc-900 dark:fill-zinc-100" />{{ label }}</TooltipContent></TooltipPortal>
    </TooltipRoot>
  </TooltipProvider>
</template>
