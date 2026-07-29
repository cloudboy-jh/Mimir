<script setup lang="ts">
import { GitBranch } from "lucide-vue-next";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import SessionModelStack from "@/components/session/SessionModelStack.vue";
import type { SessionDetail } from "@/lib/api";
import { compactNumber, duration, shortDate } from "@/lib/format";

defineProps<{ session: SessionDetail["session"] }>();
</script>

<template>
  <div class="border-b border-zinc-200 pb-6 dark:border-zinc-800"><div class="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between"><div class="max-w-4xl"><div class="mb-3 flex flex-wrap items-center gap-2"><OutcomeBadge :outcome="session.outcome" /><span v-if="session.state === 'active'" class="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-700 dark:text-emerald-400"><span class="size-1.5 rounded-full bg-emerald-500" />Active</span><span v-else class="text-xs font-medium text-zinc-500">Ended</span></div><h1 class="text-2xl font-semibold leading-tight tracking-[-0.025em] text-zinc-950 sm:text-[28px] dark:text-zinc-50">{{ session.intent || "Untitled session" }}</h1><div class="mt-4 flex flex-wrap gap-x-4 gap-y-2 text-[13px] text-zinc-500 dark:text-zinc-400"><strong class="font-medium text-zinc-800 dark:text-zinc-200">{{ session.repo || "No repository" }}</strong><span v-if="session.source_ref" class="inline-flex items-center gap-1"><GitBranch class="size-3.5" />{{ session.source_ref }}</span><span>{{ shortDate(session.started_at) }}</span><span class="font-mono text-xs">{{ session.id }}</span></div></div><SessionModelStack class="min-w-56" mode="tree" :app="session.harness" :primary="session.model_primary" :models="session.models" /></div></div>
  <dl class="grid grid-cols-2 border-b border-zinc-200 md:grid-cols-4 dark:border-zinc-800"><div class="border-r border-zinc-200 py-5 pr-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Duration</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ duration(session.started_at, session.ended_at) }}</dd></div><div class="border-r border-zinc-200 px-5 py-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Requests</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ session.request_count }}</dd></div><div class="border-r border-zinc-200 py-5 pr-5 md:px-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Input tokens</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_in) }}</dd></div><div class="py-5 pl-5"><dt class="text-xs text-zinc-500">Output tokens</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_out) }}</dd></div></dl>
</template>
