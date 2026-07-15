<script setup lang="ts">
import { useRoute } from "vue-router";
import logo from "../../../../assets/images/mimir-readme.png";
import ThemeToggle from "./ThemeToggle.vue";

const route = useRoute();
const links = [{ to: "/sessions", label: "Sessions" }, { to: "/requests", label: "Requests" }, { to: "/overview", label: "Overview" }];
const active = (path: string) => route.path === path || route.path.startsWith(`${path}/`);
</script>

<template>
  <header class="sticky top-0 z-20 border-b border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-950">
    <div class="mx-auto flex h-15 w-full max-w-[1500px] items-center px-4 sm:px-6 lg:px-8">
      <RouterLink to="/sessions" aria-label="Mimir sessions" class="flex h-9 w-20 shrink-0 items-center focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 sm:h-10 sm:w-28"><img :src="logo" alt="mimir" class="h-9 w-auto max-w-none sm:h-11" /></RouterLink>
      <nav aria-label="Primary" class="ml-4 flex h-full min-w-0 flex-1 items-stretch gap-1 overflow-x-auto sm:ml-8 sm:flex-none sm:gap-3 sm:overflow-visible">
        <RouterLink v-for="link in links" :key="link.to" :to="link.to" class="flex shrink-0 items-center gap-2 px-1.5 text-xs font-medium text-zinc-500 transition-colors hover:text-zinc-950 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 sm:px-2 sm:text-[13px] dark:text-zinc-400 dark:hover:text-zinc-100" :class="active(link.to) ? 'text-zinc-950 dark:text-zinc-100' : ''">
          <span class="size-1.5 shrink-0" :class="active(link.to) ? 'bg-teal-600 dark:bg-teal-400' : 'bg-transparent'" />
          {{ link.label }}
        </RouterLink>
      </nav>
      <div class="ml-auto pl-2"><ThemeToggle /></div>
    </div>
  </header>
</template>
