<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { ArrowLeft, Database, FileCode2, GitBranch, TriangleAlert } from "lucide-vue-next";
import IdentityBadge from "@/components/IdentityBadge.vue";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import { exchangesForSession, sessionById } from "@/lib/mock";
import { compactNumber, duration, shortDate } from "@/lib/format";

const route = useRoute();
const session = computed(() => sessionById(String(route.params.id)));
const sessionExchanges = computed(() => exchangesForSession(String(route.params.id)));
</script>

<template>
  <section v-if="session">
    <RouterLink to="/sessions" class="mb-6 inline-flex items-center gap-1.5 text-[13px] font-medium text-zinc-500 hover:text-zinc-950 dark:text-zinc-400 dark:hover:text-zinc-100"><ArrowLeft class="size-4" />Sessions</RouterLink>
    <div class="border-b border-zinc-200 pb-6 dark:border-zinc-800">
      <div class="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
        <div class="max-w-4xl">
          <div class="mb-3 flex flex-wrap items-center gap-2">
            <OutcomeBadge :outcome="session.outcome" />
            <span v-if="session.state === 'active'" class="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-700 dark:text-emerald-400"><span class="size-1.5 rounded-full bg-emerald-500" />Active</span>
          </div>
          <h1 class="text-2xl font-semibold leading-tight tracking-[-0.025em] text-zinc-950 sm:text-[28px] dark:text-zinc-50">{{ session.intent }}</h1>
          <div class="mt-4 flex flex-wrap gap-x-4 gap-y-2 text-[13px] text-zinc-500 dark:text-zinc-400"><strong class="font-medium text-zinc-800 dark:text-zinc-200">{{ session.repo }}</strong><span class="inline-flex items-center gap-1"><GitBranch class="size-3.5" />{{ session.source_ref }}</span><span>{{ shortDate(session.started_at) }}</span><span class="font-mono text-xs">{{ session.id }}</span></div>
        </div>
        <div class="space-y-2"><IdentityBadge :label="session.harness" /><IdentityBadge :label="session.model_primary" /></div>
      </div>
    </div>

    <dl class="grid grid-cols-2 border-b border-zinc-200 md:grid-cols-4 dark:border-zinc-800"><div class="border-r border-zinc-200 py-5 pr-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Duration</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ duration(session.started_at, session.ended_at) }}</dd></div><div class="border-r border-zinc-200 px-5 py-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Requests</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ session.request_count }}</dd></div><div class="border-r border-zinc-200 py-5 pr-5 md:px-5 dark:border-zinc-800"><dt class="text-xs text-zinc-500">Input tokens</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_in) }}</dd></div><div class="py-5 pl-5"><dt class="text-xs text-zinc-500">Output tokens</dt><dd class="mt-1 font-mono text-sm text-zinc-900 dark:text-zinc-100">{{ compactNumber(session.tokens_out) }}</dd></div></dl>

    <div class="grid gap-6 border-b border-zinc-200 py-6 md:grid-cols-2 dark:border-zinc-800">
      <section aria-labelledby="capture-heading">
        <h2 id="capture-heading" class="flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><Database class="size-4" />Capture</h2>
        <p class="mt-2 text-sm text-zinc-700 dark:text-zinc-300"><strong class="font-medium capitalize">{{ session.capture.status }}</strong> · {{ session.capture.saved_exchanges }} saved · {{ session.capture.failed_exchanges }} failed · {{ session.capture.pending_exchanges }} pending</p>
        <p class="mt-1 text-xs text-zinc-500">Last saved {{ session.capture.last_saved_at ? shortDate(session.capture.last_saved_at) : "Never" }}</p>
      </section>
      <section aria-labelledby="outcome-heading">
        <h2 id="outcome-heading" class="text-sm font-semibold text-zinc-900 dark:text-zinc-100">Work outcome</h2>
        <p v-if="session.outcome_reason" class="mt-2 text-sm leading-6 text-zinc-700 dark:text-zinc-300">{{ session.outcome_reason }}</p>
        <p class="mt-1 text-xs text-zinc-500"><template v-if="session.outcome_src">Set by {{ session.outcome_src }}<template v-if="session.outcome_updated_at"> · {{ shortDate(session.outcome_updated_at) }}</template></template><template v-else>No outcome evidence recorded.</template></p>
      </section>
    </div>

    <div class="grid gap-8 pt-8 xl:grid-cols-[minmax(0,1fr)_320px]">
      <div><h2 class="mb-4 text-sm font-semibold text-zinc-900 dark:text-zinc-100">Request timeline</h2><div class="border-t border-zinc-200 dark:border-zinc-800"><RouterLink v-for="exchange in sessionExchanges" :key="exchange.id" :to="`/requests/${exchange.id}`" class="grid gap-3 border-b border-zinc-200 py-4 hover:bg-stone-50 sm:grid-cols-[120px_minmax(0,1fr)_100px] sm:px-3 dark:border-zinc-800 dark:hover:bg-zinc-900"><time class="font-mono text-xs text-zinc-500">{{ shortDate(exchange.ts) }}</time><div><div class="flex flex-wrap items-center gap-3"><IdentityBadge :label="exchange.provider" /><IdentityBadge :label="exchange.model" /></div><p class="mt-2 line-clamp-1 text-xs text-zinc-500">{{ JSON.parse(exchange.request).messages[0].content }}</p></div><div class="font-mono text-xs text-zinc-500 sm:text-right">{{ exchange.input_tokens.toLocaleString() }} in</div></RouterLink><p v-if="!sessionExchanges.length" class="py-10 text-sm text-zinc-500">No request evidence is included in this mock session.</p></div></div>
      <aside class="space-y-7">
        <div><h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><FileCode2 class="size-4" />Files</h2><ul class="divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800"><li v-for="file in session.files" :key="file" class="py-2.5 font-mono text-xs text-zinc-600 dark:text-zinc-400">{{ file }}</li></ul></div>
        <div>
          <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-900 dark:text-zinc-100"><TriangleAlert class="size-4" />Errors</h2>
          <ul v-if="session.errors.length" class="space-y-2"><li v-for="error in session.errors" :key="error" class="rounded-[5px] border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs leading-5 text-amber-900 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">{{ error }}</li></ul>
          <p v-else class="text-sm text-zinc-500">No errors detected.</p>
        </div>
      </aside>
    </div>
  </section>
  <section v-else class="py-20 text-center"><h1 class="text-xl font-semibold">Session not found</h1><RouterLink to="/sessions" class="mt-3 inline-block text-sm font-medium text-teal-700 dark:text-teal-400">Return to sessions</RouterLink></section>
</template>
