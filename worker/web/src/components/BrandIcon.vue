<script setup lang="ts">
import { computed } from "vue";
import openai from "@lobehub/icons-static-svg/icons/openai.svg";
import bedrock from "@lobehub/icons-static-svg/icons/bedrock-color.svg";
import google from "@lobehub/icons-static-svg/icons/google-color.svg";
import claude from "@lobehub/icons-static-svg/icons/claude-color.svg";
import gemini from "@lobehub/icons-static-svg/icons/gemini-color.svg";
import opencode from "@lobehub/icons-static-svg/icons/opencode.svg";
import hermes from "@lobehub/icons-static-svg/icons/hermesagent.svg";
import claudeCode from "@lobehub/icons-static-svg/icons/claudecode-color.svg";

const props = defineProps<{ label: string }>();

const icon = computed(() => {
  const value = props.label.toLowerCase();
  if (value.includes("openai") || value.startsWith("gpt")) return { src: openai, monochrome: true };
  if (value.includes("bedrock")) return { src: bedrock, monochrome: false };
  if (value === "google") return { src: google, monochrome: false };
  if (value.includes("claude code")) return { src: claudeCode, monochrome: false };
  if (value.includes("claude")) return { src: claude, monochrome: false };
  if (value.includes("gemini")) return { src: gemini, monochrome: false };
  if (value.includes("opencode")) return { src: opencode, monochrome: true };
  if (value.includes("hermes")) return { src: hermes, monochrome: true };
  return null;
});
</script>

<template>
  <img v-if="icon" :src="icon.src" alt="" class="size-5 shrink-0 object-contain" :class="icon.monochrome ? 'dark:invert' : ''" />
  <span v-else aria-hidden="true" class="grid size-5 shrink-0 place-items-center rounded-[4px] border border-zinc-200 bg-zinc-50 text-xs font-semibold text-zinc-700 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-200">{{ label.charAt(0).toUpperCase() }}</span>
</template>
