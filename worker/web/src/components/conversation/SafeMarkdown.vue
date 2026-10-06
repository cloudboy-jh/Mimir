<script setup lang="ts">
import { computed } from "vue";
import { renderSafeMarkdown } from "@/lib/safe-markdown";
const props = defineProps<{ text: string }>();
const html = computed(() => renderSafeMarkdown(props.text));
function focusableCode(element: HTMLElement) {
  for (const block of element.querySelectorAll("pre")) block.tabIndex = 0;
}
const vFocusableCode = { mounted: focusableCode, updated: focusableCode };
</script>

<template><div v-focusable-code class="saved-markdown min-w-0 text-base leading-7 text-zinc-800 dark:text-zinc-200" v-html="html" /></template>

<style scoped>
.saved-markdown { overflow-wrap: anywhere; }
.saved-markdown :deep(p), .saved-markdown :deep(ul), .saved-markdown :deep(ol), .saved-markdown :deep(blockquote), .saved-markdown :deep(pre) { margin-block: 1rem; }
.saved-markdown :deep(> :first-child) { margin-top: 0; }
.saved-markdown :deep(> :last-child) { margin-bottom: 0; }
.saved-markdown :deep(p), .saved-markdown :deep(ul), .saved-markdown :deep(ol), .saved-markdown :deep(blockquote) { max-width: 70ch; }
.saved-markdown :deep(h1), .saved-markdown :deep(h2), .saved-markdown :deep(h3), .saved-markdown :deep(h4) { font-weight: 600; line-height: 1.4; margin-block: 1.5rem 0.75rem; }
.saved-markdown :deep(h1) { font-size: 1.5rem; }
.saved-markdown :deep(h2) { font-size: 1.25rem; }
.saved-markdown :deep(h3), .saved-markdown :deep(h4) { font-size: 1.125rem; }
.saved-markdown :deep(ul), .saved-markdown :deep(ol) { padding-left: 1.5rem; }
.saved-markdown :deep(ul) { list-style: disc; }
.saved-markdown :deep(ol) { list-style: decimal; }
.saved-markdown :deep(a) { color: var(--color-teal-700); text-decoration: underline; text-underline-offset: 3px; }
.saved-markdown :deep(a:focus-visible) { outline: 2px solid var(--color-teal-600); outline-offset: 2px; }
.saved-markdown :deep(code) { font-family: var(--font-mono); font-size: 0.85em; background: var(--color-stone-100); padding: 0.15rem 0.25rem; border-radius: 3px; }
.saved-markdown :deep(pre) { max-width: min(100%, 86ch); max-height: 36rem; overflow: auto; border: 1px solid var(--color-zinc-300); border-radius: 5px; background: var(--color-stone-100); padding: 1rem 1.25rem; font-size: 0.875rem; line-height: 1.7; tab-size: 2; }
.saved-markdown :deep(pre:focus-visible) { outline: 2px solid var(--color-teal-600); outline-offset: 2px; }
.saved-markdown :deep(pre code) { padding: 0; background: none; font-size: inherit; overflow-wrap: normal; white-space: pre; }
.saved-markdown :deep(blockquote) { border-left: 1px solid var(--color-zinc-300); padding-left: 1rem; color: var(--color-zinc-500); }
.saved-markdown :deep(table) { display: block; overflow-x: auto; border-collapse: collapse; }
.saved-markdown :deep(th), .saved-markdown :deep(td) { border: 1px solid var(--color-zinc-300); padding: 0.4rem 0.7rem; text-align: left; }
.saved-markdown :deep(img) { max-width: 100%; }
.saved-markdown :deep(.hljs-keyword), .saved-markdown :deep(.hljs-literal) { color: var(--color-purple-700); }
.saved-markdown :deep(.hljs-string), .saved-markdown :deep(.hljs-addition) { color: var(--color-teal-700); }
.saved-markdown :deep(.hljs-comment) { color: var(--color-zinc-600); }
.saved-markdown :deep(.hljs-number), .saved-markdown :deep(.hljs-deletion) { color: var(--color-red-700); }
.dark .saved-markdown :deep(a), .dark .saved-markdown :deep(.hljs-string), .dark .saved-markdown :deep(.hljs-addition) { color: var(--color-teal-400); }
.dark .saved-markdown :deep(code), .dark .saved-markdown :deep(pre) { background: var(--color-zinc-900); }
.dark .saved-markdown :deep(pre) { border-color: var(--color-zinc-700); }
.dark .saved-markdown :deep(.hljs-keyword), .dark .saved-markdown :deep(.hljs-literal) { color: var(--color-purple-300); }
.dark .saved-markdown :deep(.hljs-number), .dark .saved-markdown :deep(.hljs-deletion) { color: var(--color-red-300); }
.dark .saved-markdown :deep(.hljs-comment), .dark .saved-markdown :deep(blockquote) { color: var(--color-zinc-400); }
</style>
