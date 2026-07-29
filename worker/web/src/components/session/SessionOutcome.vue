<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { onBeforeRouteLeave } from "vue-router";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import { errorMessage, parseOutcomeEvidence, setSessionOutcome, type Outcome, type OutcomeEvidence, type SessionDetail } from "@/lib/api";
import { shortDate } from "@/lib/format";

const props = defineProps<{ detail: SessionDetail }>();
const emit = defineEmits<{ saved: [] }>();

type EvidenceKind = "none" | "commit" | "url" | "note";

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

const outcomeDescriptions: Record<Outcome, string> = {
  landed: "The result was kept or shipped.",
  discarded: "The result was deliberately rejected or reverted.",
  abandoned: "Work stopped without a result.",
  unresolved: "No evidenced result has been recorded yet.",
};

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

const latestEvent = computed(() => props.detail.outcome_events[0] ?? null);

watch(() => [props.detail.session.id, props.detail.session.outcome_updated_at] as const, () => {
  if (dirty.value && outcome.value !== props.detail.session.outcome) return;
  const evidenceParsed = parseOutcomeEvidence(latestEvent.value?.evidence_json ?? null);
  const kind: EvidenceKind = evidenceParsed?.commit ? "commit" : evidenceParsed?.url ? "url" : evidenceParsed?.note ? "note" : "none";
  const value = evidenceParsed?.commit ?? evidenceParsed?.url ?? evidenceParsed?.note ?? "";
  outcome.value = props.detail.session.outcome;
  reason.value = props.detail.session.outcome_reason ?? "";
  evidenceKind.value = kind;
  evidenceValue.value = value;
  initial.value = { outcome: outcome.value, reason: reason.value, evidenceKind: kind, evidenceValue: value };
}, { immediate: true });

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
  if (parsed.commit) return `commit ${parsed.commit.slice(0, 7)}`;
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
    <h2 id="outcome-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Work outcome</h2>
    <form class="mt-3 space-y-3" @submit.prevent="saveOutcome">
      <fieldset class="grid gap-1.5 sm:grid-cols-2">
        <legend class="sr-only">Outcome</legend>
        <label v-for="option in (['landed', 'discarded', 'abandoned', 'unresolved'] as Outcome[])" :key="option" class="flex cursor-pointer items-start gap-2 rounded-[5px] border px-2.5 py-2 text-[13px] has-checked:border-zinc-900 has-checked:bg-stone-50 dark:has-checked:border-zinc-300 dark:has-checked:bg-zinc-900" :class="outcome === option ? 'border-zinc-900 dark:border-zinc-300' : 'border-zinc-300 dark:border-zinc-700'">
          <input v-model="outcome" type="radio" name="outcome" :value="option" class="mt-1 accent-teal-700" />
          <span><span class="block font-medium capitalize text-zinc-900 dark:text-zinc-100">{{ option }}</span><span class="mt-0.5 block text-xs leading-4 text-zinc-500">{{ outcomeDescriptions[option] }}</span></span>
        </label>
      </fieldset>
      <label class="block"><span class="sr-only">Outcome reason</span><textarea v-model="reason" maxlength="2000" rows="2" placeholder="Why did this work land or stop?" class="w-full resize-y rounded-[5px] border border-zinc-300 bg-white px-2.5 py-2 text-[13px] leading-5 placeholder:text-zinc-500 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" /></label>
      <div class="flex flex-col gap-2 sm:flex-row">
        <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Evidence</span><select v-model="evidenceKind" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 sm:w-36"><option value="none">None</option><option value="commit">Commit</option><option value="url">URL</option><option value="note">Note</option></select></label>
        <label v-if="evidenceKind !== 'none'" class="min-w-0 flex-1 text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">{{ evidenceKind === "commit" ? "Commit SHA" : evidenceKind === "url" ? "URL" : "Note" }}</span><input v-model="evidenceValue" :placeholder="evidenceKind === 'commit' ? 'e.g. a1b2c3d' : evidenceKind === 'url' ? 'https://…' : 'Supporting detail'" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900" :class="evidenceKind === 'commit' ? 'font-mono' : ''" /></label>
      </div>
      <p v-if="evidenceError" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ evidenceError }}</p>
      <div class="flex items-center gap-3">
        <button type="submit" :disabled="saving || !dirty || !!evidenceError" class="h-8.5 rounded-[5px] bg-zinc-900 px-3 text-[13px] font-medium text-zinc-50 hover:bg-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-not-allowed disabled:opacity-60 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300">{{ saving ? "Saving…" : "Save outcome" }}</button>
        <span v-if="savedFlash" class="text-xs font-medium text-emerald-700 dark:text-emerald-400" role="status">Saved</span>
        <span v-else-if="saveError" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ saveError }}</span>
        <span v-else-if="dirty" class="text-xs text-zinc-500">Unsaved changes</span>
        <span v-else-if="detail.session.outcome_src" class="text-xs text-zinc-500" aria-live="polite">Set by {{ detail.session.outcome_src }}<template v-if="detail.session.outcome_updated_at"> · {{ shortDate(detail.session.outcome_updated_at) }}</template></span>
      </div>
    </form>
    <details v-if="detail.outcome_events.length" class="group mt-4 text-xs text-zinc-500 dark:text-zinc-400">
      <summary class="inline-flex cursor-pointer list-none items-center gap-1 font-medium text-teal-700 hover:text-teal-900 focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden dark:text-teal-400 dark:hover:text-teal-300">Outcome history ({{ detail.outcome_events.length }})</summary>
      <ul class="mt-3 space-y-2.5 border-t border-zinc-200 pt-3 dark:border-zinc-800">
        <li v-for="event in detail.outcome_events" :key="event.id" class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <OutcomeBadge :outcome="event.outcome" />
          <span>{{ event.source }}</span>
          <span>{{ shortDate(event.created_at) }}</span>
          <span v-if="event.reason" class="basis-full text-zinc-600 dark:text-zinc-400">{{ event.reason }}</span>
          <span v-if="evidenceSummary(event.evidence_json)" class="basis-full font-mono text-[11px]">{{ evidenceSummary(event.evidence_json) }}</span>
        </li>
      </ul>
    </details>
  </section>
</template>
