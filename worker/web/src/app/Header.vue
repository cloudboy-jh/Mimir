<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { LogOut } from "lucide-vue-next";
import logo from "../../../../assets/images/mimir-readme.png";
import { getIdentity, type DashboardIdentity } from "@/lib/api";
import ThemeToggle from "./ThemeToggle.vue";

const route = useRoute();
const links = [{ to: "/sessions", label: "Sessions" }, { to: "/requests", label: "Requests" }, { to: "/overview", label: "Overview" }, { to: "/settings", label: "Settings" }];
const active = (path: string) => route.path === path || route.path.startsWith(`${path}/`);
const identity = ref<DashboardIdentity | null>(null);
let controller: AbortController | null = null;

const identityLabel = computed(() => identity.value?.name || identity.value?.email || "Mimir user");
const initials = computed(() => {
  if (identity.value?.source === "local-development") return "DEV";
  const source = identity.value?.name || identity.value?.email?.split("@")[0] || "M";
  return source.split(/[\s._-]+/).filter(Boolean).slice(0, 2).map((part) => part[0]?.toUpperCase()).join("") || "M";
});

onMounted(async () => {
  controller = new AbortController();
  try {
    identity.value = await getIdentity(controller.signal);
  } catch {
    // The shared API handler routes authentication failures to the login page.
  }
});
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <header class="sticky top-0 z-20 border-b border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-950">
    <div class="mx-auto flex h-15 w-full max-w-[1500px] items-center">
      <RouterLink to="/sessions" aria-label="Mimir sessions" class="flex h-9 shrink-0 items-center focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 sm:h-10"><img :src="logo" alt="mimir" class="h-9 w-auto max-w-none sm:h-11" /></RouterLink>
      <nav aria-label="Primary" class="ml-4 flex h-full min-w-0 flex-1 items-stretch gap-1 overflow-x-auto sm:ml-8 sm:flex-none sm:gap-3 sm:overflow-visible">
        <RouterLink v-for="link in links" :key="link.to" :to="link.to" class="flex shrink-0 items-center gap-2 px-1.5 text-xs font-medium text-zinc-500 transition-colors hover:text-zinc-950 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-teal-600 sm:px-2 sm:text-[13px] dark:text-zinc-400 dark:hover:text-zinc-100" :class="active(link.to) ? 'text-zinc-950 dark:text-zinc-100' : ''">
          <span class="size-1.5 shrink-0" :class="active(link.to) ? 'bg-teal-600 dark:bg-teal-400' : 'bg-transparent'" />
          {{ link.label }}
        </RouterLink>
      </nav>
      <div class="ml-auto flex items-center gap-2">
        <div v-if="identity" class="flex h-8.5 min-w-0 items-center gap-2 rounded-[5px] border border-zinc-200 bg-stone-50 px-2 pr-2.5 dark:border-zinc-800 dark:bg-zinc-900" :title="identity.email || identityLabel">
          <span class="flex h-5 min-w-5 items-center justify-center rounded-[3px] bg-zinc-900 px-1 font-mono text-[9px] font-medium text-zinc-50 dark:bg-zinc-100 dark:text-zinc-900">{{ initials }}</span>
          <span class="hidden max-w-40 truncate text-xs font-medium text-zinc-700 md:block dark:text-zinc-300">{{ identityLabel }}</span>
        </div>
        <a href="/cdn-cgi/access/logout" aria-label="Sign out" title="Sign out" class="inline-flex h-8.5 items-center justify-center gap-2 rounded-[5px] border border-transparent px-2.5 text-[13px] font-medium text-zinc-600 transition-colors duration-150 ease-out hover:border-zinc-200 hover:bg-zinc-100 hover:text-zinc-950 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:text-zinc-400 dark:hover:border-zinc-800 dark:hover:bg-zinc-900 dark:hover:text-zinc-100">
          <LogOut class="size-4" />
          <span class="hidden sm:inline">Sign out</span>
        </a>
        <ThemeToggle />
      </div>
    </div>
  </header>
</template>
