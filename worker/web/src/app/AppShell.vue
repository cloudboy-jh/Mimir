<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { demoMode } from "@/lib/data-source";
import Header from "./Header.vue";

const route = useRoute();
const standalone = computed(() => route.meta.standalone === true);
</script>

<template>
  <div class="min-h-screen bg-stone-100 text-zinc-950 dark:bg-stone-950 dark:text-zinc-100">
    <template v-if="standalone"><RouterView /></template>
    <template v-else>
      <Header />
      <aside v-if="demoMode" aria-label="Demo data notice" class="border-b border-stone-300 bg-stone-200/70 dark:border-zinc-700 dark:bg-zinc-900">
        <p class="mx-auto w-full max-w-[1500px] px-4 py-2 text-sm text-zinc-700 sm:px-6 lg:px-8 dark:text-zinc-300">
          <strong class="font-semibold text-zinc-950 dark:text-zinc-100">Sample data.</strong>
          These sessions are synthetic, and changes reset when you reload the demo.
        </p>
      </aside>
      <main class="mx-auto w-full max-w-[1500px] px-4 py-8 sm:px-6 lg:px-8 lg:py-10"><RouterView /></main>
    </template>
  </div>
</template>
