<script setup lang="ts">
import { Moon, Sun } from "lucide-vue-next";
import Button from "@/components/ui/Button.vue";
import Tooltip from "@/components/ui/Tooltip.vue";
import { setTheme, theme } from "@/lib/theme";
</script>

<template><Tooltip :label="theme === 'dark' ? 'Use light theme' : 'Use dark theme'"><Button variant="ghost" size="icon" :aria-label="theme === 'dark' ? 'Use light theme' : 'Use dark theme'" @click="setTheme(theme === 'dark' ? 'light' : 'dark')"><Sun v-if="theme === 'dark'" class="size-4" /><Moon v-else class="size-4" /></Button></Tooltip></template>
