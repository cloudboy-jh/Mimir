<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import { Pencil, X } from "lucide-vue-next";
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from "reka-ui";
import Button from "@/components/ui/Button.vue";
import Select from "@/components/ui/Select.vue";
import { currentOutcomeEvidence, errorMessage, setSessionOutcome, type Outcome, type OutcomeEvidence, type SessionDetail } from "@/lib/api";
import { evidenceKindOptions } from "@/lib/options";
import { outcomeMeta, outcomeOrder } from "@/lib/outcomes";

// compact renders a text trigger for sessions with no recorded outcome; it
// carries the outcome anchor that summary evidence links target.
const props = defineProps<{ detail: SessionDetail; compact?: boolean }>();
const emit = defineEmits<{ saved: [] }>();

type EvidenceKind = "none" | "commit" | "url" | "note";

const editing = ref(false);
const outcome = ref<Outcome>("unresolved");
const reason = ref("");
const evidenceKind = ref<EvidenceKind>("none");
const evidenceValue = ref("");
const initial = ref({ outcome: "unresolved" as Outcome, reason: "", evidenceKind: "none" as EvidenceKind, evidenceValue: "" });
const saving = ref(false);
const saveError = ref("");
const savedFlash = ref(false);
let saveVersion = 0;
let flashTimer: ReturnType<typeof setTimeout> | undefined;

const dirty = computed(() =>
  outcome.value !== initial.value.outcome
  || reason.value !== initial.value.reason
  || evidenceKind.value !== initial.value.evidenceKind
  || evidenceValue.value !== initial.value.evidenceValue);

const evidence = computed<OutcomeEvidence | undefined>(() => {
  const value = evidenceValue.value.trim();
  const current = recordedEvidence.value;
  if (evidenceKind.value === "commit" && value) {
    if (current?.commit === value) return { ...current };
    return {
      commit: value,
      ...(current?.repository_url ? { repository_url: current.repository_url } : {}),
      ...(current?.ref ? { ref: current.ref } : {}),
      provenance: "user",
    };
  }
  if (evidenceKind.value === "url" && value) return current?.url === value ? { ...current } : { url: value };
  if (evidenceKind.value === "note" && value) return current?.note === value ? { ...current } : { note: value };
  return undefined;
});

const evidenceError = computed(() => {
  const value = evidenceValue.value.trim();
  if (evidenceKind.value === "commit" && value && !/^[0-9a-f]{7,40}$/i.test(value)) return "Enter a 7-40 character hex commit SHA.";
  if (evidenceKind.value === "url" && value && !/^https?:\/\/\S+$/.test(value)) return "Enter an http or https URL.";
  return "";
});

const evidenceKindModel = computed({
  get: () => evidenceKind.value as string,
  set: (value: string) => { evidenceKind.value = value as EvidenceKind; },
});

const recordedEvidence = computed(() => currentOutcomeEvidence(props.detail.outcome_events, props.detail.session.outcome));
const editorOpen = computed({
  get: () => editing.value,
  set: (value: boolean) => {
    if (value) startEditing();
    else cancelEditing();
  },
});

watch(() => [props.detail.session.id, props.detail.session.outcome_updated_at] as const, ([id], previous) => {
  if (previous && id !== previous[0]) editing.value = false;
  if (dirty.value && outcome.value !== props.detail.session.outcome) return;
  const parsed = recordedEvidence.value;
  const kind: EvidenceKind = parsed?.commit ? "commit" : parsed?.url ? "url" : parsed?.note ? "note" : "none";
  const value = parsed?.commit ?? parsed?.url ?? parsed?.note ?? "";
  outcome.value = props.detail.session.outcome;
  reason.value = props.detail.session.outcome_reason ?? "";
  evidenceKind.value = kind;
  evidenceValue.value = value;
  initial.value = { outcome: outcome.value, reason: reason.value, evidenceKind: kind, evidenceValue: value };
}, { immediate: true });

function startEditing() {
  saveError.value = "";
  savedFlash.value = false;
  editing.value = true;
}

function cancelEditing() {
  if (dirty.value && !window.confirm("Discard unsaved outcome changes?")) return;
  outcome.value = initial.value.outcome;
  reason.value = initial.value.reason;
  evidenceKind.value = initial.value.evidenceKind;
  evidenceValue.value = initial.value.evidenceValue;
  saveError.value = "";
  editing.value = false;
}

async function saveOutcome() {
  if (saving.value || !dirty.value || evidenceError.value) return;
  const version = ++saveVersion;
  saving.value = true;
  saveError.value = "";
  savedFlash.value = false;
  try {
    await setSessionOutcome(props.detail.session.id, outcome.value, reason.value, evidence.value);
    if (version !== saveVersion) return;
    initial.value = { outcome: outcome.value, reason: reason.value, evidenceKind: evidenceKind.value, evidenceValue: evidenceValue.value };
    savedFlash.value = true;
    editing.value = false;
    clearTimeout(flashTimer);
    flashTimer = setTimeout(() => { savedFlash.value = false; }, 3_000);
    emit("saved");
  } catch (cause) {
    if (version === saveVersion) saveError.value = errorMessage(cause, "The outcome could not be saved.");
  } finally {
    if (version === saveVersion) saving.value = false;
  }
}

