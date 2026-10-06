<script setup lang="ts">
import { computed, nextTick, onUnmounted, reactive, ref, watch, type ComponentPublicInstance } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowLeft, ExternalLink, Search } from "lucide-vue-next";
import Button from "@/components/ui/Button.vue";
import Select from "@/components/ui/Select.vue";
import DiffViewer from "@/components/session/DiffViewer.vue";
import { errorMessage, getSession, getSessionGitArtifactPatch, listCommits, listCommitCaptures, listCommitRepositories, listCommitRefs, type CommitCapture, type CommitFilters, type CommitTimelineEntry, type CommitRepository, type CommitRef } from "@/lib/api";
import { gitArtifactCommitUrl, gitArtifactProvenance, shortCommit } from "@/lib/git";
import { shortDate } from "@/lib/format";
import { outcomeOptions, repositoryOption, type SelectOption } from "@/lib/options";

const route = useRoute(), router = useRouter();
const filterKeys = ["q", "repo", "ref", "capture_status", "session", "from", "to", "outcome"] as const;
const selectionKeys = ["selected_repo", "selected_commit", "capture_session"] as const;
const draft = reactive<Record<string, string>>(Object.fromEntries(filterKeys.map((key) => [key, ""])));
const advancedFields = [
  { key: "session", label: "Filter by session", placeholder: "Exact owning session ID" },
  { key: "from", label: "From (UTC)", placeholder: "2026-08-01T00:00:00Z" },
  { key: "to", label: "To (UTC)", placeholder: "2026-08-31T23:59:59Z" },
] as const;
const commits = ref<CommitTimelineEntry[]>([]), next = ref<string | null>(null);
const loading = ref(true), loadingMore = ref(false), error = ref("");
const repositories = ref<CommitRepository[]>([]), repositoryNext = ref<string | null>(null);
const repositoryLoading = ref(false), repositoryError = ref("");
const refs = ref<CommitRef[]>([]), refsNext = ref<string | null>(null), refsLoading = ref(false), refsError = ref("");
let repositoryController: AbortController | null = null, refsController: AbortController | null = null;
const selectedCommit = ref<CommitTimelineEntry | null>(null), selectedCapture = ref<CommitCapture | null>(null);
const selectionLoading = ref(false), selectionError = ref(""), patchLoading = ref(false), patchError = ref("");
const patch = ref<string | null>(null);
const captureLoading = ref<Record<string, boolean>>({}), captureErrors = ref<Record<string, string>>({});
const readerHeading = ref<HTMLElement | null>(null), listHeading = ref<HTMLElement | null>(null);
const commitLinks = new Map<string, HTMLElement>();
const pages = new Map<string, { commits: CommitTimelineEntry[]; next: string | null }>();
const knownCommits = new Map<string, CommitTimelineEntry>();
let listController: AbortController | null = null, selectionController: AbortController | null = null, patchController: AbortController | null = null;
const captureControllers = new Set<AbortController>();
const queryValue = (key: string) => typeof route.query[key] === "string" ? route.query[key] as string : "";
const filterSignature = computed(() => JSON.stringify(filterKeys.map(queryValue)));
const selectionSignature = computed(() => JSON.stringify(selectionKeys.map(queryValue)));
const hasSelection = computed(() => selectionKeys.some((key) => Boolean(queryValue(key))));
const filterCount = computed(() => filterKeys.filter((key) => !["q", "repo", "ref"].includes(key) && queryValue(key)).length);
const captureKey = computed(() => selectedCapture.value ? JSON.stringify([selectedCapture.value.session_id, selectedCapture.value.commit_sha, selectedCapture.value.patch_sha256]) : "");
const commitKey = (commit: CommitTimelineEntry) => JSON.stringify([commit.repository_key, commit.commit_sha]);
const repositoryLabel = (commit: CommitTimelineEntry) => commit.repository_key.startsWith("session:") ? "Unknown repository (session-scoped)" : commit.repository_key.replace(/^https?:\/\//, "");
const repositoryOptions = computed(() => repositories.value.map((repo) => repositoryOption(repo.repository_key, repo.name, `${repo.host || `Session ${repo.session_id}`} · ${repo.commit_count} ${repo.commit_count === 1 ? "commit" : "commits"}`)));
const refOptions = computed(() => [{ value: "", label: "All refs" }, ...refs.value.map((item) => ({ value: item.ref, label: item.ref, description: `${item.commit_count} ${item.commit_count === 1 ? "commit" : "commits"}` }))]);
const captureOptions: SelectOption[] = [{ value: "", label: "All captures" }, { value: "saved", label: "Saved" }, { value: "accepted", label: "Pending" }, { value: "failed", label: "Failed" }];
const captureOwnerOptions = computed(() => selectedCommit.value?.captures.map((capture) => ({ value: capture.session_id, label: capture.session_title, description: `${captureStatus(capture.capture_status)} · ${capture.session_id}` })) ?? []);
async function loadRepositories(append = false) {
  repositoryController?.abort();
  const active = new AbortController(); repositoryController = active; repositoryLoading.value = true; repositoryError.value = "";
  try {
    const result = await listCommitRepositories(append ? repositoryNext.value ?? undefined : undefined, active.signal);
    if (active.signal.aborted) return;
    repositories.value = append ? [...repositories.value, ...result.repositories] : result.repositories;
    repositoryNext.value = result.next_cursor;
    if (!queryValue("repo")) {
      const repo = queryValue("selected_repo") || repositories.value.find((item) => item.host)?.repository_key || repositories.value[0]?.repository_key;
      if (repo) await router.replace({ query: { ...route.query, repo } });
      else loading.value = false;
    }
  } catch (cause) { if (!active.signal.aborted) { repositoryError.value = errorMessage(cause, "Repositories could not be loaded."); loading.value = false; } }
  finally { if (!active.signal.aborted) repositoryLoading.value = false; }
}
async function loadRefs(append = false) {
  refsController?.abort();
  const repo = queryValue("repo");
  if (!repo) { refs.value = []; refsNext.value = null; refsLoading.value = false; refsError.value = ""; return; }
  const active = new AbortController(); refsController = active; refsLoading.value = true; refsError.value = "";
  if (!append) { refs.value = []; refsNext.value = null; }
  try {
    const result = await listCommitRefs(repo, append ? refsNext.value ?? undefined : undefined, active.signal);
    if (!active.signal.aborted) { refs.value = append ? [...refs.value, ...result.refs] : result.refs; refsNext.value = result.next_cursor; }
  } catch (cause) { if (!active.signal.aborted) refsError.value = errorMessage(cause, "Recorded refs could not be loaded."); }
  finally { if (!active.signal.aborted) refsLoading.value = false; }
}
function chooseRepository(value: string) {
  draft.repo = value;
  const query: typeof route.query = { ...route.query, repo: value };
  delete query.ref;
  void router.push({ query });
}
const captureStatus = (status: string) => status === "accepted" ? "Pending" : status === "saved" ? "Saved" : status === "failed" ? "Failed" : status === "skipped" ? "Skipped" : "Unavailable";
function bindCommitLink(commit: CommitTimelineEntry, element: Element | ComponentPublicInstance | null) {
  const link = element && "$el" in element ? element.$el : element;
  if (link instanceof HTMLElement) commitLinks.set(commitKey(commit), link);
  else commitLinks.delete(commitKey(commit));
}
function captureSummary(commit: CommitTimelineEntry) {
  const counts = new Map<string, number>();
  for (const capture of commit.captures) { const label = captureStatus(capture.capture_status); counts.set(label, (counts.get(label) ?? 0) + 1); }
  return [...counts].map(([label, count]) => `${count} ${label.toLowerCase()}`).join(" · ");
}
function filters(cursor?: string): CommitFilters {
  const value = (key: string) => queryValue(key) || undefined;
  return { q: value("q"), repo: value("repo"), ref: value("ref"), session: value("session"), from: value("from"), to: value("to"), outcome: value("outcome") as CommitFilters["outcome"], captureStatus: value("capture_status") as CommitFilters["captureStatus"], cursor, limit: 25 };
}
function apply(clear = false) {
  const query = { ...route.query };
  for (const key of filterKeys) { if (key === "repo" || key === "ref") continue; delete query[key]; if (!clear && draft[key]?.trim()) query[key] = draft[key]!.trim(); }
  void router.push({ query });
}
function selectionLocation(commit: CommitTimelineEntry, capture?: CommitCapture) {
  const exact = capture ?? commit.captures.find((item) => item.capture_status === "saved") ?? commit.captures[0];
  return { query: { ...route.query, selected_repo: commit.repository_key, selected_commit: commit.commit_sha, capture_session: exact?.session_id ?? "" } };
}
async function backToCommits() {
  const key = JSON.stringify([queryValue("selected_repo"), queryValue("selected_commit")]);
  const query = { ...route.query }; for (const name of selectionKeys) delete query[name];
  await router.push({ query }); await nextTick();
  (commitLinks.get(key) ?? listHeading.value)?.focus();
}
async function load(append = false) {
  listController?.abort();
  if (!queryValue("repo")) { commits.value = []; next.value = null; loading.value = repositoryLoading.value; return; }
  const active = new AbortController(); listController = active;
  const signature = filterSignature.value;
  if (append) loadingMore.value = true;
  else { loading.value = true; loadingMore.value = false; commits.value = []; next.value = null; }
  error.value = "";
  try {
    const result = await listCommits(filters(append ? next.value ?? undefined : undefined), active.signal);
    if (active.signal.aborted) return;
    commits.value = append ? [...commits.value, ...result.commits] : result.commits;
    next.value = result.next_cursor;
    for (const commit of result.commits) if (!knownCommits.has(commitKey(commit))) knownCommits.set(commitKey(commit), commit);
    pages.set(signature, { commits: commits.value, next: next.value });
  } catch (cause) { if (!active.signal.aborted) error.value = errorMessage(cause, "Captured commits could not be loaded."); }
  finally { if (!active.signal.aborted) { loading.value = false; loadingMore.value = false; } }
}
async function resolveSelection() {
  selectionController?.abort(); patchController?.abort();
  selectedCommit.value = null; selectedCapture.value = null; patch.value = null;
  selectionError.value = ""; patchError.value = ""; patchLoading.value = false; selectionLoading.value = false;
  if (!hasSelection.value) return;
  const repo = queryValue("selected_repo"), sha = queryValue("selected_commit"), session = queryValue("capture_session");
  if (!repo || !/^[0-9a-f]{40}$/.test(sha) || !session) { selectionError.value = "This selection needs a repository, full commit SHA, and exact capture session. Choose a commit to continue."; return; }
  const active = new AbortController(); selectionController = active; selectionLoading.value = true;
  try {
    const key = JSON.stringify([repo, sha]);
    let commit = commits.value.find((item) => commitKey(item) === key) ?? knownCommits.get(key);
    let capture = commit?.captures.find((item) => item.session_id === session);
    if (!commit || !capture) {
      // This independent lookup restores old commits without touching the timeline or its cursor.
      const result = await listCommits({ repo, q: sha, session, limit: 25 }, active.signal);
      if (active.signal.aborted) return;
      const exact = result.commits.find((item) => item.repository_key === repo && item.commit_sha === sha);
      if (!exact) throw new Error("The selected commit capture is no longer available in this repository and session.");
      commit ??= exact;
      capture = commit.captures.find((item) => item.session_id === session) ?? exact.captures.find((item) => item.session_id === session);
      commit = reactive(commit);
      selectedCommit.value = commit;
      if (!capture) {
        // The association was verified above. Session detail can recover a capture beyond the first 50.
        const detail = await getSession(session, active.signal);
        if (active.signal.aborted) return;
        const artifact = detail.git_artifacts.find((item) => item.commit_sha === sha);
        if (!artifact || detail.session.id !== session) throw new Error("The exact owning session capture could not be restored. Choose an available alternate capture below.");
        capture = { ...artifact, session_id: session, session_title: detail.session.display_title ?? detail.session.title ?? detail.session.intent ?? session, repo: detail.session.repo, outcome: detail.session.outcome };
      }
      if (!commit.captures.some((item) => item.session_id === session)) commit.captures.push(capture);
      if (commit.captures.length >= commit.capture_count) commit.captures_next_cursor = null;
    }
    if (active.signal.aborted) return;
    if (!commit || !capture) throw new Error("The exact commit capture is unavailable. Choose an available capture from the timeline.");
    knownCommits.set(key, commit); selectedCommit.value = commit; selectedCapture.value = capture;
  } catch (cause) { if (!active.signal.aborted) selectionError.value = errorMessage(cause, "The selected capture could not be restored."); }
  finally { if (!active.signal.aborted) selectionLoading.value = false; }
}
async function loadPatch() {
  patchController?.abort(); patch.value = null; patchError.value = ""; patchLoading.value = false;
  const capture = selectedCapture.value;
  if (!capture || capture.capture_status !== "saved") return;
  const active = new AbortController(); patchController = active; patchLoading.value = true;
  try {
    const value = await getSessionGitArtifactPatch(capture.session_id, capture.commit_sha, active.signal);
    if (!active.signal.aborted) patch.value = value;
  } catch (cause) { if (!active.signal.aborted) patchError.value = errorMessage(cause, "The exact saved patch could not be loaded."); }
  finally { if (!active.signal.aborted) patchLoading.value = false; }
}
async function moreCaptures(commit: CommitTimelineEntry) {
  const key = commitKey(commit);
  if (captureLoading.value[key] || !commit.captures_next_cursor) return;
  const active = new AbortController(); captureControllers.add(active); captureLoading.value[key] = true; captureErrors.value[key] = "";
  try {
    const result = await listCommitCaptures(commit.repository_key, commit.commit_sha, commit.captures_next_cursor, active.signal);
    if (!active.signal.aborted) {
      for (const capture of result.captures) if (!commit.captures.some((item) => item.session_id === capture.session_id)) commit.captures.push(capture);
      commit.captures_next_cursor = result.next_cursor;
    }
  } catch (cause) { if (!active.signal.aborted) captureErrors.value[key] = errorMessage(cause, "More capture associations could not be loaded."); }
  finally { captureControllers.delete(active); if (!active.signal.aborted) captureLoading.value[key] = false; }
}
watch(filterSignature, () => {
  for (const key of filterKeys) draft[key] = queryValue(key);
  listController?.abort(); error.value = "";
  const cached = pages.get(filterSignature.value);
  if (cached) { commits.value = cached.commits; next.value = cached.next; loading.value = false; loadingMore.value = false; }
  else if (queryValue("repo")) void load();
  else {
    commits.value = []; next.value = null; loading.value = repositoryLoading.value;
    if (repositories.value.length) {
      const repo = queryValue("selected_repo") || repositories.value.find((item) => item.host)?.repository_key || repositories.value[0]!.repository_key;
      void router.replace({ query: { ...route.query, repo } });
    }
  }
}, { immediate: true });
watch(() => queryValue("repo"), () => { void loadRefs(); }, { immediate: true });
void loadRepositories();
watch(selectionSignature, async (signature, previous) => {
  await resolveSelection();
  if (signature !== selectionSignature.value) return;
  await nextTick();
  if (hasSelection.value) readerHeading.value?.focus({ preventScroll: true });
  else if (previous) {
    const [repo, sha] = JSON.parse(previous) as string[];
    (commitLinks.get(JSON.stringify([repo, sha])) ?? listHeading.value)?.focus();
  }
}, { immediate: true });
watch(captureKey, () => { void loadPatch(); });
onUnmounted(() => { listController?.abort(); selectionController?.abort(); patchController?.abort(); repositoryController?.abort(); refsController?.abort(); for (const request of captureControllers) request.abort(); });
</script>

<template>
  <section class="min-w-0 pb-8">
    <template v-if="!hasSelection">
      <header class="mb-5"><h1 class="text-2xl font-semibold tracking-tight">Commits</h1></header>
      <div class="mb-4 flex flex-wrap items-end gap-3">
        <div class="w-full min-w-0 text-xs text-zinc-600 sm:w-80 dark:text-zinc-400"><span class="mb-1 block">Repository</span>
          <Select :model-value="draft.repo ?? ''" label="Repository" :options="repositoryOptions" :disabled="repositoryLoading && !repositories.length" :placeholder="repositoryLoading ? 'Loading repositories…' : 'Choose a repository'" class="h-9 w-full" @update:model-value="chooseRepository" />
        </div>
        <div class="w-full min-w-0 text-xs text-zinc-600 sm:w-56 dark:text-zinc-400"><span class="mb-1 block">Recorded ref</span>
          <Select :model-value="draft.ref ?? ''" label="Recorded ref" :options="refOptions" searchable class="h-9 w-full" @update:model-value="router.push({ query: { ...route.query, ref: $event || undefined } })" />
        </div>
      <form class="w-full sm:w-auto" role="search" @submit.prevent="apply()">
        <div class="flex flex-wrap items-center gap-2">
          <label class="relative min-w-0 flex-1 sm:w-72 sm:flex-none"><span class="sr-only">Search commits</span><Search class="pointer-events-none absolute left-2.5 top-2.5 size-4 text-zinc-500" aria-hidden="true" /><input v-model="draft.q" type="search" placeholder="Search subject, SHA or session" class="h-9 w-full rounded-[5px] border border-zinc-300 bg-transparent pl-9 pr-3 text-[13px] focus:outline-2 focus:outline-teal-600 dark:border-zinc-700" /></label>
          <Button type="submit" variant="outline">Search</Button>
          <Button v-if="filterCount || route.query.q" type="button" variant="ghost" @click="apply(true)">Clear filters</Button>
          <details class="basis-full sm:relative sm:basis-auto"><summary class="w-fit cursor-pointer rounded-[5px] px-2 py-2 text-xs text-zinc-700 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-zinc-300">Filters{{ filterCount ? ` (${filterCount})` : '' }}</summary>
            <div class="mt-1 grid w-full gap-3 rounded-[5px] border border-zinc-300 bg-stone-50 p-4 shadow-sm sm:absolute sm:right-0 sm:z-10 sm:w-[28rem] sm:grid-cols-2 dark:border-zinc-700 dark:bg-stone-900">
              <div class="text-xs text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Capture</span><Select v-model="draft.capture_status" label="Capture" :options="captureOptions" class="h-9 w-full" /></div>
              <div class="text-xs text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Work outcome</span><Select v-model="draft.outcome" label="Work outcome" :options="outcomeOptions" class="h-9 w-full" /></div>
              <label v-for="field in advancedFields" :key="field.key" class="min-w-0 text-xs text-zinc-600 dark:text-zinc-400">{{ field.label }}<input v-model="draft[field.key]" :placeholder="field.placeholder" class="mt-1 h-9 w-full rounded-[5px] border border-zinc-300 bg-transparent px-2 text-[13px] text-zinc-900 focus:outline-2 focus:outline-teal-600 dark:border-zinc-700 dark:text-zinc-100" /></label>
              <Button type="submit" variant="outline" class="self-end" @click="($event.currentTarget as HTMLElement).closest('details')?.removeAttribute('open')">Apply filters</Button>
            </div>
          </details>
        </div>
      </form>
        <Button v-if="repositoryNext" variant="ghost" :disabled="repositoryLoading" @click="loadRepositories(true)">{{ repositoryLoading ? 'Loading…' : 'More repositories' }}</Button>
        <Button v-if="refsNext" variant="ghost" :disabled="refsLoading" @click="loadRefs(true)">{{ refsLoading ? 'Loading…' : 'More refs' }}</Button>
      </div>
      <div v-if="repositoryError" role="alert" class="mb-3 text-sm">{{ repositoryError }} <Button variant="ghost" @click="loadRepositories(Boolean(repositories.length))">Retry repositories</Button></div>
      <div v-if="refsError" role="alert" class="mb-3 text-sm">{{ refsError }} <Button variant="ghost" @click="loadRefs">Retry refs</Button></div>
      <section aria-labelledby="commit-list-heading">
        <div class="mb-2 flex items-baseline justify-between"><h2 id="commit-list-heading" ref="listHeading" tabindex="-1" class="text-sm font-semibold focus-visible:outline-2 focus-visible:outline-teal-600">Captured commits</h2><span class="text-xs text-zinc-600 dark:text-zinc-400">Newest first</span></div>
        <div v-if="loading || repositoryLoading && !draft.repo" aria-busy="true" aria-label="Loading commits" class="space-y-4 border-t border-zinc-200 py-5 dark:border-zinc-800"><div v-for="row in 4" :key="row" class="h-5 w-3/4 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div>
        <div v-if="error" role="alert" class="py-4 text-sm"><p>{{ error }}</p><Button variant="outline" class="mt-2" @click="load(Boolean(commits.length))">Retry commits</Button></div>
        <p v-if="!loading && !repositoryLoading && !error && !commits.length" class="py-8 text-sm text-zinc-600 dark:text-zinc-400">{{ draft.repo ? 'No captured commits match. Clear filters or choose another ref.' : 'No repositories have captured commits yet.' }}</p>
        <div v-if="commits.length" class="overflow-x-auto">
          <table class="w-full border-collapse text-left text-[13px]">
            <caption class="sr-only">Captured commits for the selected repository</caption>
            <thead class="border-y border-zinc-200 text-xs text-zinc-600 dark:border-zinc-800 dark:text-zinc-400"><tr><th scope="col" class="py-2 pr-4 font-medium">Commit</th><th scope="col" class="hidden px-3 py-2 font-medium sm:table-cell">Recorded</th><th scope="col" class="hidden px-3 py-2 font-medium md:table-cell">Changes</th><th scope="col" class="hidden py-2 pl-3 font-medium lg:table-cell">Sessions</th></tr></thead>
            <tbody class="divide-y divide-zinc-200 dark:divide-zinc-800"><tr v-for="commit in commits" :key="commitKey(commit)" class="hover:bg-stone-200/50 dark:hover:bg-stone-900">
              <td class="py-3 pr-4"><RouterLink :ref="(element) => bindCommitLink(commit, element)" :to="selectionLocation(commit)" class="block rounded-sm font-medium focus-visible:outline-2 focus-visible:outline-teal-600"><span class="break-words">{{ commit.subject || 'Untitled commit' }}</span><span class="mt-1 block text-xs font-normal text-zinc-600 dark:text-zinc-400"><code>{{ shortCommit(commit.commit_sha) }}</code><span class="ml-3">{{ captureSummary(commit) }}</span><span class="ml-3 sm:hidden">{{ shortDate(commit.committed_at || commit.captures[0]?.created_at || '') }}</span><span class="ml-3 md:hidden">{{ commit.patch_files }} files · +{{ commit.patch_additions }} −{{ commit.patch_deletions }}</span></span></RouterLink><div class="mt-1 flex flex-wrap gap-x-3 text-xs lg:hidden"><RouterLink v-for="capture in commit.captures.slice(0, 2)" :key="capture.session_id" :to="`/sessions/${encodeURIComponent(capture.session_id)}`" class="break-words text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ capture.session_title }}</RouterLink><span v-if="commit.capture_count > 2" class="text-zinc-600 dark:text-zinc-400">+{{ commit.capture_count - 2 }} more</span></div></td>
              <td class="hidden whitespace-nowrap px-3 py-3 text-xs text-zinc-600 sm:table-cell dark:text-zinc-400"><time :datetime="commit.committed_at || commit.captures[0]?.created_at">{{ shortDate(commit.committed_at || commit.captures[0]?.created_at || '') }}</time></td>
              <td class="hidden whitespace-nowrap px-3 py-3 text-xs md:table-cell">{{ commit.patch_files }} files <span class="ml-2 font-mono text-emerald-700 dark:text-emerald-400">+{{ commit.patch_additions }}</span> <span class="font-mono text-red-700 dark:text-red-400">−{{ commit.patch_deletions }}</span></td>
              <td class="hidden max-w-64 py-3 pl-3 text-xs lg:table-cell"><RouterLink v-for="capture in commit.captures.slice(0, 2)" :key="capture.session_id" :to="`/sessions/${encodeURIComponent(capture.session_id)}`" class="block truncate text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ capture.session_title }}</RouterLink><span v-if="commit.capture_count > 2" class="text-zinc-600 dark:text-zinc-400">+{{ commit.capture_count - 2 }} more</span></td>
            </tr></tbody>
          </table>
        </div>
        <Button v-if="next" variant="outline" class="mt-4" :disabled="loadingMore" @click="load(true)">{{ loadingMore ? 'Loading…' : 'Load more commits' }}</Button>
      </section>
    </template>
    <section v-else aria-labelledby="capture-reader-heading" class="min-w-0" :aria-busy="selectionLoading || patchLoading">
      <button type="button" class="mb-4 inline-flex items-center gap-1.5 text-xs text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="backToCommits"><ArrowLeft class="size-3.5" aria-hidden="true" />Back to commits</button>
      <p v-if="selectionLoading" aria-live="polite" class="py-6 text-sm text-zinc-600 dark:text-zinc-400">Loading selected capture…</p>
      <div v-if="selectionError" role="alert" class="py-4 text-sm"><p>{{ selectionError }}</p><Button variant="outline" class="mt-2" @click="resolveSelection">Retry selection</Button></div>
      <template v-if="selectedCommit">
        <header class="min-w-0 border-b border-zinc-200 pb-4 dark:border-zinc-800">
          <h1 id="capture-reader-heading" ref="readerHeading" tabindex="-1" class="break-words text-2xl font-semibold leading-8 tracking-tight focus-visible:outline-2 focus-visible:outline-teal-600">{{ selectedCapture?.subject || selectedCommit.subject || 'Untitled commit' }}</h1>
          <div class="mt-2 flex flex-wrap items-baseline gap-x-3 gap-y-1 text-xs text-zinc-600 dark:text-zinc-400"><code>{{ shortCommit(selectedCommit.commit_sha) }}</code><span class="break-all">{{ repositoryLabel(selectedCommit) }}</span><span>{{ selectedCapture?.ref || selectedCommit.ref || 'Ref not recorded' }}</span></div>
          <div v-if="selectedCapture" class="mt-3 flex flex-wrap items-baseline gap-x-4 gap-y-2 text-[13px]"><RouterLink :to="`/sessions/${encodeURIComponent(selectedCapture.session_id)}`" class="min-w-0 break-words text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">{{ selectedCapture.session_title }}</RouterLink><span class="text-xs text-zinc-600 dark:text-zinc-400">{{ captureStatus(selectedCapture.capture_status) }} · {{ selectedCapture.patch_files }} files <span class="font-mono text-emerald-700 dark:text-emerald-400">+{{ selectedCapture.patch_additions }}</span> <span class="font-mono text-red-700 dark:text-red-400">−{{ selectedCapture.patch_deletions }}</span></span><a v-if="gitArtifactCommitUrl(selectedCapture)" :href="gitArtifactCommitUrl(selectedCapture)!" target="_blank" rel="noreferrer noopener" class="text-xs text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">Repository commit <ExternalLink class="inline size-3" aria-hidden="true" /><span class="sr-only"> (new tab)</span></a></div>
        </header>
        <div v-if="selectedCommit.capture_count > 1" class="flex flex-wrap items-end gap-2 border-b border-zinc-200 py-3 dark:border-zinc-800">
          <div class="min-w-0 flex-1 basis-64 text-xs text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Captured by</span><Select :model-value="selectedCapture?.session_id || queryValue('capture_session')" label="Captured by" :options="captureOwnerOptions" searchable class="h-9 w-full" @update:model-value="router.push({ query: { ...route.query, capture_session: $event } })" /></div>
          <Button v-if="selectedCommit.captures_next_cursor" variant="outline" :disabled="captureLoading[commitKey(selectedCommit)]" @click="moreCaptures(selectedCommit)">{{ captureLoading[commitKey(selectedCommit)] ? 'Loading…' : `More captures (${selectedCommit.captures.length}/${selectedCommit.capture_count})` }}</Button>
          <p v-if="captureErrors[commitKey(selectedCommit)]" role="alert" class="basis-full text-xs">{{ captureErrors[commitKey(selectedCommit)] }}</p>
        </div>
        <template v-if="selectedCapture && !selectionLoading">
          <p v-if="selectedCapture.capture_status !== 'saved'" role="status" class="max-w-prose py-6 text-sm leading-6 text-zinc-600 dark:text-zinc-400">{{ captureStatus(selectedCapture.capture_status) }}: this session has no saved patch.<template v-if="selectedCapture.failure_code"> Failure: {{ selectedCapture.failure_code }}.</template><template v-if="selectedCommit.capture_count > 1"> Choose another capture above to inspect its patch.</template> No alternate patch is substituted.</p>
          <p v-else-if="patchLoading" aria-live="polite" class="py-6 text-sm text-zinc-600 dark:text-zinc-400">Loading exact captured patch…</p>
          <div v-else-if="patchError" role="alert" class="py-4 text-sm"><p>{{ patchError }}</p><Button variant="outline" class="mt-2" @click="loadPatch">Retry exact patch</Button></div>
          <DiffViewer v-else-if="patch !== null" :key="captureKey" :patch="patch" />
        </template>
        <details :key="captureKey || commitKey(selectedCommit)" class="mt-5 border-y border-zinc-200 py-3 dark:border-zinc-800">
          <summary class="cursor-pointer text-xs text-zinc-600 focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-zinc-400">Details</summary>
          <dl class="mt-3 grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-2 text-xs"><dt>Full commit SHA</dt><dd class="break-all font-mono">{{ selectedCommit.commit_sha }}</dd><template v-if="selectedCapture"><dt>Owning session</dt><dd class="break-all font-mono">{{ selectedCapture.session_id }}</dd><dt>Patch SHA-256</dt><dd class="break-all font-mono">{{ selectedCapture.patch_sha256 || 'Not recorded' }}</dd><dt>Provenance</dt><dd>{{ gitArtifactProvenance(selectedCapture.provenance).label }}</dd><dt>Accepted</dt><dd>{{ shortDate(selectedCapture.accepted_at) }}</dd><dt v-if="selectedCapture.saved_at">Saved</dt><dd v-if="selectedCapture.saved_at">{{ shortDate(selectedCapture.saved_at) }}</dd><dt v-if="selectedCapture.failed_at">Failed</dt><dd v-if="selectedCapture.failed_at">{{ shortDate(selectedCapture.failed_at) }}</dd><dt>Work outcome</dt><dd>{{ selectedCapture.outcome }}</dd><dt>Capture bytes</dt><dd>{{ selectedCapture.patch_bytes.toLocaleString() }}</dd></template></dl>
          <p class="mt-3 max-w-prose text-xs leading-5 text-zinc-600 dark:text-zinc-400">Each session owns its patch. Local capture does not establish a push, merge or landing.</p>
          <RouterLink v-if="selectedCapture?.capture_status === 'saved'" :to="{ name: 'session-artifact-diff', params: { id: selectedCapture.session_id, commit: selectedCapture.commit_sha } }" class="mt-3 inline-block text-xs text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">Standalone diff</RouterLink>
          <details v-if="patch !== null" class="mt-3"><summary class="cursor-pointer text-xs focus-visible:outline-2 focus-visible:outline-teal-600">Exact raw patch, including binary payloads</summary><pre tabindex="0" aria-label="Exact raw captured patch" class="mt-3 max-h-96 max-w-full overflow-auto bg-stone-100 p-3 font-mono text-xs leading-5 focus-visible:outline-2 focus-visible:outline-teal-600 dark:bg-stone-900">{{ patch }}</pre></details>
        </details>
      </template>
    </section>
  </section>
</template>
