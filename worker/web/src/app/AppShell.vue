<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import Header from "./Header.vue";

const route = useRoute();
const standalone = computed(() => route.meta.standalone === true);
</script>

<template><div class="min-h-screen bg-stone-100 text-zinc-950 dark:bg-zinc-950 dark:text-zinc-100"><template v-if="standalone"><RouterView /></template><template v-else><Header /><main class="mx-auto w-full max-w-[1500px] px-4 py-8 sm:px-6 lg:px-8 lg:py-10"><RouterView /></main></template></div></template>
