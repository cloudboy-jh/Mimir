<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef } from "vue";
import { Check, ChevronDown, GitBranch, Pencil, X } from "lucide-vue-next";
import SessionModelStack from "@/components/session/SessionModelStack.vue";
import DeviceIdentity from "@/components/DeviceIdentity.vue";
import SessionLivenessBadge from "@/components/session/SessionLivenessBadge.vue";
import { errorMessage, setSessionTitle, type SessionDetail, type SessionLiveness, type SessionTitleUpdate } from "@/lib/api";
import { compactNumber, duration, shortDate } from "@/lib/format";
import { displayTitle } from "@/lib/sessions";

const MAX_TITLE_LENGTH = 200;
const props = defineProps<{ session: SessionDetail["session"]; capture: SessionDetail["capture"]; liveness: SessionLiveness }>();
const emit = defineEmits<{ saved: [update: SessionTitleUpdate] }>();
const editing = ref(false);
const saving = ref(false);
const editError = ref("");
const draftTitle = ref("");
const titleInput = useTemplateRef<HTMLInputElement>("titleInput");
const editButton = useTemplateRef<HTMLButtonElement>("editButton");
const captureTotal = computed(() => props.capture.saved_exchanges + props.capture.failed_exchanges + props.capture.pending_exchanges);

async function startEditing() {
  draftTitle.value = displayTitle(props.session);
  editError.value = "";
  editing.value = true;
  await nextTick();
  titleInput.value?.select();
}

async function cancelEditing() {
  if (saving.value) return;
  editing.value = false;
  editError.value = "";
  await nextTick();
  editButton.value?.focus();
}

