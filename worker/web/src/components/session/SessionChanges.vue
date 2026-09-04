<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { CheckCircle2, ChevronDown, CircleX, Clock3, ExternalLink, GitBranch, GitCommitHorizontal } from "lucide-vue-next";
import { outcomeCommitEvidence, type GitArtifact, type OutcomeEvent, type OutcomeEvidence } from "@/lib/api";
import { parsePatch } from "@/lib/diff";
import { shortDate } from "@/lib/format";
import { commitUrl, gitArtifactCommitUrl, gitArtifactProvenance, outcomeCommitMatchesArtifact, outcomeUrlMatchesArtifact, shortCommit } from "@/lib/git";

const PREVIEW_LIMIT = 5;
const props = defineProps<{ sessionId: string; artifacts: GitArtifact[]; events: OutcomeEvent[]; evidence: OutcomeEvidence | null; sourceRef?: string | null }>();
const showAll = ref(false);
const outcomeEvidence = computed(() => props.evidence);
const diffs = computed(() => outcomeEvidence.value?.patch ? parsePatch(outcomeEvidence.value.patch) : []);
const totals = computed(() => ({
  added: outcomeEvidence.value?.patch_additions ?? diffs.value.reduce((sum, file) => sum + file.added, 0),
  removed: outcomeEvidence.value?.patch_deletions ?? diffs.value.reduce((sum, file) => sum + file.removed, 0),
}));
const fileCount = computed(() => outcomeEvidence.value?.patch_files ?? diffs.value.length);
const hasLegacyDiff = computed(() => Boolean(outcomeEvidence.value?.patch || outcomeEvidence.value?.patch_r2_key));
const visibleDiffs = computed(() => showAll.value ? diffs.value : diffs.value.slice(0, PREVIEW_LIMIT));
const commitHistory = computed(() => outcomeCommitEvidence(props.events));
const outcomeUrl = computed(() => outcomeEvidence.value?.url ?? (!outcomeEvidence.value?.commit ? outcomeEvidence.value?.commit_url : undefined));
const duplicateOutcomeUrl = computed(() => outcomeUrlMatchesArtifact(outcomeUrl.value, props.artifacts));
const distinctOutcomeUrl = computed(() => duplicateOutcomeUrl.value ? null : outcomeUrl.value ?? null);
const hasUnverifiedArtifacts = computed(() => props.artifacts.some((artifact) => gitArtifactProvenance(artifact.provenance).unverified));

function artifactStatus(artifact: GitArtifact) {
  if (artifact.capture_status === "saved") return { label: "Patch saved", icon: CheckCircle2, class: "text-emerald-700 dark:text-emerald-400" };
  if (artifact.capture_status === "failed") return { label: "Patch capture failed", icon: CircleX, class: "text-red-700 dark:text-red-400" };
  return { label: "Patch pending", icon: Clock3, class: "text-amber-700 dark:text-amber-400" };
}

function captureTime(artifact: GitArtifact) {
  const lifecycle = [`Accepted ${shortDate(artifact.accepted_at)}`];
  if (artifact.capture_status === "saved" && artifact.saved_at) lifecycle.push(`Saved ${shortDate(artifact.saved_at)}`);
  if (artifact.capture_status === "failed" && artifact.failed_at) lifecycle.push(`Failed ${shortDate(artifact.failed_at)}`);
  return lifecycle.join(" · ");
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(bytes < 10 * 1024 ? 1 : 0)} KiB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
}

watch(() => outcomeEvidence.value?.commit, () => { showAll.value = false; });
</script>

