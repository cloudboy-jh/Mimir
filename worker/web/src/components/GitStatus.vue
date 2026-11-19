<script setup lang="ts">
import { CloudUpload, GitCommitHorizontal } from "lucide-vue-next";
import type { GitStatus } from "@/lib/api";

defineProps<{ status: GitStatus; count?: number }>();
</script>

<template>
  <span v-if="status === 'pushed'" class="inline-flex items-center gap-1 font-medium text-emerald-800 dark:text-emerald-300"><CloudUpload class="size-3.5 shrink-0" aria-hidden="true" />Pushed<span v-if="count" class="font-mono font-normal">{{ count }}</span></span>
  <span v-else-if="status === 'committed'" class="inline-flex items-center gap-1 font-medium text-zinc-800 dark:text-zinc-200"><GitCommitHorizontal class="size-3.5 shrink-0" aria-hidden="true" />Committed<span v-if="count" class="font-mono font-normal">{{ count }}</span></span>
  <span v-else class="text-zinc-500 dark:text-zinc-400">No commits</span>
</template>
