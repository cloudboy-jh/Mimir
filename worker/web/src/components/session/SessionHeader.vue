<script setup lang="ts">
import { GitBranch } from "lucide-vue-next";
import SessionModelStack from "@/components/session/SessionModelStack.vue";
import type { SessionDetail } from "@/lib/api";
import { compactNumber, duration, shortDate } from "@/lib/format";

defineProps<{ session: SessionDetail["session"] }>();
</script>

<template>
  <div class="border-b border-zinc-200 pb-6 dark:border-zinc-800">
    <div class="flex flex-col gap-6 lg:flex-row lg:items-start lg:justify-between">
      <div class="min-w-0 max-w-4xl flex-1">
        <div class="mb-3"><span v-if="session.state === 'active'" class="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-700 dark:text-emerald-400"><span class="size-1.5 rounded-full bg-emerald-500" />Active</span><span v-else class="text-xs font-medium text-zinc-500">Ended</span></div>
        <h1 class="text-2xl font-semibold leading-tight tracking-[-0.025em] text-zinc-950 sm:text-[28px] dark:text-zinc-50">{{ session.intent || "Untitled session" }}</h1>
        <div class="mt-4 flex flex-wrap gap-x-4 gap-y-2 text-[13px] text-zinc-500 dark:text-zinc-400"><strong class="font-medium text-zinc-800 dark:text-zinc-200">{{ session.repo || "No repository" }}</strong><span v-if="session.source_ref" class="inline-flex items-center gap-1"><GitBranch class="size-3.5" />{{ session.source_ref }}</span><span>{{ shortDate(session.started_at) }}</span><span class="break-all font-mono text-xs">{{ session.id }}</span></div>
        <dl class="mt-5 flex flex-wrap divide-x divide-zinc-200 border-t border-zinc-200 pt-3 dark:divide-zinc-800 dark:border-zinc-800">
          <div class="pr-4"><dt class="text-[11px] text-zinc-500">Duration</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ duration(session.started_at, session.ended_at) }}</dd></div>
          <div class="px-4"><dt class="text-[11px] text-zinc-500">Requests</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ session.request_count }}</dd></div>
          <div class="px-4"><dt class="text-[11px] text-zinc-500">Input</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_in) }}</dd></div>
          <div class="pl-4"><dt class="text-[11px] text-zinc-500">Output</dt><dd class="mt-0.5 font-mono text-xs text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_out) }}</dd></div>
        </dl>
      </div>
      <div class="w-full min-w-0 border-t border-zinc-200 pt-4 lg:w-80 lg:shrink-0 lg:border-t-0 lg:pt-0 xl:w-96 dark:border-zinc-800"><h2 class="mb-2 text-xs font-medium text-zinc-500">Models involved</h2><SessionModelStack mode="tree" :app="session.harness" :primary="session.model_primary" :models="session.models" /></div>
    </div>
  </div>
</template>
