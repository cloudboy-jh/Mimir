<script setup lang="ts">
import { X } from "lucide-vue-next";
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from "reka-ui";
import type { SessionDetail } from "@/lib/api";
import SessionSummary from "./SessionSummary.vue";

const props = defineProps<{ detail: SessionDetail; returnFocus?: HTMLElement }>();
const open = defineModel<boolean>({ required: true });
defineEmits<{ navigate: [hash: string] }>();
function restoreFocus(event: Event) {
  event.preventDefault();
  if (props.returnFocus?.isConnected) props.returnFocus.focus();
  else document.getElementById("session-tab-conversation")?.focus();
}
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-zinc-950/40" />
      <DialogContent class="fixed inset-y-0 right-0 z-50 flex w-full max-w-xl flex-col border-l border-zinc-200 bg-stone-50 shadow-xl focus:outline-none dark:border-zinc-800 dark:bg-stone-950" @close-auto-focus="restoreFocus">
        <div class="flex shrink-0 items-center justify-between gap-4 border-b border-zinc-200 px-5 py-4 sm:px-6 dark:border-zinc-800">
          <DialogTitle class="text-lg font-semibold">Summary</DialogTitle>
          <DialogDescription class="sr-only">Reconstructed session summary and its recorded source evidence.</DialogDescription>
          <DialogClose as-child><button type="button" class="grid size-10 shrink-0 place-items-center rounded-[5px] text-zinc-600 hover:bg-stone-200 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:bg-zinc-900" aria-label="Close session summary"><X class="size-5" aria-hidden="true" /></button></DialogClose>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 py-6 sm:px-6">
          <SessionSummary :session="detail.session" :summary="detail.summary" full @navigate="$emit('navigate', $event)" />
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
