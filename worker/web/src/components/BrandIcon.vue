<script setup lang="ts">
import { computed } from "vue";
import openai from "@lobehub/icons-static-svg/icons/openai.svg";
import anthropic from "@lobehub/icons-static-svg/icons/anthropic.svg";
import bedrock from "@lobehub/icons-static-svg/icons/bedrock-color.svg";
import google from "@lobehub/icons-static-svg/icons/google-color.svg";
import claude from "@lobehub/icons-static-svg/icons/claude-color.svg";
import gemini from "@lobehub/icons-static-svg/icons/gemini-color.svg";
import opencode from "@lobehub/icons-static-svg/icons/opencode.svg";
import hermes from "@lobehub/icons-static-svg/icons/hermesagent.svg";
import nous from "@lobehub/icons-static-svg/icons/nousresearch.svg";
import claudeCode from "@lobehub/icons-static-svg/icons/claudecode-color.svg";
import openrouter from "@lobehub/icons-static-svg/icons/openrouter.svg";
import xai from "@lobehub/icons-static-svg/icons/xai.svg";
import grok from "@lobehub/icons-static-svg/icons/grok.svg";
import deepseek from "@lobehub/icons-static-svg/icons/deepseek-color.svg";
import mistral from "@lobehub/icons-static-svg/icons/mistral-color.svg";
import metaai from "@lobehub/icons-static-svg/icons/metaai-color.svg";
import qwen from "@lobehub/icons-static-svg/icons/qwen-color.svg";
import ollama from "@lobehub/icons-static-svg/icons/ollama.svg";
import groq from "@lobehub/icons-static-svg/icons/groq.svg";
import cerebras from "@lobehub/icons-static-svg/icons/cerebras-color.svg";
import kimi from "@lobehub/icons-static-svg/icons/kimi-color.svg";
import moonshot from "@lobehub/icons-static-svg/icons/moonshot.svg";
import azure from "@lobehub/icons-static-svg/icons/azure-color.svg";
import vertexai from "@lobehub/icons-static-svg/icons/vertexai-color.svg";
import cohere from "@lobehub/icons-static-svg/icons/cohere-color.svg";
import perplexity from "@lobehub/icons-static-svg/icons/perplexity-color.svg";
import together from "@lobehub/icons-static-svg/icons/together-color.svg";
import fireworks from "@lobehub/icons-static-svg/icons/fireworks-color.svg";
import nvidia from "@lobehub/icons-static-svg/icons/nvidia-color.svg";

const props = defineProps<{ label: string }>();

// Matches run most specific first: a harness or product name must win over the
// vendor name it contains, and a vendor must win over a bare model family.
const matchers: Array<{ test: (value: string) => boolean; src: string; monochrome: boolean }> = [
  { test: (v) => v.includes("claude code") || v.includes("claudecode"), src: claudeCode, monochrome: false },
  { test: (v) => v.includes("opencode"), src: opencode, monochrome: true },
  { test: (v) => v.includes("openrouter"), src: openrouter, monochrome: true },
  { test: (v) => v.includes("hermes"), src: hermes, monochrome: true },
  { test: (v) => v.includes("nous"), src: nous, monochrome: true },
  { test: (v) => v.includes("bedrock"), src: bedrock, monochrome: false },
  { test: (v) => v.includes("vertex"), src: vertexai, monochrome: false },
  { test: (v) => v.includes("azure"), src: azure, monochrome: false },
  { test: (v) => v.includes("anthropic"), src: anthropic, monochrome: true },
  { test: (v) => v.includes("claude"), src: claude, monochrome: false },
  { test: (v) => v.includes("openai") || v.startsWith("gpt") || v.startsWith("o1") || v.startsWith("o3") || v.startsWith("o4") || v.startsWith("codex"), src: openai, monochrome: true },
  { test: (v) => v.includes("gemini"), src: gemini, monochrome: false },
  { test: (v) => v === "google" || v.includes("google"), src: google, monochrome: false },
  { test: (v) => v.includes("grok"), src: grok, monochrome: true },
  { test: (v) => v.includes("xai") || v === "x-ai", src: xai, monochrome: true },
  { test: (v) => v.includes("deepseek"), src: deepseek, monochrome: false },
  { test: (v) => v.includes("mistral") || v.includes("mixtral") || v.includes("magistral") || v.includes("devstral"), src: mistral, monochrome: false },
  { test: (v) => v.includes("llama") || v.includes("meta"), src: metaai, monochrome: false },
  { test: (v) => v.includes("qwen") || v.includes("alibaba"), src: qwen, monochrome: false },
  { test: (v) => v.includes("ollama"), src: ollama, monochrome: true },
  { test: (v) => v.includes("groq"), src: groq, monochrome: true },
  { test: (v) => v.includes("cerebras"), src: cerebras, monochrome: false },
  { test: (v) => v.includes("kimi"), src: kimi, monochrome: false },
  { test: (v) => v.includes("moonshot"), src: moonshot, monochrome: true },
  { test: (v) => v.includes("cohere") || v.includes("command-r"), src: cohere, monochrome: false },
  { test: (v) => v.includes("perplexity") || v.includes("sonar"), src: perplexity, monochrome: false },
  { test: (v) => v.includes("together"), src: together, monochrome: false },
  { test: (v) => v.includes("fireworks"), src: fireworks, monochrome: false },
  { test: (v) => v.includes("nvidia") || v.includes("nemotron"), src: nvidia, monochrome: false },
];

const icon = computed(() => {
  const value = props.label.toLowerCase();
  return matchers.find((matcher) => matcher.test(value)) ?? null;
});
</script>

<template>
  <img v-if="icon" :src="icon.src" alt="" class="size-5 shrink-0 object-contain" :class="icon.monochrome ? 'dark:invert' : ''" />
  <span v-else aria-hidden="true" class="grid size-5 shrink-0 place-items-center rounded-[4px] border border-zinc-200 bg-zinc-50 text-xs font-semibold text-zinc-700 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-200">{{ label.charAt(0).toUpperCase() }}</span>
</template>
