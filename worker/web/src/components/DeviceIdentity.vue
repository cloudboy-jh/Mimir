<script setup lang="ts">
import { Apple, Laptop, Monitor, Terminal } from "lucide-vue-next";
import type { Component } from "vue";
import type { DeviceIdentity } from "@/lib/api";

const props = withDefaults(defineProps<{ device: DeviceIdentity; compact?: boolean }>(), { compact: false });

const platforms: Record<string, { label: string; icon: Component }> = {
  darwin: { label: "macOS", icon: Apple },
  macos: { label: "macOS", icon: Apple },
  windows: { label: "Windows", icon: Monitor },
  win32: { label: "Windows", icon: Monitor },
  linux: { label: "Linux", icon: Terminal },
};

const platform = platforms[props.device.platform.toLowerCase()] ?? { label: props.device.platform || "Unknown platform", icon: Laptop };
</script>

<template>
  <span class="inline-flex min-w-0 items-center gap-1.5 text-zinc-600 dark:text-zinc-400">
    <component :is="platform.icon" class="size-3.5 shrink-0" :stroke-width="1.75" aria-hidden="true" />
    <span class="truncate font-medium text-zinc-800 dark:text-zinc-200">{{ device.name }}</span>
    <span class="shrink-0">{{ platform.label }}</span>
    <span v-if="!compact" class="shrink-0 font-mono text-[11px]">{{ device.arch }}</span>
  </span>
</template>
