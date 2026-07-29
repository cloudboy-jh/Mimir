<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import { ExternalLink, GitCommitHorizontal, Pencil } from "lucide-vue-next";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import Button from "@/components/ui/Button.vue";
import Select from "@/components/ui/Select.vue";
import { errorMessage, parseOutcomeEvidence, setSessionOutcome, type Outcome, type OutcomeEvidence, type SessionDetail } from "@/lib/api";
import { shortDate } from "@/lib/format";
import { commitUrl, shortCommit } from "@/lib/git";
import { evidenceKindOptions } from "@/lib/options";
import { outcomeMeta, outcomeOrder } from "@/lib/outcomes";

const props = defineProps<{ detail: SessionDetail }>();
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
  if (evidenceKind.value === "commit" && value) return { commit: value, provenance: "user" };
  if (evidenceKind.value === "url" && value) return { url: value };
  if (evidenceKind.value === "note" && value) return { note: value };
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

const latestEvent = computed(() => props.detail.outcome_events[0] ?? null);
const recordedEvidence = computed(() => parseOutcomeEvidence(latestEvent.value?.evidence_json ?? null));
const recordedCommitUrl = computed(() => commitUrl(recordedEvidence.value));

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

function evidenceSummary(json: string | null): string {
  const parsed = parseOutcomeEvidence(json);
  if (!parsed) return "";
  if (parsed.commit) return `commit ${shortCommit(parsed.commit)}`;
  if (parsed.url) return parsed.url;
  if (parsed.note) return parsed.note;
  return "";
}

onBeforeRouteLeave(() => {
  if (!dirty.value) return true;
  return window.confirm("Discard unsaved outcome changes?");
});
onBeforeUnmount(() => clearTimeout(flashTimer));
</script>

