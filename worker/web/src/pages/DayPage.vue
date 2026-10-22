<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ChevronDown, ChevronLeft, ChevronRight, GitBranch, GitCommitHorizontal, RotateCw } from "lucide-vue-next";
import Button from "@/components/ui/Button.vue";
import Select from "@/components/ui/Select.vue";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import { dashboardNow, errorMessage, getDay, setSessionsOutcome, type DaySession, type DaySnapshot, type DayWorkstream, type Outcome } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { compactNumber, harnessLabel } from "@/lib/format";
import { outcomeOptions } from "@/lib/options";
import { outcomeMeta, outcomeOrder } from "@/lib/outcomes";

const route = useRoute();
const router = useRouter();
const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
const localDate = new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" });
const today = ref(localDate.format(dashboardNow()));
const date = computed(() => (typeof route.params.date === "string" && route.params.date) || today.value);

const snapshot = ref<DaySnapshot | null>(null);
const loading = ref(true);
const error = ref("");
const quietOpen = ref(false);
const selected = ref(new Set<string>());
const bulkOutcome = ref<Outcome>("landed");
const bulkApplying = ref(false);
const bulkError = ref("");
let controller: AbortController | null = null;

async function load(silent = false) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  if (!silent) loading.value = true;
  error.value = "";
  try {
    today.value = localDate.format(dashboardNow());
    snapshot.value = await getDay(date.value, timeZone, active.signal);
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "This day could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

watch(date, () => {
  snapshot.value = null;
  quietOpen.value = false;
  selected.value = new Set();
  void load();
}, { immediate: true });
useAutoRefresh(() => (date.value === today.value ? load(true) : undefined));
onBeforeUnmount(() => controller?.abort());

const sessions = computed(() => new Map((snapshot.value?.sessions ?? []).map((session) => [session.id, session])));
const lookup = (ids: string[]) => ids.flatMap((id) => sessions.value.get(id) ?? []);

const heading = computed(() => {
  if (date.value === today.value) return "Today";
  const yesterday = new Date(`${today.value}T12:00:00Z`);
  yesterday.setUTCDate(yesterday.getUTCDate() - 1);
  if (date.value === yesterday.toISOString().slice(0, 10)) return "Yesterday";
  return new Intl.DateTimeFormat("en", { weekday: "long", month: "long", day: "numeric", timeZone: "UTC" }).format(new Date(`${date.value}T12:00:00Z`));
});
const calendarLabel = computed(() => new Intl.DateTimeFormat("en", { weekday: "long", year: "numeric", month: "long", day: "numeric", timeZone: "UTC" }).format(new Date(`${date.value}T12:00:00Z`)));

const plural = (count: number, one: string, many = `${one}s`) => `${compactNumber(count)} ${count === 1 ? one : many}`;
const totalsLine = computed(() => {
  const totals = snapshot.value?.totals;
  if (!totals) return "";
  const working = totals.sessions - totals.quiet_sessions;
  return [
    working ? plural(working, "session") : "",
    totals.quiet_sessions ? plural(totals.quiet_sessions, "quick session") : "",
    totals.sub_agents ? plural(totals.sub_agents, "sub-agent") : "",
    totals.requests ? plural(totals.requests, "request") : "",
    totals.tokens_in + totals.tokens_out ? `${compactNumber(totals.tokens_in + totals.tokens_out)} tokens` : "",
    totals.commits ? plural(totals.commits, "commit") : "",
    totals.repos ? plural(totals.repos, "repository", "repositories") : "",
  ].filter(Boolean).join(" · ");
});

const clock = computed(() => new Intl.DateTimeFormat("en", { timeZone, hour: "2-digit", minute: "2-digit", hourCycle: "h23" }));
function span(first: string, last: string) {
  const from = clock.value.format(new Date(first)), to = clock.value.format(new Date(last));
  return from === to ? from : `${from}–${to}`;
}

// Hour ticks come from real local wall-clock time so 23- and 25-hour days stay exact.
const ticks = computed(() => {
  const day = snapshot.value;
  if (!day) return [];
  const hour = new Intl.DateTimeFormat("en", { timeZone, hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
  const start = Date.parse(day.start);
  const result: Array<{ bucket: number; label: string }> = [];
  for (let bucket = 0; bucket < day.buckets; bucket++) {
    const label = hour.format(new Date(start + bucket * day.bucket_minutes * 60_000));
    if (label.endsWith(":00") && Number(label.slice(0, 2)) % 6 === 0) result.push({ bucket, label: label.slice(0, 2) });
  }
  return result;
});
const peak = computed(() => Math.max(1, ...(snapshot.value?.sessions ?? []).flatMap((session) => session.activity.map(([, count]) => count))));
const barHeight = (count: number) => 3 + 17 * Math.sqrt(Math.min(1, count / peak.value));

function streamActivity(stream: DayWorkstream) {
  const merged = new Map<number, number>();
  for (const session of lookup(stream.session_ids)) for (const [bucket, count] of session.activity) merged.set(bucket, (merged.get(bucket) ?? 0) + count);
  return [...merged.entries()];
}

function outcomeLine(stream: DayWorkstream) {
  return outcomeOrder.filter((outcome) => stream.outcomes[outcome]).map((outcome) => `${stream.outcomes[outcome]} ${outcomeMeta[outcome].label.toLowerCase()}`).join(" · ");
}

const unresolved = computed(() => lookup(snapshot.value?.attention.unresolved ?? []));
const inProgress = computed(() => lookup(snapshot.value?.attention.active ?? []));
const captureProblems = computed(() => lookup(snapshot.value?.attention.capture ?? []));
const quiet = computed(() => lookup(snapshot.value?.quiet_session_ids ?? []));
const allUnresolvedSelected = computed(() => unresolved.value.length > 0 && unresolved.value.every((session) => selected.value.has(session.id)));
const settableOutcomes = outcomeOptions.filter((option) => option.value && option.value !== "unresolved");

function toggle(id: string) {
  const next = new Set(selected.value);
  if (!next.delete(id)) next.add(id);
  selected.value = next;
}
function toggleAll() {
  selected.value = allUnresolvedSelected.value ? new Set() : new Set(unresolved.value.map((session) => session.id));
}
async function applyOutcome() {
  bulkApplying.value = true;
  bulkError.value = "";
  try {
    await setSessionsOutcome([...selected.value], bulkOutcome.value, "");
    selected.value = new Set();
    await load(true);
  } catch (cause) {
    bulkError.value = errorMessage(cause, "Outcomes could not be saved.");
  } finally {
    bulkApplying.value = false;
  }
}

function go(target: string | null) {
  if (target) void router.push(target === today.value ? "/days" : `/days/${target}`);
}
const titleOf = (session: DaySession) => session.display_title ?? "Untitled session";
</script>

<template>
  <section>
    <div class="mb-7 flex flex-wrap items-end justify-between gap-4">
      <div class="min-w-0">
        <h1 class="text-[28px] font-semibold tracking-[-0.025em]">{{ heading }}</h1>
        <p class="mt-1.5 text-sm text-zinc-600 dark:text-zinc-400"><span v-if="heading !== calendarLabel.replace(/, \d{4}$/, '')">{{ calendarLabel }} · </span>{{ timeZone }}</p>
        <p v-if="snapshot && snapshot.totals.sessions" class="mt-1 text-sm text-zinc-800 dark:text-zinc-200">{{ totalsLine }}</p>
      </div>
      <nav aria-label="Day navigation" class="flex items-center gap-2">
        <Button variant="outline" size="icon" aria-label="Previous active day" :disabled="!snapshot?.previous_date" @click="go(snapshot?.previous_date ?? null)"><ChevronLeft class="size-4" aria-hidden="true" /></Button>
        <label class="relative"><span class="sr-only">Choose a day</span><input type="date" :value="date" :max="today" class="h-8.5 rounded-[5px] border border-zinc-300 bg-white px-2.5 font-mono text-xs text-zinc-800 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200 dark:[color-scheme:dark]" @change="go(($event.target as HTMLInputElement).value || null)" /></label>
        <Button variant="outline" size="icon" aria-label="Next active day" :disabled="!snapshot?.next_date" @click="go(snapshot?.next_date ?? null)"><ChevronRight class="size-4" aria-hidden="true" /></Button>
        <Button v-if="date !== today" variant="ghost" @click="go(today)">Today</Button>
      </nav>
    </div>

    <div v-if="loading && !snapshot" aria-busy="true" aria-label="Loading day">
      <div v-for="index in 4" :key="index" class="grid gap-3 border-t border-zinc-200 py-5 md:grid-cols-[minmax(0,1fr)_minmax(200px,38%)] dark:border-zinc-800"><div class="h-4 w-3/5 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-5 animate-pulse bg-zinc-100 motion-reduce:animate-none dark:bg-zinc-900" /></div>
    </div>
    <div v-else-if="error && !snapshot" class="border-y border-zinc-200 py-16 text-center dark:border-zinc-800" role="alert">
      <p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Day unavailable</p>
      <p class="mx-auto mt-1 max-w-md text-sm text-zinc-500">{{ error }}</p>
      <button type="button" class="mt-4 inline-flex items-center gap-2 text-sm font-medium text-teal-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="load()"><RotateCw class="size-4" aria-hidden="true" />Retry</button>
    </div>
    <div v-else-if="snapshot && !snapshot.totals.sessions" class="border-y border-zinc-200 py-16 text-center dark:border-zinc-800">
      <p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">No agent activity on this day</p>
      <p class="mt-1 text-sm text-zinc-500"><button v-if="snapshot.previous_date" type="button" class="font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="go(snapshot.previous_date)">Go to the previous active day</button><span v-else>Captured sessions will appear here by local day.</span></p>
    </div>

    <div v-else-if="snapshot" class="grid gap-10 xl:grid-cols-[minmax(0,1fr)_300px]">
      <div class="min-w-0">
        <div class="mb-2 flex items-baseline justify-between gap-4">
          <h2 class="text-base font-semibold">Workstreams</h2>
          <p class="text-xs text-zinc-500 dark:text-zinc-400">Grouped by repository, branch and continued titles</p>
        </div>
        <p v-if="!snapshot.workstreams.length" class="border-t border-zinc-200 py-4 text-sm text-zinc-600 dark:border-zinc-800 dark:text-zinc-400">Only quick sessions were recorded on this day.</p>
        <div v-else class="hidden border-t border-zinc-200 py-1.5 md:grid md:grid-cols-[minmax(0,1fr)_minmax(200px,38%)] md:gap-6 dark:border-zinc-800" aria-hidden="true">
          <span />
          <div class="relative h-4 font-mono text-[10px] text-zinc-500">
            <span v-for="tick in ticks" :key="tick.bucket" class="absolute -translate-x-1/2 first:translate-x-0" :style="{ left: `${(tick.bucket / snapshot.buckets) * 100}%` }">{{ tick.label }}</span>
          </div>
        </div>
        <ol v-if="snapshot.workstreams.length" class="border-b border-zinc-200 dark:border-zinc-800">
          <li v-for="stream in snapshot.workstreams" :key="stream.id" class="border-t border-zinc-200 py-4 dark:border-zinc-800">
            <div class="grid gap-3 md:grid-cols-[minmax(0,1fr)_minmax(200px,38%)] md:items-start md:gap-6">
              <div class="min-w-0">
                <div class="flex min-w-0 items-center gap-2">
                  <RouterLink v-if="stream.session_ids.length === 1" :to="`/sessions/${stream.id}`" class="min-w-0 truncate text-sm font-medium text-zinc-950 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-100">{{ stream.label }}</RouterLink>
                  <h3 v-else class="min-w-0 truncate text-sm font-medium text-zinc-950 dark:text-zinc-100">{{ stream.label }}</h3>
                  <span v-if="stream.active" class="shrink-0 rounded-[4px] border border-emerald-200 bg-emerald-50 px-1.5 text-[11px] font-medium text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300">In progress</span>
                </div>
                <div class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-500 dark:text-zinc-400">
                  <span class="font-medium text-zinc-700 dark:text-zinc-300">{{ stream.repo || "No repository" }}</span>
                  <span v-if="stream.source_ref" class="inline-flex items-center gap-1"><GitBranch class="size-3" aria-hidden="true" />{{ stream.source_ref }}</span>
                  <span class="font-mono">{{ span(stream.first_at, stream.last_at) }}</span>
                  <span>{{ plural(stream.requests, "request") }}</span>
                  <span v-if="stream.sub_agents">{{ plural(stream.sub_agents, "sub-agent") }}</span>
                  <span v-if="stream.session_ids.length > 1">{{ outcomeLine(stream) }}</span>
                  <template v-else><span v-for="session in lookup(stream.session_ids)" :key="session.id">{{ harnessLabel(session.harness) }}</span></template>
                </div>
              </div>
              <svg :viewBox="`0 0 ${snapshot.buckets} 20`" preserveAspectRatio="none" class="h-5 w-full text-zinc-800 dark:text-zinc-300" role="img" :aria-label="`Activity ${span(stream.first_at, stream.last_at)}, ${plural(stream.requests, 'request')}`">
                <line v-for="tick in ticks" :key="tick.bucket" :x1="tick.bucket" :x2="tick.bucket" y1="0" y2="20" class="stroke-zinc-200 dark:stroke-zinc-800" vector-effect="non-scaling-stroke" />
                <rect v-for="[bucket, count] in streamActivity(stream)" :key="bucket" :x="bucket + 0.12" width="0.76" :y="20 - barHeight(count)" :height="barHeight(count)" fill="currentColor" />
              </svg>
            </div>
            <OutcomeBadge v-if="stream.session_ids.length === 1 && stream.outcomes.unresolved === 0" :outcome="outcomeOrder.find((outcome) => stream.outcomes[outcome])!" class="mt-2" />
            <ol v-if="stream.session_ids.length > 1" class="mt-3 border-l border-zinc-200 pl-3 dark:border-zinc-800">
              <li v-for="session in lookup(stream.session_ids)" :key="session.id">
                <RouterLink :to="`/sessions/${session.id}`" class="group grid min-w-0 gap-x-3 gap-y-0.5 rounded-[3px] py-1.5 text-xs focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 sm:grid-cols-[88px_minmax(0,1fr)_auto] sm:items-center">
                  <span class="font-mono text-zinc-500">{{ span(session.first_at, session.last_at) }}</span>
                  <span class="min-w-0 truncate text-[13px] text-zinc-800 group-hover:underline dark:text-zinc-200">{{ titleOf(session) }}<span class="text-zinc-500"> · {{ harnessLabel(session.harness) }} · {{ plural(session.requests, "request") }}</span></span>
                  <span class="inline-flex items-center gap-1" :class="outcomeMeta[session.outcome].accent"><component :is="outcomeMeta[session.outcome].icon" class="size-3.5" aria-hidden="true" />{{ outcomeMeta[session.outcome].label }}</span>
                </RouterLink>
              </li>
            </ol>
            <ul v-if="stream.commits.length" class="mt-2 space-y-1">
              <li v-for="commit in stream.commits" :key="commit.commit_sha">
                <RouterLink :to="`/sessions/${commit.session_ids[0]}/diff/${commit.commit_sha}`" class="group inline-flex max-w-full items-center gap-2 text-xs text-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-300">
                  <GitCommitHorizontal class="size-3.5 shrink-0 text-zinc-500" aria-hidden="true" />
                  <span class="font-mono text-teal-700 group-hover:underline dark:text-teal-400">{{ commit.commit_sha.slice(0, 8) }}</span>
                  <span class="min-w-0 truncate">{{ commit.subject || "No subject" }}</span>
                  <span class="shrink-0 font-mono text-zinc-500">+{{ commit.patch_additions }} −{{ commit.patch_deletions }}</span>
                  <span v-if="commit.capture_status !== 'saved'" class="shrink-0 text-amber-700 dark:text-amber-400">Patch {{ commit.capture_status === "accepted" ? "pending" : commit.capture_status }}</span>
                </RouterLink>
              </li>
            </ul>
          </li>
        </ol>
        <div v-if="quiet.length" class="mt-4">
          <button type="button" class="inline-flex items-center gap-1.5 text-xs font-medium text-zinc-600 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:text-zinc-100" :aria-expanded="quietOpen" aria-controls="quiet-sessions" @click="quietOpen = !quietOpen">
            <ChevronDown class="size-3.5 transition-transform duration-150 ease-out motion-reduce:transition-none" :class="{ '-rotate-90': !quietOpen }" aria-hidden="true" />{{ plural(quiet.length, "quick session") }} with one request or none
          </button>
          <ul v-show="quietOpen" id="quiet-sessions" class="mt-2 border-t border-zinc-200 dark:border-zinc-800">
            <li v-for="session in quiet" :key="session.id" class="border-b border-zinc-200 dark:border-zinc-800">
              <RouterLink :to="`/sessions/${session.id}`" class="grid gap-x-3 py-2 text-xs text-zinc-600 hover:text-zinc-950 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 sm:grid-cols-[88px_minmax(0,1fr)_auto] dark:text-zinc-400 dark:hover:text-zinc-100">
                <span class="font-mono">{{ span(session.first_at, session.last_at) }}</span>
                <span class="min-w-0 truncate">{{ titleOf(session) }}</span>
                <span>{{ harnessLabel(session.harness) }} · {{ session.repo || "No repository" }}</span>
              </RouterLink>
            </li>
          </ul>
        </div>
      </div>

      <aside class="space-y-8" aria-label="Needs attention">
        <section v-if="inProgress.length">
          <h2 class="mb-2 text-base font-semibold">In progress</h2>
          <ul class="border-t border-zinc-200 dark:border-zinc-800">
            <li v-for="session in inProgress" :key="session.id" class="border-b border-zinc-200 dark:border-zinc-800"><RouterLink :to="`/sessions/${session.id}`" class="block py-2 text-[13px] text-zinc-800 hover:underline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-200">{{ titleOf(session) }}<span class="block text-xs text-zinc-500">{{ harnessLabel(session.harness) }} · last request {{ clock.format(new Date(session.last_at)) }}</span></RouterLink></li>
          </ul>
        </section>
        <section>
          <div class="mb-2 flex items-baseline justify-between gap-3">
            <h2 class="text-base font-semibold">Unresolved</h2>
            <label v-if="unresolved.length > 1" class="inline-flex items-center gap-1.5 text-xs text-zinc-600 dark:text-zinc-400"><input type="checkbox" :checked="allUnresolvedSelected" class="size-4 rounded border-zinc-300 accent-teal-700" @change="toggleAll" />All</label>
          </div>
          <p v-if="!unresolved.length" class="border-t border-zinc-200 py-3 text-sm text-zinc-600 dark:border-zinc-800 dark:text-zinc-400">Every finished session from this day has an outcome.</p>
          <ul v-else class="border-t border-zinc-200 dark:border-zinc-800">
            <li v-for="session in unresolved" :key="session.id" class="flex items-start gap-2.5 border-b border-zinc-200 py-2 dark:border-zinc-800">
              <input :id="`unresolved-${session.id}`" type="checkbox" :checked="selected.has(session.id)" class="mt-0.5 size-4 shrink-0 rounded border-zinc-300 accent-teal-700" @change="toggle(session.id)" />
              <label :for="`unresolved-${session.id}`" class="sr-only">Select {{ titleOf(session) }}</label>
              <RouterLink :to="`/sessions/${session.id}`" class="min-w-0 text-[13px] text-zinc-800 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-200"><span class="line-clamp-2">{{ titleOf(session) }}</span><span class="block text-xs text-zinc-500">{{ session.repo || "No repository" }} · {{ plural(session.requests, "request") }}</span></RouterLink>
            </li>
          </ul>
          <div v-if="selected.size" class="mt-3 flex flex-wrap items-center gap-2">
            <Select v-model="bulkOutcome" label="Outcome for selected sessions" :options="settableOutcomes" class="w-36" />
            <Button :disabled="bulkApplying" @click="applyOutcome">{{ bulkApplying ? "Saving…" : `Set ${selected.size}` }}</Button>
          </div>
          <p v-if="bulkError" class="mt-2 text-xs text-red-700 dark:text-red-400" role="alert">{{ bulkError }}</p>
        </section>
        <section v-if="captureProblems.length">
          <h2 class="mb-2 text-base font-semibold">Capture problems</h2>
          <ul class="border-t border-zinc-200 dark:border-zinc-800">
            <li v-for="session in captureProblems" :key="session.id" class="border-b border-zinc-200 dark:border-zinc-800"><RouterLink :to="`/sessions/${session.id}`" class="block py-2 text-[13px] text-zinc-800 hover:underline focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-200">{{ titleOf(session) }}<span class="block text-xs text-red-700 dark:text-red-400">{{ session.capture_status === "failed" ? "Failed" : "Partial" }} capture</span></RouterLink></li>
          </ul>
        </section>
      </aside>
    </div>
  </section>
</template>
