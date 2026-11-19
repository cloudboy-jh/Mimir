<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef } from "vue";
import { Check, GitBranch, Pencil, X } from "lucide-vue-next";
import CaptureStatus from "@/components/CaptureStatus.vue";
import DeviceIdentity from "@/components/DeviceIdentity.vue";
import GitStatus from "@/components/GitStatus.vue";
import IdentityBadge from "@/components/IdentityBadge.vue";
import { errorMessage, setSessionTitle, type Receipt, type Session, type TitleUpdate } from "@/lib/api";
import { compactNumber, duration, harnessLabel, shortDate } from "@/lib/format";

const MAX_TITLE_LENGTH = 200;
const props = defineProps<{ session: Session; receipt: Receipt }>();
const emit = defineEmits<{ saved: [update: TitleUpdate] }>();
const editing = ref(false);
const saving = ref(false);
const editError = ref("");
const draftTitle = ref("");
const titleInput = useTemplateRef<HTMLInputElement>("titleInput");
const editButton = useTemplateRef<HTMLButtonElement>("editButton");

const facts = computed(() => {
  const { session } = props;
  const rows: Array<{ label: string; value: string }> = [
    { label: "Started", value: shortDate(session.started_at) },
    { label: "Duration", value: duration(session.started_at, session.ended_at) },
    { label: "Requests", value: String(session.request_count) },
    { label: "Tokens", value: `${compactNumber(session.tokens_in)} in · ${compactNumber(session.tokens_out)} out` },
  ];
  if (session.cache_read_tokens) rows.push({ label: "Cache read", value: compactNumber(session.cache_read_tokens) });
  if (session.cache_write_tokens) rows.push({ label: "Cache write", value: compactNumber(session.cache_write_tokens) });
  if (session.capture.last_saved_at) rows.push({ label: "Last saved", value: shortDate(session.capture.last_saved_at) });
  return rows;
});

async function startEditing() {
  draftTitle.value = props.session.title ?? props.session.display_title;
  editError.value = "";
  editing.value = true;
  await nextTick();
  titleInput.value?.select();
}

async function stopEditing() {
  editing.value = false;
  editError.value = "";
  await nextTick();
  editButton.value?.focus();
}

// An empty title clears the manual title and restores the automatic one.
async function saveTitle() {
  if (saving.value) return;
  saving.value = true;
  editError.value = "";
  try {
    emit("saved", await setSessionTitle(props.session.id, draftTitle.value.trim() || null));
    await stopEditing();
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
  <header class="min-w-0 border-b border-zinc-200 pb-4 dark:border-zinc-800">
    <form v-if="editing" id="session-title-editor" class="max-w-3xl" @submit.prevent="saveTitle" @keydown.esc.prevent="!saving && stopEditing()">
      <label for="session-title" class="sr-only">Session title</label>
      <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
        <input id="session-title" ref="titleInput" v-model="draftTitle" type="text" :maxlength="MAX_TITLE_LENGTH" :disabled="saving" aria-describedby="session-title-hint" class="h-10 min-w-0 flex-1 rounded-[5px] border border-zinc-300 bg-white px-3 text-lg font-semibold tracking-[-0.025em] text-zinc-950 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 disabled:cursor-wait disabled:opacity-60 sm:text-xl dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-50" />
        <div class="flex shrink-0 items-center gap-2">
          <button type="submit" :disabled="saving" class="inline-flex h-8.5 items-center gap-1.5 rounded-[5px] bg-zinc-900 px-2.5 text-xs font-medium text-white hover:bg-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-wait disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-950 dark:hover:bg-zinc-300"><Check class="size-3.5" aria-hidden="true" />{{ saving ? "Saving…" : "Save" }}</button>
          <button type="button" :disabled="saving" class="inline-flex h-8.5 items-center gap-1.5 rounded-[5px] border border-zinc-300 px-2.5 text-xs font-medium text-zinc-700 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:opacity-60 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800" @click="stopEditing"><X class="size-3.5" aria-hidden="true" />Cancel</button>
        </div>
      </div>
      <p id="session-title-hint" class="mt-2 text-xs text-zinc-600 dark:text-zinc-400">Leave empty to restore the automatic title.</p>
      <p v-if="editError" class="mt-1 text-xs text-red-700 dark:text-red-400" role="alert">{{ editError }}</p>
    </form>
    <div v-else class="flex min-w-0 items-start gap-2">
      <h1 class="min-w-0 break-words text-xl font-semibold leading-tight tracking-[-0.025em] text-zinc-950 sm:text-2xl dark:text-zinc-50">{{ session.display_title }}</h1>
      <button ref="editButton" type="button" class="inline-flex size-8 shrink-0 items-center justify-center rounded-[5px] text-zinc-600 hover:bg-stone-100 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100" aria-label="Edit session title" @click="startEditing"><Pencil class="size-3.5" aria-hidden="true" /></button>
    </div>

    <div class="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-[13px] text-zinc-600 dark:text-zinc-400">
      <span v-if="session.state === 'active'" class="font-medium text-emerald-800 dark:text-emerald-300">In progress</span>
      <strong class="break-all font-medium text-zinc-800 dark:text-zinc-200">{{ session.repo || "No repository" }}</strong>
      <span v-if="session.source_ref" class="inline-flex min-w-0 items-center gap-1"><GitBranch class="size-3.5 shrink-0" aria-hidden="true" /><span class="break-all">{{ session.source_ref }}</span></span>
      <GitStatus :status="session.git_status" :count="session.commit_count" />
      <span class="inline-flex gap-1.5"><span>Capture</span><CaptureStatus :capture="session.capture" /></span>
      <RouterLink v-if="session.parent_session_id" :to="`/sessions/${session.parent_session_id}`" class="font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">Parent session</RouterLink>
    </div>

    <div role="group" aria-label="Session identity" class="mt-3 space-y-2 text-[13px]">
      <div class="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-2">
        <IdentityBadge :label="harnessLabel(session.harness)" :truncate="false" />
        <DeviceIdentity v-if="session.device" :device="session.device" />
      </div>
      <ul aria-label="Models" class="flex min-w-0 flex-wrap items-center gap-x-5 gap-y-2">
        <li v-for="model in session.models" :key="model.name" class="flex min-w-0 items-center gap-2">
          <IdentityBadge :label="model.name" :truncate="false" />
          <span class="shrink-0 text-xs text-zinc-600 dark:text-zinc-400">{{ model.request_count }} {{ model.request_count === 1 ? "request" : "requests" }}</span>
        </li>
        <li v-if="!session.models.length"><IdentityBadge :label="session.model_primary || 'Unknown model'" /></li>
      </ul>
    </div>

    <p class="mt-3 text-xs text-zinc-700 dark:text-zinc-300">{{ receipt.line }}</p>
    <dl aria-label="Session facts" class="mt-2 flex flex-wrap gap-x-5 gap-y-1.5 text-xs">
      <div v-for="fact in facts" :key="fact.label" class="flex gap-1.5"><dt class="text-zinc-600 dark:text-zinc-400">{{ fact.label }}</dt><dd class="tabular-nums text-zinc-800 dark:text-zinc-200">{{ fact.value }}</dd></div>
      <div class="flex min-w-0 gap-1.5"><dt class="text-zinc-600 dark:text-zinc-400">ID</dt><dd class="break-all font-mono text-[11px] leading-4 text-zinc-800 dark:text-zinc-200">{{ session.id }}</dd></div>
    </dl>
  </header>
</template>
