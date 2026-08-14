<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, RotateCw } from "lucide-vue-next";
import { errorMessage, getSession, getSessionDiff, type SessionDetail } from "@/lib/api";
import { parsePatch } from "@/lib/diff";
import { displayTitle } from "@/lib/sessions";

const route = useRoute();
const detail = ref<SessionDetail | null>(null);
const patch = ref("");
const loading = ref(true);
const error = ref("");
const activeIndex = ref(0);
let controller: AbortController | null = null;

const files = computed(() => parsePatch(patch.value));
const totals = computed(() => ({ added: files.value.reduce((sum, file) => sum + file.added, 0), removed: files.value.reduce((sum, file) => sum + file.removed, 0) }));
const anchor = (index: number) => `file-${index}`;

function jumpToFile(event: Event) {
  const index = Number((event.target as HTMLSelectElement).value);
  activeIndex.value = index;
  document.getElementById(anchor(index))?.scrollIntoView({ block: "start" });
}

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
  <section v-if="detail && files.length" class="mx-auto max-w-[1360px] pb-12">
    <RouterLink :to="`/sessions/${detail.session.id}`" class="mb-6 inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-500 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" />Session</RouterLink>
    <header class="border-b border-zinc-200 pb-5 dark:border-zinc-800">
      <div class="flex flex-wrap items-end justify-between gap-4"><div><h1 class="text-[28px] font-semibold tracking-[-0.025em]">Full diff</h1><p class="mt-1 text-sm text-zinc-500 dark:text-zinc-400">{{ displayTitle(detail.session) }}</p></div><p class="font-mono text-xs text-zinc-500">{{ files.length }} {{ files.length === 1 ? "file" : "files" }} <span class="ml-2 text-emerald-700 dark:text-emerald-400">+{{ totals.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ totals.removed }}</span></p></div>
      <p class="mt-3 break-all font-mono text-xs text-zinc-500">{{ detail.session.id }}</p>
    </header>
    <div class="pt-6 lg:hidden">
      <label for="diff-file" class="mb-1.5 block text-xs font-medium text-zinc-500">Changed file</label>
      <select id="diff-file" class="h-10 w-full rounded-[5px] border border-zinc-300 bg-white px-3 font-mono text-xs focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-stone-900" :value="activeIndex" @change="jumpToFile"><option v-for="(file, index) in files" :key="file.file" :value="index">{{ file.file }} (+{{ file.added }} −{{ file.removed }})</option></select>
    </div>
    <div class="grid items-start gap-10 pt-6 lg:grid-cols-[300px_minmax(0,1fr)]">
      <nav aria-label="Changed files" class="hidden max-h-[calc(100vh-9rem)] overflow-auto border-y border-zinc-200 lg:sticky lg:top-6 lg:block dark:border-zinc-800">
        <a v-for="(file, index) in files" :key="file.file" :href="`#${anchor(index)}`" :aria-current="activeIndex === index ? 'location' : undefined" class="grid grid-cols-[8px_minmax(0,1fr)_auto] items-baseline gap-2 border-b border-zinc-200 py-3 pr-2 text-xs last:border-0 hover:bg-stone-200/60 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-teal-600 dark:border-zinc-800 dark:hover:bg-stone-900 dark:hover:text-zinc-100" @click="activeIndex = index"><span class="mt-1 size-1.5" :class="activeIndex === index ? 'bg-teal-600' : ''" aria-hidden="true" /><span class="truncate font-mono" :title="file.file">{{ file.file }}</span><span class="shrink-0 font-mono text-xs"><span class="text-emerald-700 dark:text-emerald-400">+{{ file.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ file.removed }}</span></span></a>
      </nav>
      <div class="min-w-0 space-y-8">
        <section v-for="(file, fileIndex) in files" :id="anchor(fileIndex)" :key="file.file" class="scroll-mt-6" :aria-labelledby="`${anchor(fileIndex)}-heading`">
          <div class="flex items-baseline justify-between gap-3 border-b border-zinc-300 pb-2 dark:border-zinc-700"><h2 :id="`${anchor(fileIndex)}-heading`" class="break-all font-mono text-[13px] font-medium">{{ file.file }}</h2><span class="shrink-0 font-mono text-xs"><span class="text-emerald-700 dark:text-emerald-400">+{{ file.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ file.removed }}</span></span></div>
          <div class="overflow-x-auto border-b border-zinc-200 dark:border-zinc-800" tabindex="0">
            <div class="min-w-max py-1 font-mono text-xs leading-6">
              <div v-for="(line, lineIndex) in file.lines" :key="lineIndex" class="grid grid-cols-[48px_48px_minmax(0,1fr)]" :class="line.type === 'add' ? 'bg-emerald-50 text-emerald-950 dark:bg-emerald-950/25 dark:text-emerald-200' : line.type === 'del' ? 'bg-red-50 text-red-950 dark:bg-red-950/25 dark:text-red-200' : line.type === 'meta' ? 'bg-stone-200/50 text-zinc-500 dark:bg-stone-900 dark:text-zinc-400' : 'text-zinc-700 dark:text-zinc-300'">
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