onBeforeRouteLeave(() => {
  if (!dirty.value) return true;
  return window.confirm("Discard unsaved outcome changes?");
});
onBeforeUnmount(() => clearTimeout(flashTimer));
</script>

<template>
  <span v-if="savedFlash" class="mr-1 text-xs font-medium text-emerald-700 dark:text-emerald-400" role="status">Saved</span>
  <PopoverRoot v-model:open="editorOpen">
    <PopoverTrigger as-child>
      <button v-if="compact" id="outcome-heading" type="button" class="scroll-mt-20 rounded-[3px] text-[13px] font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">Set outcome</button>
      <Button v-else variant="outline" class="px-2.5"><Pencil class="size-3.5" />Edit</Button>
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent :align="compact ? 'start' : 'end'" :side-offset="8" :collision-padding="16" class="z-50 max-h-[min(42rem,calc(100vh-2rem))] w-[min(34rem,calc(100vw-2rem))] origin-(--reka-popover-content-transform-origin) overflow-y-auto rounded-[7px] border border-zinc-200 bg-white p-4 shadow-[0_18px_50px_rgba(0,0,0,0.18)] focus:outline-none data-[state=closed]:animate-popover-out data-[state=open]:animate-popover-in motion-reduce:animate-none dark:border-zinc-700 dark:bg-zinc-900">
        <div class="mb-3 flex items-start justify-between gap-6">
          <div><h3 class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">Edit work outcome</h3><p class="mt-1 text-xs leading-4 text-zinc-500 dark:text-zinc-400">Record the result separately from capture status.</p></div>
          <button type="button" class="grid size-7 shrink-0 place-items-center rounded-[4px] text-zinc-500 transition-colors duration-150 ease-out hover:bg-stone-100 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:hover:bg-zinc-800 dark:hover:text-zinc-50" aria-label="Close outcome editor" @click="cancelEditing"><X class="size-4" /></button>
        </div>
        <form id="outcome-editor" class="space-y-3" @submit.prevent="saveOutcome">
          <fieldset class="overflow-hidden rounded-[5px] border border-zinc-300 dark:border-zinc-700">
            <legend class="sr-only">Outcome</legend>
            <label v-for="option in outcomeOrder" :key="option" class="flex cursor-pointer items-center gap-2.5 border-b border-zinc-200 px-2.5 py-2 text-[13px] transition-colors duration-150 ease-out last:border-b-0 has-focus-visible:outline-2 has-focus-visible:-outline-offset-2 has-focus-visible:outline-teal-600 dark:border-zinc-800" :class="outcome === option ? outcomeMeta[option].selected : 'text-zinc-700 hover:bg-stone-50 dark:text-zinc-300 dark:hover:bg-zinc-800/60'">
              <input v-model="outcome" type="radio" name="outcome" :value="option" class="sr-only" />
              <component :is="outcomeMeta[option].icon" class="size-4 shrink-0" :class="outcome === option ? '' : outcomeMeta[option].accent" aria-hidden="true" />
              <span class="min-w-0"><span class="block font-medium">{{ outcomeMeta[option].label }}</span><span v-if="outcome === option" class="mt-0.5 block text-xs leading-4 opacity-90">{{ outcomeMeta[option].description }}</span></span>
            </label>
          </fieldset>
          <label class="block text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Reason</span><textarea v-model="reason" maxlength="2000" rows="2" placeholder="Why did this work land or stop?" class="w-full resize-y rounded-[5px] border border-zinc-300 bg-white px-2.5 py-2 text-[13px] font-normal leading-5 placeholder:text-zinc-500 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
          <div class="flex flex-col gap-2 sm:flex-row">
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Evidence</span><Select v-model="evidenceKindModel" label="Evidence type" :options="evidenceKindOptions" class="w-full font-normal sm:w-40" /></div>
            <label v-if="evidenceKind !== 'none'" class="min-w-0 flex-1 text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">{{ evidenceKind === "commit" ? "Commit SHA" : evidenceKind === "url" ? "URL" : "Note" }}</span><input v-model="evidenceValue" :placeholder="evidenceKind === 'commit' ? 'e.g. a1b2c3d' : evidenceKind === 'url' ? 'https://…' : 'Supporting detail'" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" :class="evidenceKind === 'commit' ? 'font-mono' : ''" /></label>
          </div>
          <p v-if="evidenceError" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ evidenceError }}</p>
          <p v-if="saveError" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ saveError }}</p>
          <div class="flex items-center justify-end gap-2 border-t border-zinc-200 pt-3 dark:border-zinc-800">
            <span v-if="dirty && !saveError" class="mr-auto text-xs text-zinc-500">Unsaved changes</span>
            <Button variant="ghost" @click="cancelEditing">Cancel</Button>
            <Button type="submit" :disabled="saving || !dirty || !!evidenceError">{{ saving ? "Saving…" : "Save outcome" }}</Button>
          </div>
        </form>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>
