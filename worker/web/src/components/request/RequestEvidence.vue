<script setup lang="ts">
import { computed } from "vue";
import type { LogEnvelope } from "@/lib/api";
import { structuredEvidence, type StructuredBlock } from "@/lib/structured-exchange";
import EvidenceMessages from "@/components/conversation/EvidenceMessages.vue";

const props = defineProps<{ envelope: LogEnvelope; side: "request" | "response"; originSession?: string }>();
const structured = computed(() => structuredEvidence(props.envelope, props.side));
const pairedResults = computed(() => {
  const results: Record<string, StructuredBlock[]> = Object.create(null);
  if (props.side === "request") for (const message of structuredEvidence(props.envelope, "response").messages) for (const block of message.blocks) {
    if (block.type === "tool-result" && block.callId) (results[block.callId] ??= []).push(block);
  }
  return results;
});
</script>

<template>
  <EvidenceMessages :messages="structured.messages" :paired-results="pairedResults" :origin-session="originSession" />
</template>