<template>
  <div class="space-y-8">
    <section aria-labelledby="changes-heading">
      <div class="flex items-baseline justify-between gap-3 border-b border-zinc-200 pb-2.5 dark:border-zinc-800">
        <h2 id="changes-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Git artifacts</h2>
        <span v-if="artifacts.length" class="font-mono text-[11px] text-zinc-500">{{ artifacts.length }} {{ artifacts.length === 1 ? "commit" : "commits" }}</span>
      </div>

      <p v-if="hasUnverifiedArtifacts" class="border-b border-zinc-200 py-2.5 text-xs leading-5 text-zinc-600 dark:border-zinc-800 dark:text-zinc-400">
        Local evidence is unverified. It shows commits observed in a checkout, not that they were pushed, merged, or landed.
      </p>

      <ol v-if="artifacts.length" class="divide-y divide-zinc-200 border-b border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="artifact in artifacts" :key="artifact.commit_sha" class="py-4 first:pt-3">
        <div class="flex min-w-0 items-start gap-2.5">
          <GitCommitHorizontal class="mt-0.5 size-4 shrink-0 text-zinc-500" aria-hidden="true" />
          <div class="min-w-0 flex-1">
            <p v-if="artifact.subject" class="mb-1 text-[13px] font-medium leading-5 text-zinc-900 dark:text-zinc-100">{{ artifact.subject }}</p>
            <div class="flex min-w-0 items-start gap-1.5">
              <a v-if="gitArtifactCommitUrl(artifact)" :href="gitArtifactCommitUrl(artifact)!" target="_blank" rel="noreferrer noopener" class="min-w-0 break-all font-mono text-xs text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ artifact.commit_sha }}<ExternalLink class="ml-1 inline size-3" aria-hidden="true" /><span class="sr-only">(opens in a new tab)</span></a>
              <span v-else class="min-w-0 break-all font-mono text-xs text-zinc-800 dark:text-zinc-200">{{ artifact.commit_sha }}</span>
            </div>
          </div>
        </div>

        <dl class="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs leading-5">
          <template v-if="artifact.committed_at"><dt class="text-zinc-500">Committed</dt><dd class="text-zinc-700 dark:text-zinc-300">{{ shortDate(artifact.committed_at) }}</dd></template>
          <dt class="text-zinc-500">Ref</dt><dd class="min-w-0 break-all font-mono text-zinc-700 dark:text-zinc-300">{{ artifact.ref || "Not recorded" }}</dd>
          <dt class="text-zinc-500">Source</dt><dd class="text-zinc-700 dark:text-zinc-300">{{ gitArtifactProvenance(artifact.provenance).label }}<span v-if="gitArtifactProvenance(artifact.provenance).label !== artifact.provenance" class="ml-1 font-mono text-[11px] text-zinc-500">({{ artifact.provenance }})</span></dd>
          <dt class="text-zinc-500">Capture</dt><dd class="flex flex-wrap items-center gap-x-2 gap-y-1"><span class="inline-flex items-center gap-1 font-medium" :class="artifactStatus(artifact).class"><component :is="artifactStatus(artifact).icon" class="size-3.5" aria-hidden="true" />{{ artifactStatus(artifact).label }}</span><span class="text-zinc-500">{{ captureTime(artifact) }}</span><span v-if="artifact.failure_code" class="font-mono text-[11px] text-red-700 dark:text-red-400">{{ artifact.failure_code }}</span></dd>
          <dt class="text-zinc-500">Patch</dt><dd class="font-mono text-[11px] text-zinc-600 dark:text-zinc-400">{{ artifact.patch_files }} {{ artifact.patch_files === 1 ? "file" : "files" }} · <span class="text-emerald-700 dark:text-emerald-400">+{{ artifact.patch_additions }}</span> <span class="text-red-700 dark:text-red-400">−{{ artifact.patch_deletions }}</span> · {{ formatBytes(artifact.patch_bytes) }}</dd>
        </dl>

        <RouterLink v-if="artifact.capture_status === 'saved'" :to="{ name: 'session-artifact-diff', params: { id: sessionId, commit: artifact.commit_sha } }" class="mt-2.5 inline-flex text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">View exact patch</RouterLink>
        <p v-else class="mt-2.5 text-xs text-zinc-500">Exact patch retrieval is unavailable until capture succeeds.</p>
        </li>
      </ol>
      <p v-else class="pt-3 text-sm text-zinc-500">No independent Git artifacts recorded.</p>
    </section>

    <section aria-labelledby="outcome-evidence-heading">
      <div class="border-b border-zinc-200 pb-2.5 dark:border-zinc-800">
        <h2 id="outcome-evidence-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Outcome evidence</h2>
        <p class="mt-1 text-[11px] leading-4 text-zinc-500">Recorded with the work outcome, separately from independent Git artifacts.</p>
      </div>

      <ol v-if="commitHistory.length" class="divide-y divide-zinc-200 border-b border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
        <li v-for="entry in commitHistory" :key="entry.event.id" class="py-3 text-xs">
          <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
            <GitCommitHorizontal class="size-3.5 text-zinc-500" aria-hidden="true" />
            <a v-if="commitUrl(entry.evidence)" :href="commitUrl(entry.evidence)!" target="_blank" rel="noreferrer noopener" class="inline-flex items-center gap-1 font-mono text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ shortCommit(entry.evidence.commit!) }}<ExternalLink class="size-3" aria-hidden="true" /><span class="sr-only">(opens in a new tab)</span></a>
            <span v-else class="font-mono text-zinc-800 dark:text-zinc-200">{{ shortCommit(entry.evidence.commit!) }}</span>
            <span class="capitalize text-zinc-600 dark:text-zinc-400">{{ entry.event.outcome }}</span>
            <span class="text-zinc-500">{{ shortDate(entry.event.created_at) }} · {{ entry.event.source }}</span>
          </div>
          <p v-if="outcomeCommitMatchesArtifact(entry.evidence.commit, artifacts)" class="mt-1.5 text-zinc-500">The captured patch for this commit is shown in Git artifacts above.</p>
          <p v-if="entry.event.reason" class="mt-1.5 leading-5 text-zinc-600 dark:text-zinc-400">{{ entry.event.reason }}</p>
          <div v-if="entry.evidence.ref || sourceRef || entry.evidence.base_commit || entry.evidence.provenance" class="mt-1.5 flex flex-wrap gap-x-3 gap-y-1 font-mono text-[11px] text-zinc-500">
            <span v-if="entry.evidence.ref || sourceRef" class="inline-flex min-w-0 items-center gap-1"><GitBranch class="size-3 shrink-0" aria-hidden="true" /><span class="break-all">{{ entry.evidence.ref || sourceRef }}</span></span>
            <span v-if="entry.evidence.base_commit">on {{ shortCommit(entry.evidence.base_commit) }}</span>
            <span v-if="entry.evidence.provenance">via {{ entry.evidence.provenance }}</span>
          </div>
        </li>
      </ol>
      <p v-if="distinctOutcomeUrl" class="border-b border-zinc-200 py-2.5 text-xs dark:border-zinc-800"><a :href="distinctOutcomeUrl" target="_blank" rel="noreferrer noopener" class="inline-flex items-center gap-1 break-all text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ distinctOutcomeUrl }}<ExternalLink class="size-3 shrink-0" aria-hidden="true" /><span class="sr-only">(opens in a new tab)</span></a></p>
      <p v-if="outcomeEvidence?.note" class="border-b border-zinc-200 py-2.5 text-xs leading-5 text-zinc-600 dark:border-zinc-800 dark:text-zinc-400">{{ outcomeEvidence.note }}</p>
      <p v-if="hasLegacyDiff && !diffs.length" class="pt-3 text-xs leading-5 text-zinc-500">The legacy outcome patch is stored separately. Open it to inspect changed files.</p>

      <template v-if="hasLegacyDiff">
        <p class="py-2.5 font-mono text-[11px] text-zinc-500">
          Legacy patch · {{ fileCount }} {{ fileCount === 1 ? "file" : "files" }}
          <span class="ml-1 text-emerald-700 dark:text-emerald-400">+{{ totals.added }}</span>
          <span class="ml-0.5 text-red-700 dark:text-red-400">−{{ totals.removed }}</span>
        </p>
        <ul v-if="diffs.length" class="divide-y divide-zinc-200 border-b border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
          <li v-for="file in visibleDiffs" :key="file.file" class="min-w-0">
            <details class="group">
              <summary class="flex cursor-pointer list-none items-baseline justify-between gap-3 rounded-[3px] py-2.5 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden">
                <span class="min-w-0 truncate font-mono text-xs text-zinc-700 dark:text-zinc-300" :title="file.file">{{ file.file }}</span>
                <span class="flex shrink-0 items-center gap-1.5 font-mono text-[11px]"><span><span class="text-emerald-700 dark:text-emerald-400">+{{ file.added }}</span> <span class="text-red-700 dark:text-red-400">−{{ file.removed }}</span></span><ChevronDown class="size-3.5 text-zinc-500 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none" aria-hidden="true" /></span>
              </summary>
              <div class="max-h-72 overflow-auto border-t border-zinc-200 dark:border-zinc-800"><pre class="min-w-max py-1 font-mono text-[11px] leading-4"><code><span v-for="(line, index) in file.lines" :key="index" class="block whitespace-pre px-2.5" :class="line.type === 'add' ? 'bg-emerald-50 text-emerald-900 dark:bg-emerald-950/60 dark:text-emerald-200' : line.type === 'del' ? 'bg-red-50 text-red-900 dark:bg-red-950/60 dark:text-red-200' : line.type === 'meta' ? 'text-zinc-400 dark:text-zinc-500' : 'text-zinc-600 dark:text-zinc-400'">{{ line.text || " " }}</span></code></pre></div>
            </details>
          </li>
        </ul>
        <button v-if="diffs.length > PREVIEW_LIMIT" type="button" class="mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="showAll = !showAll">{{ showAll ? "Show fewer files" : `Show all ${diffs.length} files` }}</button>
        <RouterLink :to="{ name: 'session-diff', params: { id: sessionId } }" class="mt-3 inline-flex text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400">View legacy outcome patch</RouterLink>
      </template>
      <p v-if="!outcomeEvidence && !commitHistory.length" class="pt-3 text-sm text-zinc-500">No separate outcome evidence recorded.</p>
    </section>
  </div>
</template>
