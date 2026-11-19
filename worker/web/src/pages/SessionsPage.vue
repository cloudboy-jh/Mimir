<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Filter, GitBranch, RotateCw, Search, X } from "lucide-vue-next";
import CaptureStatus from "@/components/CaptureStatus.vue";
import DeviceIdentity from "@/components/DeviceIdentity.vue";
import GitStatus from "@/components/GitStatus.vue";
import IdentityBadge from "@/components/IdentityBadge.vue";
import Button from "@/components/ui/Button.vue";
import DropdownPanel from "@/components/ui/DropdownPanel.vue";
import Select from "@/components/ui/Select.vue";
import { errorMessage, getStats, listDevices, listSessions, type Session } from "@/lib/api";
import { useAutoRefresh } from "@/lib/auto-refresh";
import { harnessLabel, relativeDate } from "@/lib/format";
import { pageSizeOptions, type SelectOption } from "@/lib/options";

const SEARCH_DEBOUNCE_MS = 350;
const DEFAULT_LIMIT = "20";
// Filter option lists cover every recorded value, not only the visible page.
const ALL_TIME = "3650d";
const panelKeys = ["harness", "device", "state", "git_status", "from", "to"] as const;
const labels: Record<string, string> = { repo: "Repository", harness: "App", device: "Device", state: "State", git_status: "Git", from: "From (UTC)", to: "To (UTC)" };
const stateOptions: SelectOption[] = [{ value: "", label: "All states" }, { value: "active", label: "Active" }, { value: "inactive", label: "Inactive" }];
const gitOptions: SelectOption[] = [{ value: "", label: "Any Git status" }, { value: "none", label: "No commits" }, { value: "committed", label: "Committed" }, { value: "pushed", label: "Pushed" }];

const route = useRoute();
const router = useRouter();
const sessions = ref<Session[]>([]);
const nextCursor = ref<string | null>(null);
const loading = ref(true);
const error = ref("");
const filtersOpen = ref(false);
const search = ref("");
const draft = reactive<Record<string, string>>(Object.fromEntries(panelKeys.map((key) => [key, ""])));
const repoOptions = ref<SelectOption[]>([{ value: "", label: "All repositories" }]);
const harnessOptions = ref<SelectOption[]>([{ value: "", label: "All apps" }]);
const deviceOptions = ref<SelectOption[]>([{ value: "", label: "All devices" }]);
const deviceNames = computed(() => new Map(deviceOptions.value.map((option) => [option.value, option.label])));
let controller: AbortController | null = null;
let searchTimer: ReturnType<typeof setTimeout> | undefined;

function param(key: string) {
  const value = route.query[key];
  return typeof value === "string" ? value : "";
}

const limit = computed(() => param("limit") || DEFAULT_LIMIT);
const cursor = computed(() => param("cursor"));
const activeFilters = computed(() => panelKeys.flatMap((key) => {
  const value = param(key);
  if (!value) return [];
  const shown = key === "device" ? deviceNames.value.get(value) ?? value : key === "harness" ? harnessLabel(value) : value;
  return [{ key, label: labels[key], value: shown }];
}));
const filtered = computed(() => Boolean(activeFilters.value.length || param("q") || param("repo")));

// Any filter change returns to the first page; only paging sets a cursor.
function setParams(patch: Record<string, string>) {
  const query = { ...route.query } as Record<string, string>;
  delete query.cursor;
  for (const [key, raw] of Object.entries(patch)) {
    const value = raw.trim();
    if (!value || (key === "limit" && value === DEFAULT_LIMIT)) delete query[key];
    else query[key] = value;
  }
  void router.push({ name: "sessions", query });
}

async function load(silent = false) {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  if (!silent) loading.value = true;
  error.value = "";
  try {
    const result = await listSessions({
      q: param("q"), repo: param("repo"), harness: param("harness"), device: param("device"), state: param("state"),
      git_status: param("git_status"), from: param("from"), to: param("to"), limit: Number(limit.value), cursor: cursor.value,
    }, active.signal);
    sessions.value = result.sessions;
    nextCursor.value = result.next_cursor;
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "Sessions could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

async function loadOptions() {
  const [repos, harnesses, devices] = await Promise.allSettled([getStats("repo", { from: ALL_TIME }), getStats("harness", { from: ALL_TIME }), listDevices()]);
  if (repos.status === "fulfilled") repoOptions.value = [repoOptions.value[0]!, ...repos.value.groups.filter((group) => group.key).map((group) => ({ value: group.key, label: group.key }))];
  if (harnesses.status === "fulfilled") harnessOptions.value = [harnessOptions.value[0]!, ...harnesses.value.groups.filter((group) => group.key).map((group) => ({ value: group.key, label: harnessLabel(group.key) }))];
  if (devices.status === "fulfilled") deviceOptions.value = [deviceOptions.value[0]!, ...devices.value.devices.map((device) => ({ value: device.id, label: device.name, description: `${device.platform} ${device.arch}` }))];
}

function applyDraft() {
  setParams({ ...draft });
  filtersOpen.value = false;
}

function clearFilters() {
  search.value = "";
  clearTimeout(searchTimer);
  setParams(Object.fromEntries([...panelKeys, "q", "repo"].map((key) => [key, ""])));
  filtersOpen.value = false;
}

function nextPage() {
  if (nextCursor.value) void router.push({ name: "sessions", query: { ...route.query, cursor: nextCursor.value } });
}

watch(search, (value) => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (value.trim() !== param("q")) setParams({ q: value });
  }, SEARCH_DEBOUNCE_MS);
});
watch(filtersOpen, (open) => {
  if (open) for (const key of panelKeys) draft[key] = param(key);
});
watch(() => route.fullPath, () => {
  if (param("q") !== search.value.trim()) search.value = param("q");
  void load();
}, { immediate: true });
onMounted(loadOptions);
useAutoRefresh(() => load(true));
onBeforeUnmount(() => { controller?.abort(); clearTimeout(searchTimer); });
</script>

