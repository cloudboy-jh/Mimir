<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ArrowLeft, ChevronLeft, ChevronRight, CircleDashed, GitCommitHorizontal, RotateCw } from "lucide-vue-next";
import Button from "@/components/ui/Button.vue";
import Select from "@/components/ui/Select.vue";
import BrandIcon from "@/components/BrandIcon.vue";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import DayDonut from "@/components/day/DayDonut.vue";
import { dashboardNow, errorMessage, getDay, setSessionsOutcome, type DaySnapshot, type Outcome } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { agentTime, dayLevel, share, sliceTones, type DayItem } from "@/lib/day";
import { compactNumber, harnessLabel } from "@/lib/format";
import { outcomeOptions } from "@/lib/options";
import { outcomeMeta, outcomeOrder } from "@/lib/outcomes";

const route = useRoute();
const router = useRouter();
const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
const localDate = new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" });
const today = ref(localDate.format(dashboardNow()));
const date = computed(() => (typeof route.params.date === "string" && route.params.date) || today.value);
const focus = computed(() => (typeof route.query.focus === "string" ? route.query.focus : null));

const snapshot = ref<DaySnapshot | null>(null);
const loading = ref(true);
const error = ref("");
const hovered = ref<string | null>(null);
const outcomeRow = ref<string | null>(null);
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
  void load();
}, { immediate: true });
watch(focus, () => {
  hovered.value = null;
  outcomeRow.value = null;
});
useAutoRefresh(() => (date.value === today.value ? load(true) : undefined));
onBeforeUnmount(() => controller?.abort());

const level = computed(() => (snapshot.value ? dayLevel(snapshot.value, focus.value) : null));
const entries = computed(() => {
  const items = level.value?.items ?? [];
  const tones = sliceTones(items);
  return items.map((item, index) => ({ item, tone: tones[index]! }));
});
const total = computed(() => entries.value.reduce((sum, entry) => sum + entry.item.minutes, 0));

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

