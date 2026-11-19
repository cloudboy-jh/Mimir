<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { X } from "lucide-vue-next";
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from "reka-ui";
import DiffViewer from "@/components/session/DiffViewer.vue";
import { errorMessage, getCommitPatch, type Commit, type Patch } from "@/lib/api";

const props = defineProps<{ commit: Commit | null }>();
const emit = defineEmits<{ close: [] }>();
const patch = ref<Patch | null>(null);
const loading = ref(false);
const error = ref("");
let controller: AbortController | null = null;

async function load(commit: Commit) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  patch.value = null;
  error.value = "";
  loading.value = true;
  try {
    patch.value = await getCommitPatch(commit.session_id, commit.commit_sha, active.signal);
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "The patch could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

watch(() => props.commit, (commit) => {
  if (commit) void load(commit);
  else controller?.abort();
}, { immediate: true });
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <DialogRoot :open="Boolean(commit)" @update:open="(open) => { if (!open) emit('close'); }">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-40 bg-zinc-950/40" />
      <DialogContent class="fixed inset-y-0 right-0 z-50 flex w-full max-w-6xl flex-col border-l border-zinc-200 bg-stone-50 shadow-xl focus:outline-none dark:border-zinc-800 dark:bg-stone-950">
        <div v-if="commit" class="flex shrink-0 items-start justify-between gap-4 border-b border-zinc-200 px-5 py-4 sm:px-6 dark:border-zinc-800">
          <div class="min-w-0">
            <DialogTitle class="break-words text-lg font-semibold">{{ commit.subject || "Untitled commit" }}</DialogTitle>
            <DialogDescription class="mt-1 flex flex-wrap gap-x-3 text-xs text-zinc-600 dark:text-zinc-400">
              <code class="font-mono">{{ commit.commit_sha }}</code>
              <span v-if="commit.ref">{{ commit.ref }}</span>
              <span>{{ commit.patch_files }} {{ commit.patch_files === 1 ? "file" : "files" }} · +{{ commit.patch_additions }} −{{ commit.patch_deletions }}</span>
            </DialogDescription>
          </div>
          <DialogClose as-child><button type="button" class="grid size-10 shrink-0 place-items-center rounded-[5px] text-zinc-600 hover:bg-stone-200 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:bg-zinc-900" aria-label="Close patch"><X class="size-5" aria-hidden="true" /></button></DialogClose>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-5 pb-6 sm:px-6">
          <div v-if="loading" aria-busy="true" aria-label="Loading patch" class="mt-6 h-40 animate-pulse bg-zinc-100 motion-reduce:animate-none dark:bg-zinc-900" />
          <p v-else-if="error" role="alert" class="mt-6 text-sm text-red-700 dark:text-red-400">{{ error }}</p>
          <template v-else-if="patch">
            <p v-if="patch.truncated" class="mt-4 text-xs text-amber-800 dark:text-amber-300">This patch was truncated for display.</p>
            <DiffViewer :patch="patch.text" />
          </template>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
