<script setup lang="ts">
import { computed } from "vue";
import { ArrowRight, LockKeyhole } from "lucide-vue-next";
import { useRoute } from "vue-router";
import wordmark from "../../../../assets/images/mimir-wordmark.png";
import ThemeToggle from "@/app/ThemeToggle.vue";

const route = useRoute();
const returnTo = computed(() => {
  const value = typeof route.query.returnTo === "string" ? route.query.returnTo : "/dashboard/sessions";
  return value.startsWith("/dashboard/") && !value.startsWith("//") ? value : "/dashboard/sessions";
});
const continueHref = computed(() => `/dashboard/auth?returnTo=${encodeURIComponent(returnTo.value)}`);
</script>

<template>
  <main class="relative flex min-h-screen items-center justify-center px-5 py-16 sm:px-8">
    <div class="absolute right-4 top-4 sm:right-6 sm:top-6"><ThemeToggle /></div>
    <section aria-labelledby="login-heading" class="w-full max-w-md">
      <img :src="wordmark" alt="Mimir" class="mx-auto h-auto w-full max-w-[360px] [image-rendering:pixelated]" />
      <div class="mt-10 border-y border-zinc-300 py-7 dark:border-zinc-700">
        <div class="flex items-start gap-3"><LockKeyhole class="mt-0.5 size-5 shrink-0 text-zinc-500" /><div><h1 id="login-heading" class="text-xl font-semibold tracking-[-0.02em] text-zinc-950 dark:text-zinc-50">Open your private dashboard</h1><p class="mt-2 text-sm leading-6 text-zinc-600 dark:text-zinc-400">Cloudflare Access verifies your identity before Mimir exposes session memory.</p></div></div>
        <a :href="continueHref" class="mt-6 inline-flex h-10 w-full items-center justify-center gap-2 rounded-[5px] bg-zinc-900 px-4 text-sm font-medium text-zinc-50 hover:bg-zinc-700 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-zinc-300">Continue with Cloudflare Access<ArrowRight class="size-4" /></a>
      </div>
      <p class="mt-4 text-center text-xs leading-5 text-zinc-500">Mimir does not store dashboard passwords or browser machine tokens.</p>
    </section>
  </main>
</template>