const clock = new Intl.DateTimeFormat("en", { timeZone, hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
function span(item: DayItem) {
  if (!item.firstAt || !item.lastAt) return "";
  const from = clock.format(new Date(item.firstAt)), to = clock.format(new Date(item.lastAt));
  return from === to ? from : `${from}–${to}`;
}
function decided(item: DayItem) {
  return outcomeOrder.filter((outcome) => outcome !== "unresolved" && item.outcomes[outcome]).map((outcome) => `${item.outcomes[outcome]} ${outcomeMeta[outcome].label.toLowerCase()}`).join(" · ");
}

function open(item: DayItem) {
  if ("session" in item.open) void router.push(`/sessions/${item.open.session}`);
  else void router.push({ query: { focus: item.open.focus } });
}
function go(target: string | null) {
  if (target) void router.push(target === today.value ? "/days" : `/days/${target}`);
}

const settableOutcomes = outcomeOptions.filter((option) => option.value && option.value !== "unresolved");
async function applyOutcome(ids: string[]) {
  bulkApplying.value = true;
  bulkError.value = "";
  try {
    await setSessionsOutcome(ids, bulkOutcome.value, "");
    outcomeRow.value = null;
    await load(true);
  } catch (cause) {
    bulkError.value = errorMessage(cause, "Outcomes could not be saved.");
  } finally {
    bulkApplying.value = false;
  }
}
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

    <div v-if="loading && !snapshot" aria-busy="true" aria-label="Loading day" class="grid items-start gap-10 lg:grid-cols-[minmax(240px,340px)_minmax(0,1fr)]">
      <div class="mx-auto aspect-square w-full max-w-[340px] animate-pulse rounded-full border-[36px] border-zinc-200 motion-reduce:animate-none dark:border-zinc-800" />
      <div><div v-for="index in 5" :key="index" class="border-t border-zinc-200 py-4 dark:border-zinc-800"><div class="h-4 w-3/5 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
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

    <div v-else-if="level" class="grid items-start gap-x-12 gap-y-8 lg:grid-cols-[minmax(240px,340px)_minmax(0,1fr)]">
      <div class="mx-auto w-full max-w-[340px] lg:mx-0">
        <DayDonut v-model:hovered="hovered" :entries="entries" :caption="level.caption" @open="open" />
        <p class="mt-4 text-center text-xs leading-5 text-zinc-500 dark:text-zinc-400">Agent time counts each session's 15-minute blocks with requests. Parallel sessions each count, so it can exceed clock time.</p>
      </div>

      <div class="min-w-0">
        <div class="mb-2 flex min-h-8.5 items-center gap-3">
          <RouterLink v-if="level.focus" :to="{ query: {} }" class="inline-flex shrink-0 items-center gap-1.5 text-[13px] font-medium text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400"><ArrowLeft class="size-3.5" aria-hidden="true" />Whole day</RouterLink>
          <h2 class="min-w-0 truncate text-base font-semibold">{{ level.title ?? "Workstreams" }}</h2>
        </div>
        <ol class="border-t border-zinc-200 dark:border-zinc-800">
          <li v-for="{ item, tone } in entries" :key="item.key" class="border-b border-zinc-200 transition-colors duration-150 motion-reduce:transition-none dark:border-zinc-800" :class="hovered === item.key ? 'bg-white dark:bg-zinc-900' : ''" @mouseenter="hovered = item.key" @mouseleave="hovered = null">
            <button type="button" class="group grid w-full grid-cols-[10px_minmax(0,1fr)_auto] items-start gap-x-3 px-2 py-3 text-left focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600" @click="open(item)" @focus="hovered = item.key" @blur="hovered = null">
              <span class="mt-1.5 size-2.5 rounded-[2px]" :class="tone.swatch" aria-hidden="true" />
              <span class="min-w-0">
                <span class="flex min-w-0 items-center gap-2">
                  <BrandIcon v-if="item.app" :label="harnessLabel(item.app)" />
                  <span class="truncate text-sm font-medium text-zinc-950 group-hover:underline dark:text-zinc-100">{{ item.label }}</span>
                  <span v-if="item.active" class="shrink-0 rounded-[4px] border border-emerald-200 bg-emerald-50 px-1.5 text-[11px] font-medium text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300">In progress</span>
                  <OutcomeBadge v-if="item.kind === 'session' && item.outcomes.unresolved === 0" :outcome="outcomeOrder.find((outcome) => item.outcomes[outcome])!" class="shrink-0" />
                </span>
                <span class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-zinc-500 dark:text-zinc-400">
                  <span v-if="item.repo" class="font-medium text-zinc-700 dark:text-zinc-300">{{ item.repo }}<span v-if="item.sourceRef" class="font-normal text-zinc-500 dark:text-zinc-400"> · {{ item.sourceRef }}</span></span>
                  <span v-if="item.model" class="font-mono">{{ item.model }}</span>
                  <span class="font-mono">{{ span(item) }}</span>
                  <span v-if="item.sessionIds.length > 1">{{ plural(item.sessionIds.length, "session") }}</span>
                  <span>{{ plural(item.requests, "request") }}</span>
                  <span v-if="item.commits" class="inline-flex items-center gap-1"><GitCommitHorizontal class="size-3.5" aria-hidden="true" />{{ item.commits }}</span>
                  <span v-if="item.kind !== 'session' && decided(item)">{{ decided(item) }}</span>
                  <span v-if="item.captureProblems" class="text-red-700 dark:text-red-400">{{ plural(item.captureProblems, "capture problem") }}</span>
                </span>
              </span>
              <span class="flex items-center gap-2 text-right">
                <span><span class="block font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ agentTime(item.minutes) }}</span><span class="block text-xs text-zinc-500 dark:text-zinc-400">{{ share(item.minutes, total) }}</span></span>
                <ChevronRight class="size-4 text-zinc-400 transition-transform duration-150 group-hover:translate-x-0.5 motion-reduce:transform-none motion-reduce:transition-none" aria-hidden="true" />
              </span>
            </button>
            <div v-if="item.unresolvedIds.length" class="flex flex-wrap items-center gap-2 pb-3 pl-[34px] pr-2 text-xs">
              <span class="inline-flex items-center gap-1 text-zinc-600 dark:text-zinc-400"><CircleDashed class="size-3.5" aria-hidden="true" />{{ item.unresolvedIds.length === item.sessionIds.length && item.sessionIds.length === 1 ? "Unresolved" : `${item.unresolvedIds.length} unresolved` }}</span>
              <button v-if="outcomeRow !== item.key" type="button" class="font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400" @click="outcomeRow = item.key">Set outcome</button>
              <template v-else>
                <Select v-model="bulkOutcome" :label="`Outcome for ${item.label}`" :options="settableOutcomes" class="w-36" />
                <Button :disabled="bulkApplying" @click="applyOutcome(item.unresolvedIds)">{{ bulkApplying ? "Saving…" : item.unresolvedIds.length > 1 ? `Set ${item.unresolvedIds.length}` : "Set" }}</Button>
                <Button variant="ghost" :disabled="bulkApplying" @click="outcomeRow = null">Cancel</Button>
              </template>
              <p v-if="bulkError && outcomeRow === item.key" class="w-full text-red-700 dark:text-red-400" role="alert">{{ bulkError }}</p>
            </div>
          </li>
        </ol>

        <section v-if="level.commits.length" class="mt-6">
          <h3 class="mb-2 text-sm font-semibold">Commits</h3>
          <ul class="space-y-1.5">
            <li v-for="commit in level.commits" :key="commit.commit_sha">
              <RouterLink :to="`/sessions/${commit.session_ids[0]}/diff/${commit.commit_sha}`" class="group inline-flex max-w-full items-center gap-2 text-xs text-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-300">
                <GitCommitHorizontal class="size-3.5 shrink-0 text-zinc-500" aria-hidden="true" />
                <span class="font-mono text-teal-700 group-hover:underline dark:text-teal-400">{{ commit.commit_sha.slice(0, 8) }}</span>
                <span class="min-w-0 truncate">{{ commit.subject || "No subject" }}</span>
                <span class="shrink-0 font-mono text-zinc-500">+{{ commit.patch_additions }} −{{ commit.patch_deletions }}</span>
                <span v-if="commit.capture_status !== 'saved'" class="shrink-0 text-amber-700 dark:text-amber-400">Patch {{ commit.capture_status === "accepted" ? "pending" : commit.capture_status }}</span>
              </RouterLink>
            </li>
          </ul>
        </section>
      </div>
    </div>
  </section>
</template>