<template>
  <section>
    <h1 class="mb-4 text-2xl font-semibold tracking-tight">Sessions</h1>

    <div class="mb-4">
      <div class="flex flex-wrap items-center gap-2">
        <Select :model-value="param('repo')" label="Repository" :options="repoOptions" class="w-full sm:w-56" @update:model-value="setParams({ repo: $event })" />
        <form class="relative min-w-0 flex-1 sm:w-72 sm:flex-none" role="search" @submit.prevent="setParams({ q: search })">
          <label class="sr-only" for="session-search">Search sessions</label>
          <Search class="pointer-events-none absolute left-2.5 top-2.25 size-4 text-zinc-400" aria-hidden="true" />
          <input id="session-search" v-model="search" type="search" placeholder="Search sessions" class="h-8.5 w-full rounded-[5px] border border-zinc-300 bg-transparent pl-8.5 pr-3 text-[13px] text-zinc-900 placeholder:text-zinc-500 focus-visible:outline-2 focus-visible:outline-teal-600 dark:border-zinc-700 dark:text-zinc-100" />
        </form>
        <DropdownPanel v-model:open="filtersOpen" title="Filter sessions">
          <template #trigger><Button variant="outline"><Filter class="size-3.5" />Filters<span v-if="activeFilters.length" class="font-mono text-[11px]">{{ activeFilters.length }}</span></Button></template>
          <form id="session-filters" class="grid gap-3 sm:grid-cols-2" @submit.prevent="applyDraft">
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">App</span><Select v-model="draft.harness" label="App" :options="harnessOptions" class="w-full font-normal" /></div>
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Device</span><Select v-model="draft.device" label="Device" :options="deviceOptions" class="w-full font-normal" /></div>
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">State</span><Select v-model="draft.state" label="State" :options="stateOptions" class="w-full font-normal" /></div>
            <div class="text-xs font-medium text-zinc-600 dark:text-zinc-400"><span class="mb-1 block">Git</span><Select v-model="draft.git_status" label="Git status" :options="gitOptions" class="w-full font-normal" /></div>
            <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">From (UTC)<input v-model="draft.from" type="date" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:[color-scheme:dark]" /></label>
            <label class="text-xs font-medium text-zinc-600 dark:text-zinc-400">To (UTC)<input v-model="draft.to" type="date" class="mt-1 block h-8.5 w-full rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] font-normal text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100 dark:[color-scheme:dark]" /></label>
          </form>
          <template #footer>
            <Button variant="ghost" @click="clearFilters">Clear all</Button>
            <Button variant="outline" @click="filtersOpen = false">Cancel</Button>
            <Button type="submit" form="session-filters">Apply filters</Button>
          </template>
        </DropdownPanel>
      </div>
      <ul v-if="activeFilters.length" class="mt-2.5 flex flex-wrap items-center gap-2">
        <li v-for="facet in activeFilters" :key="facet.key">
          <button type="button" class="inline-flex items-center gap-1.5 rounded-[5px] border border-zinc-300 px-2 py-1 text-[11px] text-zinc-700 transition-colors duration-150 ease-out hover:border-zinc-400 hover:bg-stone-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:border-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-800" @click="setParams({ [facet.key]: '' })">
            <span class="text-zinc-500">{{ facet.label }}</span><span class="font-mono">{{ facet.value }}</span><X class="size-3" aria-hidden="true" />
            <span class="sr-only">Remove {{ facet.label }} filter</span>
          </button>
        </li>
        <li><button type="button" class="text-[11px] font-medium text-zinc-500 hover:text-zinc-900 focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:hover:text-zinc-100" @click="clearFilters">Clear all</button></li>
      </ul>
    </div>

    <div class="overflow-x-auto rounded-[7px] border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
      <table class="w-full min-w-[1080px] border-collapse text-left text-xs">
        <caption class="sr-only">Root sessions, newest activity first</caption>
        <thead class="border-b border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400">
          <tr>
            <th scope="col" class="px-4 py-2.5 font-medium">Session</th>
            <th scope="col" class="px-3 py-2.5 font-medium">Repository</th>
            <th scope="col" class="px-3 py-2.5 font-medium">App</th>
            <th scope="col" class="px-3 py-2.5 font-medium">Device</th>
            <th scope="col" class="px-3 py-2.5 font-medium">Models</th>
            <th scope="col" class="px-3 py-2.5 font-medium">Git</th>
            <th scope="col" class="px-3 py-2.5 font-medium">Capture</th>
            <th scope="col" class="px-3 py-2.5 text-right font-medium">Requests</th>
            <th scope="col" class="px-4 py-2.5 text-right font-medium">Activity</th>
          </tr>
        </thead>
        <tbody v-if="loading" aria-busy="true">
          <tr v-for="index in 5" :key="index" class="border-b border-zinc-200 last:border-b-0 dark:border-zinc-800"><td colspan="9" class="px-4 py-4"><div class="h-4 w-3/5 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></td></tr>
        </tbody>
        <tbody v-else-if="error && !sessions.length">
          <tr><td colspan="9" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Sessions unavailable</p><p class="mx-auto mt-1 max-w-md text-sm text-zinc-500">{{ error }}</p><Button class="mt-4" variant="outline" @click="load()"><RotateCw class="size-3.5" />Retry</Button></td></tr>
        </tbody>
        <tbody v-else-if="!sessions.length">
          <tr><td colspan="9" class="px-4 py-16 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">{{ filtered ? "No matching sessions" : "No sessions captured yet" }}</p><p class="mt-1 text-sm text-zinc-500">{{ filtered ? "Clear a filter or try a broader search." : "Captured model traffic will appear here as work sessions." }}</p></td></tr>
        </tbody>
        <tbody v-else>
          <tr v-for="session in sessions" :key="session.id" class="border-b border-zinc-200 align-top last:border-b-0 hover:bg-stone-50 dark:border-zinc-800 dark:hover:bg-stone-900/40">
            <td class="max-w-[22rem] px-4 py-3">
              <RouterLink :to="`/sessions/${session.id}`" class="block truncate text-[13px] font-medium text-zinc-950 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-100">{{ session.display_title }}</RouterLink>
              <p class="mt-1 flex flex-wrap gap-x-2 text-zinc-500 dark:text-zinc-400">
                <span v-if="session.state === 'active'" class="font-medium text-emerald-800 dark:text-emerald-300">In progress</span>
                <span v-if="session.child_session_count">{{ session.child_session_count }} {{ session.child_session_count === 1 ? "sub-agent" : "sub-agents" }}</span>
              </p>
            </td>
            <td class="max-w-[14rem] px-3 py-3">
              <p class="truncate font-medium text-zinc-700 dark:text-zinc-300">{{ session.repo || "No repository" }}</p>
              <p v-if="session.source_ref" class="mt-1 flex min-w-0 items-center gap-1 text-zinc-500 dark:text-zinc-400"><GitBranch class="size-3 shrink-0" aria-hidden="true" /><span class="truncate">{{ session.source_ref }}</span></p>
            </td>
            <td class="px-3 py-3"><IdentityBadge :label="harnessLabel(session.harness)" /></td>
            <td class="max-w-[12rem] px-3 py-3"><DeviceIdentity v-if="session.device" :device="session.device" compact /><span v-else class="text-zinc-500">Unknown</span></td>
            <td class="max-w-[14rem] px-3 py-3">
              <div class="flex min-w-0 items-center gap-2">
                <IdentityBadge :label="session.models[0]?.name || session.model_primary || 'Unknown model'" />
                <span v-if="session.models.length > 1" class="shrink-0 font-medium text-zinc-500 dark:text-zinc-400" :title="session.models.slice(1).map((model) => model.name).join(', ')">+{{ session.models.length - 1 }}</span>
              </div>
            </td>
            <td class="px-3 py-3"><GitStatus :status="session.git_status" :count="session.commit_count" /></td>
            <td class="px-3 py-3"><CaptureStatus :capture="session.capture" /></td>
            <td class="px-3 py-3 text-right font-mono text-zinc-700 dark:text-zinc-300">{{ session.request_count }}</td>
            <td class="whitespace-nowrap px-4 py-3 text-right text-zinc-600 dark:text-zinc-400"><time :datetime="session.activity_at" :title="session.activity_at">{{ relativeDate(session.activity_at) }}</time></td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="mt-4 flex flex-wrap items-center gap-3">
      <p v-if="error && sessions.length" class="text-xs text-red-700 dark:text-red-400" role="alert">{{ error }}</p>
      <Button v-if="cursor" variant="ghost" @click="setParams({})">First page</Button>
      <Button v-if="nextCursor" variant="outline" :disabled="loading" @click="nextPage">Next page</Button>
      <div class="ml-auto flex items-center gap-2"><span class="text-xs text-zinc-600 dark:text-zinc-400">Rows</span><Select :model-value="limit" label="Sessions per page" :options="pageSizeOptions" class="w-24" @update:model-value="setParams({ limit: $event })" /></div>
    </div>
  </section>
</template>