<template>
  <section aria-labelledby="outcome-heading">
    <div class="flex items-center justify-between gap-4">
      <h2 id="outcome-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Work outcome</h2>
      <span v-if="savedFlash" class="text-xs font-medium text-emerald-700 dark:text-emerald-400" role="status">Saved</span>
      <Button v-if="!editing" variant="outline" class="px-2.5" @click="startEditing"><Pencil class="size-3.5" />Edit</Button>
    </div>

    <div v-if="!editing" class="mt-3 space-y-2.5">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <OutcomeBadge :outcome="detail.session.outcome" />
        <span class="text-xs text-zinc-500">
          <template v-if="detail.session.outcome_src">Set by {{ detail.session.outcome_src }}<template v-if="detail.session.outcome_updated_at"> · {{ shortDate(detail.session.outcome_updated_at) }}</template></template>
          <template v-else>Not recorded yet</template>
        </span>
      </div>
      <p class="max-w-prose text-[13px] leading-5 text-zinc-700 dark:text-zinc-300">{{ detail.session.outcome_reason || outcomeMeta[detail.session.outcome].description }}</p>
      <p v-if="recordedEvidence?.commit" class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-zinc-500">
        <GitCommitHorizontal class="size-3.5" aria-hidden="true" />
        <a v-if="recordedCommitUrl" :href="recordedCommitUrl" target="_blank" rel="noreferrer noopener" class="inline-flex items-center gap-1 font-mono text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ shortCommit(recordedEvidence.commit) }}<ExternalLink class="size-3" aria-hidden="true" /></a>
        <span v-else class="font-mono text-zinc-800 dark:text-zinc-200">{{ shortCommit(recordedEvidence.commit) }}</span>
        <span v-if="recordedEvidence.ref || detail.session.source_ref" class="font-mono">{{ recordedEvidence.ref || detail.session.source_ref }}</span>
        <span v-if="recordedEvidence.provenance">via {{ recordedEvidence.provenance }}</span>
      </p>
      <p v-else-if="recordedEvidence?.url" class="text-xs">
        <a :href="recordedEvidence.url" target="_blank" rel="noreferrer noopener" class="inline-flex items-center gap-1 break-all text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ recordedEvidence.url }}<ExternalLink class="size-3 shrink-0" aria-hidden="true" /></a>
      </p>
      <p v-else-if="recordedEvidence?.note" class="text-xs leading-5 text-zinc-500">{{ recordedEvidence.note }}</p>
      <p v-else class="text-xs text-zinc-500">No supporting evidence recorded.</p>
      <p v-if="saveError" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ saveError }}</p>
    </div>

    <form v-else class="mt-3 space-y-3" @submit.prevent="saveOutcome">
      <fieldset class="overflow-hidden rounded-[5px] border border-zinc-300 dark:border-zinc-700">
        <legend class="sr-only">Outcome</legend>
        <label v-for="option in outcomeOrder" :key="option" class="flex cursor-pointer items-center gap-2.5 border-b border-zinc-200 px-2.5 py-2 text-[13px] transition-colors duration-150 ease-out last:border-b-0 has-focus-visible:outline-2 has-focus-visible:-outline-offset-2 has-focus-visible:outline-teal-600 dark:border-zinc-800" :class="outcome === option ? outcomeMeta[option].selected : 'text-zinc-700 hover:bg-stone-50 dark:text-zinc-300 dark:hover:bg-zinc-800/60'">
          <input v-model="outcome" type="radio" name="outcome" :value="option" class="sr-only" />
          <component :is="outcomeMeta[option].icon" class="size-4 shrink-0" :class="outcome === option ? '' : outcomeMeta[option].accent" aria-hidden="true" />
          <span class="min-w-0">
            <span class="block font-medium">{{ outcomeMeta[option].label }}</span>
            <span v-if="outcome === option" class="mt-0.5 block text-xs leading-4 opacity-90">{{ outcomeMeta[option].description }}</span>
          </span>
        </label>
      </fieldset>
      <label class="block"><span class="sr-only">Outcome reason</span><textarea v-model="reason" maxlength="2000" rows="2" placeholder="Why did this work land or stop?" class="w-full resize-y rounded-[5px] border border-zinc-300 bg-white px-2.5 py-2 text-[13px] leading-5 placeholder:text-zinc-500 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
      <div class="flex flex-col gap-2 sm:flex-row">
        <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Evidence</span><Select v-model="evidenceKindModel" label="Evidence type" :options="evidenceKindOptions" class="w-full font-normal sm:w-40" /></div>
        <label v-if="evidenceKind !== 'none'" class="min-w-0 flex-1 text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">{{ evidenceKind === "commit" ? "Commit SHA" : evidenceKind === "url" ? "URL" : "Note" }}</span><input v-model="evidenceValue" :placeholder="evidenceKind === 'commit' ? 'e.g. a1b2c3d' : evidenceKind === 'url' ? 'https://…' : 'Supporting detail'" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" :class="evidenceKind === 'commit' ? 'font-mono' : ''" /></label>
      </div>
      <p v-if="evidenceError" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ evidenceError }}</p>
      <div class="flex items-center gap-3">
        <Button type="submit" :disabled="saving || !dirty || !!evidenceError">{{ saving ? "Saving…" : "Save outcome" }}</Button>
        <Button variant="ghost" @click="cancelEditing">Cancel</Button>
        <span v-if="saveError" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ saveError }}</span>
        <span v-else-if="dirty" class="text-xs text-zinc-500">Unsaved changes</span>
      </div>
    </form>

    <details v-if="detail.outcome_events.length" class="mt-4 text-xs text-zinc-500 dark:text-zinc-400">
      <summary class="inline-flex cursor-pointer list-none items-center gap-1 font-medium text-teal-700 hover:text-teal-900 focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-teal-400 dark:hover:text-teal-300">Outcome history ({{ detail.outcome_events.length }})</summary>
      <ul class="mt-3 max-h-60 space-y-2.5 overflow-y-auto border-t border-zinc-200 pt-3 dark:border-zinc-800">
        <li v-for="event in detail.outcome_events" :key="event.id" class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <OutcomeBadge :outcome="event.outcome" />
          <span>{{ event.source }}</span>
          <span>{{ shortDate(event.created_at) }}</span>
          <span v-if="event.reason" class="basis-full text-zinc-600 dark:text-zinc-400">{{ event.reason }}</span>
          <span v-if="evidenceSummary(event.evidence_json)" class="basis-full truncate font-mono text-[11px]">{{ evidenceSummary(event.evidence_json) }}</span>
        </li>
      </ul>
    </details>
  </section>
</template>