async function saveTitle() {
  if (saving.value) return;
  const title = draftTitle.value.trim();
  if (!title) {
    editError.value = "Enter a session title.";
    titleInput.value?.focus();
    return;
  }
  saving.value = true;
  editError.value = "";
  try {
    const update = await setSessionTitle(props.session.id, title);
    emit("saved", update);
    editing.value = false;
    await nextTick();
    editButton.value?.focus();
  } catch (cause) {
    editError.value = errorMessage(cause, "The session title could not be saved.");
    await nextTick();
    titleInput.value?.focus();
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="border-b border-zinc-200 pb-6 dark:border-zinc-800">
    <div class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_360px] lg:items-start">
      <div class="min-w-0 max-w-4xl flex-1">
        <form v-if="editing" id="session-title-editor" class="max-w-3xl" @submit.prevent="saveTitle" @keydown.esc.prevent="cancelEditing">
          <label for="session-title" class="sr-only">Session title</label>
          <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
            <input id="session-title" ref="titleInput" v-model="draftTitle" type="text" :maxlength="MAX_TITLE_LENGTH" :disabled="saving" :aria-invalid="Boolean(editError)" :aria-describedby="editError ? 'session-title-error' : undefined" class="h-10 min-w-0 flex-1 rounded-[5px] border border-zinc-300 bg-white px-3 text-lg font-semibold tracking-[-0.025em] text-zinc-950 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 disabled:cursor-wait disabled:opacity-60 sm:text-xl dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-50" />
            <div class="flex shrink-0 items-center gap-2">
              <button type="submit" :disabled="saving" class="inline-flex h-8.5 items-center gap-1.5 rounded-[5px] bg-zinc-900 px-2.5 text-xs font-medium text-white hover:bg-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-wait disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-300"><Check class="size-3.5" aria-hidden="true" />{{ saving ? "Saving..." : "Save" }}</button>
              <button type="button" :disabled="saving" class="inline-flex h-8.5 items-center gap-1.5 rounded-[5px] border border-zinc-300 px-2.5 text-xs font-medium text-zinc-700 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:opacity-60 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800" @click="cancelEditing"><X class="size-3.5" aria-hidden="true" />Cancel</button>
            </div>
          </div>
          <p id="session-title-error" class="mt-2 min-h-4 text-xs text-red-700 dark:text-red-400" role="alert">{{ editError }}</p>
        </form>
        <div v-else class="flex min-w-0 items-start gap-2">
          <h1 class="min-w-0 break-words text-2xl font-semibold leading-tight tracking-[-0.025em] text-zinc-950 sm:text-[28px] dark:text-zinc-50">{{ displayTitle(session) }}</h1>
          <button ref="editButton" type="button" class="mt-0.5 inline-flex size-8 shrink-0 items-center justify-center rounded-[5px] text-zinc-500 hover:bg-stone-100 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100" aria-label="Edit session title" aria-controls="session-title-editor" :aria-expanded="editing" @click="startEditing"><Pencil class="size-3.5" aria-hidden="true" /></button>
        </div>
        <div class="mt-4 flex flex-wrap items-center gap-x-4 gap-y-2 text-[13px] text-zinc-500 dark:text-zinc-400"><SessionLivenessBadge :liveness="liveness" announce /><strong class="font-medium text-zinc-800 dark:text-zinc-200">{{ session.repo || "No repository" }}</strong><DeviceIdentity v-if="session.device" :device="session.device" /><span v-if="session.source_ref" class="inline-flex min-w-0 items-center gap-1"><GitBranch class="size-3.5 shrink-0" aria-hidden="true" /><span class="break-all">{{ session.source_ref }}</span></span><RouterLink v-if="session.parent_session_id" :to="`/sessions/${session.parent_session_id}`" class="font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">Parent session</RouterLink><span>{{ shortDate(session.started_at) }}</span><span class="break-all font-mono text-xs">{{ session.id }}</span></div>
        <dl class="mt-5 flex flex-wrap gap-x-8 gap-y-3 border-t border-zinc-200 pt-3 dark:border-zinc-800">
          <div><dt class="text-xs text-zinc-500">Duration</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ duration(session.started_at, session.ended_at) }}</dd></div>
          <div><dt class="text-xs text-zinc-500">Requests</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ session.request_count }}</dd></div>
          <div><dt class="text-xs text-zinc-500">Input</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_in) }}</dd></div>
          <div><dt class="text-xs text-zinc-500">Output</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_out) }}</dd></div>
          <div v-if="(session.cache_read_tokens ?? 0) > 0"><dt class="text-xs text-zinc-500">Cache read</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.cache_read_tokens ?? 0) }}</dd></div>
          <div v-if="(session.cache_write_tokens ?? 0) > 0"><dt class="text-xs text-zinc-500">Cache write</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.cache_write_tokens ?? 0) }}</dd></div>
          <div class="relative"><dt class="text-xs text-zinc-500">Capture</dt><dd class="mt-0.5"><details class="group"><summary class="inline-flex cursor-pointer list-none items-center gap-1 rounded-[3px] font-mono text-xs text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-zinc-100"><span class="capitalize">{{ capture.status }}</span> · {{ captureTotal }}<ChevronDown class="size-3.5 text-zinc-500 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none" aria-hidden="true" /></summary><dl class="absolute left-0 top-full z-20 mt-2 grid w-56 grid-cols-2 gap-3 rounded-[7px] border border-zinc-200 bg-white p-3 text-xs shadow-[0_18px_50px_rgba(0,0,0,0.18)] dark:border-zinc-700 dark:bg-stone-900"><div><dt class="text-zinc-500">Saved</dt><dd class="mt-0.5 font-mono">{{ capture.saved_exchanges }}</dd></div><div><dt class="text-zinc-500">Pending</dt><dd class="mt-0.5 font-mono">{{ capture.pending_exchanges }}</dd></div><div><dt class="text-zinc-500">Failed</dt><dd class="mt-0.5 font-mono">{{ capture.failed_exchanges }}</dd></div><div><dt class="text-zinc-500">Last saved</dt><dd class="mt-0.5">{{ capture.last_saved_at ? shortDate(capture.last_saved_at) : "Never" }}</dd></div></dl></details></dd></div>
        </dl>
      </div>
      <div class="min-w-0 border-t border-zinc-200 pt-4 lg:border-t-0 lg:pt-0 dark:border-zinc-800"><h2 class="mb-2 text-xs font-medium text-zinc-500">Models involved</h2><SessionModelStack mode="tree" :app="session.harness" :primary="session.model_primary" :models="session.models" /></div>
    </div>
  </div>
</template>
