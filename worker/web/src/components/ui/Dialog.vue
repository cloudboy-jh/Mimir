<script setup lang="ts">
import { X } from "lucide-vue-next";
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from "reka-ui";

const open = defineModel<boolean>("open", { required: true });
defineProps<{ title: string; description?: string }>();
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-zinc-950/45 data-[state=open]:animate-overlay-in motion-reduce:animate-none" />
      <DialogContent class="fixed left-1/2 top-1/2 z-50 max-h-[85vh] w-[min(34rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-[7px] border border-zinc-200 bg-white p-5 shadow-[0_18px_50px_rgba(0,0,0,0.18)] focus:outline-none data-[state=open]:animate-panel-in motion-reduce:animate-none dark:border-zinc-700 dark:bg-zinc-900">
        <div class="mb-4 flex items-start justify-between gap-6">
          <div>
            <DialogTitle class="text-base font-semibold text-zinc-950 dark:text-zinc-50">{{ title }}</DialogTitle>
            <DialogDescription v-if="description" class="mt-1 text-[13px] leading-5 text-zinc-500 dark:text-zinc-400">{{ description }}</DialogDescription>
          </div>
          <DialogClose class="grid size-7 shrink-0 place-items-center rounded-[4px] text-zinc-500 transition-colors duration-150 ease-out hover:bg-stone-100 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:hover:bg-zinc-800 dark:hover:text-zinc-50" aria-label="Close">
            <X class="size-4" />
          </DialogClose>
        </div>
        <slot />
        <div v-if="$slots.footer" class="mt-5 flex items-center justify-end gap-2 border-t border-zinc-200 pt-4 dark:border-zinc-800"><slot name="footer" /></div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
