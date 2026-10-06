<script setup lang="ts">
import { computed, nextTick, ref, useTemplateRef } from "vue";
import { Check, GitBranch, Pencil, X } from "lucide-vue-next";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import DeviceIdentity from "@/components/DeviceIdentity.vue";
import IdentityBadge from "@/components/IdentityBadge.vue";
import SessionLivenessBadge from "@/components/session/SessionLivenessBadge.vue";
import { errorMessage, setSessionTitle, type SessionDetail, type SessionLiveness, type SessionTitleUpdate } from "@/lib/api";
import { displayTitle } from "@/lib/sessions";
import { harnessLabel } from "@/lib/format";

const MAX_TITLE_LENGTH = 200;
const props = defineProps<{ session: SessionDetail["session"]; capture: SessionDetail["capture"]; liveness: SessionLiveness }>();
const emit = defineEmits<{ saved: [update: SessionTitleUpdate] }>();
const editing = ref(false);
const saving = ref(false);
const editError = ref("");
const draftTitle = ref("");
const titleInput = useTemplateRef<HTMLInputElement>("titleInput");
const editButton = useTemplateRef<HTMLButtonElement>("editButton");
const headerModels = computed(() => props.session.models.length ? props.session.models : props.session.model_primary ? [{ name: props.session.model_primary, request_count: 0 }] : []);

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
  <div class="min-w-0 border-b border-zinc-200 pb-4 dark:border-zinc-800">
    <div class="min-w-0">
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
          <h1 class="min-w-0 break-words text-xl font-semibold leading-tight tracking-[-0.025em] text-zinc-950 sm:text-2xl dark:text-zinc-50">{{ displayTitle(session) }}</h1>
          <button ref="editButton" type="button" class="inline-flex size-8 shrink-0 items-center justify-center rounded-[5px] text-zinc-600 hover:bg-stone-100 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:bg-zinc-800 dark:hover:text-zinc-100" aria-label="Edit session title" aria-controls="session-title-editor" :aria-expanded="editing" @click="startEditing"><Pencil class="size-3.5" aria-hidden="true" /></button>
        </div>
        <div class="mt-3 flex flex-wrap items-center gap-x-3 gap-y-2 text-[13px] text-zinc-600 dark:text-zinc-400">
          <SessionLivenessBadge :liveness="liveness" announce />
          <strong class="break-all font-medium text-zinc-800 dark:text-zinc-200">{{ session.repo || "No repository" }}</strong>
          <span v-if="session.source_ref" class="inline-flex min-w-0 items-center gap-1"><GitBranch class="size-3.5 shrink-0" aria-hidden="true" /><span class="break-all">{{ session.source_ref }}</span></span>
          <OutcomeBadge :outcome="session.outcome" />
          <span class="inline-flex gap-1.5"><span>Capture</span><span class="capitalize" :class="capture.failed_exchanges ? 'text-red-700 dark:text-red-400' : 'text-zinc-800 dark:text-zinc-200'">{{ capture.status }}</span></span>
          <RouterLink v-if="session.parent_session_id" :to="`/sessions/${session.parent_session_id}`" class="font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">Parent session</RouterLink>
        </div>
        <div role="group" aria-label="Session identity" class="mt-3 space-y-2 text-[13px]">
          <div class="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-2">
            <IdentityBadge :label="harnessLabel(session.harness)" :truncate="false" />
            <DeviceIdentity v-if="session.device" :device="session.device" />
          </div>
          <ul id="session-models-heading" aria-label="Session models" class="flex min-w-0 flex-wrap items-center gap-x-5 gap-y-2 scroll-mt-20">
            <li v-for="model in headerModels" :key="model.name" class="flex min-w-0 items-center gap-2">
              <IdentityBadge :label="model.name" :truncate="false" />
              <span v-if="model.request_count" class="shrink-0 text-xs text-zinc-600 dark:text-zinc-400">{{ model.request_count }} {{ model.request_count === 1 ? "request" : "requests" }}</span>
            </li>
            <li v-if="!headerModels.length"><IdentityBadge label="Unknown model" /></li>
          </ul>
        </div>
    </div>
  </div>
</template>
