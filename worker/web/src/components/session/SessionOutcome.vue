<script setup lang="ts">
import { History as HistoryIcon } from "lucide-vue-next";
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from "reka-ui";
import OutcomeBadge from "@/components/OutcomeBadge.vue";
import Button from "@/components/ui/Button.vue";
import SessionOutcomeEditor from "./SessionOutcomeEditor.vue";
import type { SessionDetail } from "@/lib/api";
import { shortDate } from "@/lib/format";

defineProps<{ detail: SessionDetail }>();
defineEmits<{ saved: [] }>();
</script>

<template>
  <section aria-labelledby="outcome-heading" class="border-t border-zinc-200 pt-4 first:border-t-0 first:pt-0 dark:border-zinc-800">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 id="outcome-heading" tabindex="-1" class="scroll-mt-20 text-sm font-semibold text-zinc-900 focus:outline-none dark:text-zinc-100">Work outcome</h2>
      <div class="flex flex-wrap items-center gap-1.5">
        <PopoverRoot v-if="detail.outcome_events.length">
          <PopoverTrigger as-child><Button variant="ghost" class="px-2.5"><HistoryIcon class="size-3.5" />History · {{ detail.outcome_events.length }}</Button></PopoverTrigger>
          <PopoverPortal>
            <PopoverContent align="end" :side-offset="8" :collision-padding="16" class="z-50 max-h-[min(30rem,calc(100vh-2rem))] w-[min(26rem,calc(100vw-2rem))] origin-(--reka-popover-content-transform-origin) overflow-y-auto rounded-[7px] border border-zinc-200 bg-white p-4 shadow-[0_18px_50px_rgba(0,0,0,0.18)] focus:outline-none data-[state=closed]:animate-popover-out data-[state=open]:animate-popover-in motion-reduce:animate-none dark:border-zinc-700 dark:bg-zinc-900">
              <h3 class="text-sm font-semibold text-zinc-950 dark:text-zinc-50">Outcome history</h3>
              <ol class="mt-3 divide-y divide-zinc-200 border-y border-zinc-200 dark:divide-zinc-800 dark:border-zinc-800">
                <li v-for="event in detail.outcome_events" :key="event.id" class="py-3">
                  <div class="flex flex-wrap items-center gap-x-2.5 gap-y-1"><OutcomeBadge :outcome="event.outcome" /><span class="text-xs text-zinc-500">{{ event.source }} · {{ shortDate(event.created_at) }}</span></div>
                  <p v-if="event.reason" class="mt-2 text-xs leading-5 text-zinc-600 dark:text-zinc-400">{{ event.reason }}</p>
                </li>
              </ol>
            </PopoverContent>
          </PopoverPortal>
        </PopoverRoot>
        <SessionOutcomeEditor :detail="detail" @saved="$emit('saved')" />
      </div>
    </div>
    <p v-if="detail.session.outcome_src" class="mt-2 text-xs text-zinc-600 dark:text-zinc-400">Set by {{ detail.session.outcome_src }}<template v-if="detail.session.outcome_updated_at"> · {{ shortDate(detail.session.outcome_updated_at) }}</template></p>
  </section>
</template>
