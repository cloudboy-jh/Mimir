<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, RotateCw } from "lucide-vue-next";
import { errorMessage, getSession, getSessionDiff, type SessionDetail } from "@/lib/api";
import { parsePatch } from "@/lib/diff";

const route = useRoute();
const detail = ref<SessionDetail | null>(null);
const patch = ref("");
const loading = ref(true);
const error = ref("");
let controller: AbortController | null = null;

const files = computed(() => parsePatch(patch.value));
const totals = computed(() => ({ added: files.value.reduce((sum, file) => sum + file.added, 0), removed: files.value.reduce((sum, file) => sum + file.removed, 0) }));
const anchor = (index: number) => `file-${index}`;

async function load() {
  controller?.abort();
  controller = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const id = String(route.params.id);
    const [session, diff] = await Promise.all([getSession(id, controller.signal), getSessionDiff(id, controller.signal)]);
    detail.value = session;
    patch.value = diff;
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorMessage(cause, "The complete diff could not be loaded.");
  } finally {
    if (!controller.signal.aborted) loading.value = false;
  }
}

watch(() => String(route.params.id), load, { immediate: true });
</script>

<template>
  <section v-if="detail && files.length">
    <RouterLink :to="`/sessions/${detail.session.id}`" class="mb-6 inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-500 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" />Session</RouterLink>
    <header class="border-b border-zinc-200 pb-5 dark:border-zinc-800">
      <p class="font-mono text-xs text-zinc-500">{{ detail.session.id }}</p>
      <div class="mt-2 flex flex-wrap items-end justify-between gap-3"><h1 class="text-[28px] font-semibold tracking-[-0.025em]">Full diff</h1><p class="font-mono text-xs text-zinc-500">{{ files.length }} {{ files.length === 1 ? "file" : "files" }} <span class="ml-2 text-emerald-700 dark:text-emerald-400">+{{ totals.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ totals.removed }}</span></p></div>
    </header>
    <div class="grid items-start gap-8 pt-6 lg:grid-cols-[240px_minmax(0,1fr)]">
      <nav aria-label="Changed files" class="max-h-[calc(100vh-9rem)] overflow-auto border-y border-zinc-200 lg:sticky lg:top-6 dark:border-zinc-800">
        <a v-for="(file, index) in files" :key="file.file" :href="`#${anchor(index)}`" class="flex items-baseline justify-between gap-3 border-b border-zinc-200 py-2.5 text-xs last:border-0 hover:text-teal-700 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-teal-600 dark:border-zinc-800 dark:hover:text-teal-400"><span class="truncate font-mono" :title="file.file">{{ file.file }}</span><span class="shrink-0 font-mono text-[10px]"><span class="text-emerald-700 dark:text-emerald-400">+{{ file.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ file.removed }}</span></span></a>
      </nav>
      <div class="min-w-0 space-y-8">
        <section v-for="(file, fileIndex) in files" :id="anchor(fileIndex)" :key="file.file" class="scroll-mt-6" :aria-labelledby="`${anchor(fileIndex)}-heading`">
          <div class="flex items-baseline justify-between gap-3 border-b border-zinc-300 pb-2 dark:border-zinc-700"><h2 :id="`${anchor(fileIndex)}-heading`" class="break-all font-mono text-xs font-medium">{{ file.file }}</h2><span class="shrink-0 font-mono text-[11px]"><span class="text-emerald-700 dark:text-emerald-400">+{{ file.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ file.removed }}</span></span></div>
          <div class="overflow-x-auto border-b border-zinc-200 dark:border-zinc-800" tabindex="0">
            <div class="min-w-max py-1 font-mono text-[11px] leading-5">
              <div v-for="(line, lineIndex) in file.lines" :key="lineIndex" class="grid grid-cols-[44px_44px_minmax(0,1fr)]" :class="line.type === 'add' ? 'bg-emerald-50 text-emerald-950 dark:bg-emerald-950/50 dark:text-emerald-200' : line.type === 'del' ? 'bg-red-50 text-red-950 dark:bg-red-950/50 dark:text-red-200' : line.type === 'meta' ? 'bg-stone-50 text-zinc-500 dark:bg-zinc-900 dark:text-zinc-400' : 'text-zinc-700 dark:text-zinc-300'">
                <span class="select-none border-r border-zinc-200 px-2 text-right text-zinc-400 dark:border-zinc-800 dark:text-zinc-600">{{ line.oldLine ?? "" }}</span><span class="select-none border-r border-zinc-200 px-2 text-right text-zinc-400 dark:border-zinc-800 dark:text-zinc-600">{{ line.newLine ?? "" }}</span><span class="whitespace-pre px-3">{{ line.text || " " }}</span>
              </div>
            </div>
          </div>
        </section>
      </div>
    </div>
  </section>
  <section v-else-if="loading" aria-busy="true" class="py-16"><div class="h-4 w-28 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-5 h-9 w-48 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="mt-8 h-96 animate-pulse bg-zinc-100 motion-reduce:animate-none dark:bg-zinc-900" /></section>
  <section v-else class="py-20 text-center"><h1 class="text-xl font-semibold">Diff unavailable</h1><p class="mx-auto mt-2 max-w-md text-sm text-zinc-500 dark:text-zinc-400">{{ error || "No complete patch was recorded for this session." }}</p><div class="mt-4 flex justify-center gap-4"><button class="inline-flex items-center gap-2 text-sm font-medium text-teal-700 dark:text-teal-400" @click="load"><RotateCw class="size-4" />Retry</button><RouterLink :to="`/sessions/${String(route.params.id)}`" class="text-sm font-medium text-teal-700 dark:text-teal-400">Return to session</RouterLink></div></section>
</template>
