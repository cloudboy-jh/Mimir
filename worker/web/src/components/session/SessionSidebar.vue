<script setup lang="ts">
import { computed, ref } from "vue";
import { ExternalLink } from "lucide-vue-next";
import GitStatus from "@/components/GitStatus.vue";
import type { SessionDetail } from "@/lib/api";
import { harnessLabel, relativeDate, shortDate } from "@/lib/format";
import { commitUrl } from "@/lib/git";

const PREVIEW = 8;
const props = defineProps<{ detail: SessionDetail }>();
defineEmits<{ openCommit: [sha: string] }>();
const showAllFiles = ref(false);
const showAllErrors = ref(false);
const files = computed(() => (showAllFiles.value ? props.detail.files : props.detail.files.slice(0, PREVIEW)));
const errors = computed(() => (showAllErrors.value ? props.detail.errors : props.detail.errors.slice(0, PREVIEW)));
const heading = "mb-2 flex items-baseline gap-2 border-b border-zinc-200 pb-2 text-sm font-semibold dark:border-zinc-800";
const more = "mt-2 text-xs font-medium text-teal-700 hover:underline focus-visible:rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-teal-400";
</script>

<template>
  <aside aria-label="Session evidence" class="min-w-0 space-y-6 border-t border-zinc-200 pt-4 lg:self-start lg:border-t-0 lg:border-l lg:pl-6 lg:pt-0 dark:border-zinc-800">
    <section aria-labelledby="commits-heading">
      <h2 id="commits-heading" :class="heading">Commits <span class="font-mono text-xs font-normal text-zinc-600 dark:text-zinc-400">{{ detail.commits.length }}</span></h2>
      <p v-if="!detail.commits.length" class="text-xs text-zinc-600 dark:text-zinc-400">No commits were captured.</p>
      <ol v-else class="divide-y divide-zinc-200 dark:divide-zinc-800">
        <li v-for="commit in detail.commits" :key="`${commit.session_id}-${commit.commit_sha}`" class="py-2.5">
          <button type="button" class="block w-full min-w-0 rounded-[3px] text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:cursor-default" :disabled="commit.capture_status !== 'saved'" @click="$emit('openCommit', commit.commit_sha)">
            <span class="block break-words text-[13px] font-medium leading-5" :class="commit.capture_status === 'saved' ? 'text-teal-700 hover:underline dark:text-teal-400' : 'text-zinc-800 dark:text-zinc-200'">{{ commit.subject || "Untitled commit" }}</span>
            <span class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-zinc-600 dark:text-zinc-400">
              <code class="font-mono" :title="commit.commit_sha">{{ commit.commit_sha.slice(0, 7) }}</code>
              <GitStatus :status="commit.pushed ? 'pushed' : 'committed'" />
              <span class="font-mono">+{{ commit.patch_additions }} −{{ commit.patch_deletions }}</span>
              <span v-if="commit.capture_status !== 'saved'" class="text-amber-800 dark:text-amber-300">Patch {{ commit.capture_status === "accepted" ? "pending" : commit.capture_status }}</span>
            </span>
          </button>
          <p class="mt-1 flex flex-wrap items-center gap-x-3 text-xs text-zinc-600 dark:text-zinc-400">
            <span v-if="commit.ref" class="break-all">{{ commit.ref }}</span>
            <span v-if="commit.committed_at">{{ shortDate(commit.committed_at) }}</span>
            <a v-if="commitUrl(commit.repository_url, commit.commit_sha)" :href="commitUrl(commit.repository_url, commit.commit_sha)!" target="_blank" rel="noreferrer noopener" class="inline-flex items-center gap-1 text-teal-700 hover:underline focus-visible:outline-2 focus-visible:outline-teal-600 dark:text-teal-400">Remote<ExternalLink class="size-3" aria-hidden="true" /><span class="sr-only">(opens in a new tab)</span></a>
          </p>
        </li>
      </ol>
    </section>

    <section v-if="detail.files.length" aria-labelledby="files-heading">
      <h2 id="files-heading" :class="heading">Files <span class="font-mono text-xs font-normal text-zinc-600 dark:text-zinc-400">{{ detail.files.length }}</span></h2>
      <p class="mb-2 text-[11px] leading-4 text-zinc-600 dark:text-zinc-400">Mentioned in captured traffic, not an inventory of edits.</p>
      <ul class="divide-y divide-zinc-200 dark:divide-zinc-800">
        <li v-for="file in files" :key="file.path" class="flex items-baseline justify-between gap-3 py-1.5 font-mono text-xs text-zinc-700 dark:text-zinc-300"><span class="min-w-0 break-all">{{ file.path }}</span><span class="shrink-0 text-zinc-500">{{ file.count }}×</span></li>
      </ul>
      <button v-if="detail.files.length > PREVIEW" type="button" :class="more" @click="showAllFiles = !showAllFiles">{{ showAllFiles ? "Show fewer files" : `Show all ${detail.files.length} files` }}</button>
    </section>

    <section v-if="detail.errors.length" aria-labelledby="errors-heading">
      <h2 id="errors-heading" :class="heading">Errors <span class="font-mono text-xs font-normal text-zinc-600 dark:text-zinc-400">{{ detail.errors.length }}</span></h2>
      <ul class="divide-y divide-zinc-200 dark:divide-zinc-800">
        <li v-for="item in errors" :key="item.signature" class="py-2">
          <details>
            <summary class="flex cursor-pointer list-none items-baseline justify-between gap-3 rounded-[3px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 [&::-webkit-details-marker]:hidden">
              <span class="min-w-0 truncate text-xs leading-5 text-zinc-800 dark:text-zinc-200">{{ item.signature }}</span>
              <span class="shrink-0 font-mono text-[11px] text-zinc-600 dark:text-zinc-400">{{ item.count }}×</span>
            </summary>
            <p class="mt-2 max-h-40 overflow-y-auto whitespace-pre-wrap break-words font-mono text-[11px] leading-4 text-zinc-600 dark:text-zinc-400">{{ item.signature }}</p>
            <p v-if="item.last_seen_at" class="mt-1 text-[11px] text-zinc-600 dark:text-zinc-400">Last seen {{ relativeDate(item.last_seen_at) }}</p>
          </details>
        </li>
      </ul>
      <button v-if="detail.errors.length > PREVIEW" type="button" :class="more" @click="showAllErrors = !showAllErrors">{{ showAllErrors ? "Show fewer errors" : `Show all ${detail.errors.length} errors` }}</button>
    </section>

    <section v-if="detail.supporting_sessions.length" aria-labelledby="supporting-heading">
      <h2 id="supporting-heading" :class="heading">Supporting sessions <span class="font-mono text-xs font-normal text-zinc-600 dark:text-zinc-400">{{ detail.supporting_sessions.length }}</span></h2>
      <ol class="divide-y divide-zinc-200 dark:divide-zinc-800">
        <li v-for="session in detail.supporting_sessions" :key="session.id">
          <RouterLink :to="`/sessions/${session.id}`" class="block min-w-0 py-2 hover:bg-stone-50 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 dark:hover:bg-zinc-900">
            <span class="block break-words text-xs font-medium leading-5 text-zinc-800 dark:text-zinc-200">{{ session.display_title }}</span>
            <span class="mt-0.5 block truncate text-[11px] text-zinc-600 dark:text-zinc-400">{{ harnessLabel(session.harness) }}<template v-if="session.model_primary"> · <span class="font-mono">{{ session.model_primary }}</span></template> · {{ session.request_count }} {{ session.request_count === 1 ? "request" : "requests" }}</span>
          </RouterLink>
        </li>
      </ol>
    </section>
  </aside>
</template>
