<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import BrandIcon from "@/components/BrandIcon.vue";
import { harnessLabel } from "@/lib/format";
import { agentTime, share, type DayItem, type SliceTone } from "@/lib/day";
import { outcomeMeta, outcomeOrder } from "@/lib/outcomes";

const props = defineProps<{ entries: Array<{ item: DayItem; tone: SliceTone }>; caption: string }>();
const hovered = defineModel<string | null>("hovered", { required: true });
const emit = defineEmits<{ open: [DayItem] }>();

// Geometry lives in a 200-unit viewBox; the chart scales to its container.
const C = 100, OUTER = 96, INNER = 62, GAP = 0.025, LIFT = 4;
const ICON_MIN_SWEEP = 0.5, MODEL_MIN_SWEEP = 0.9;
const RING = `M${C} ${C - OUTER}A${OUTER} ${OUTER} 0 1 1 ${C} ${C + OUTER}A${OUTER} ${OUTER} 0 1 1 ${C} ${C - OUTER}ZM${C} ${C - INNER}A${INNER} ${INNER} 0 1 0 ${C} ${C + INNER}A${INNER} ${INNER} 0 1 0 ${C} ${C - INNER}Z`;

const total = computed(() => props.entries.reduce((sum, entry) => sum + entry.item.minutes, 0));
const point = (radius: number, angle: number) => [C + radius * Math.sin(angle), C - radius * Math.cos(angle)] as const;

const arcs = computed(() => {
  const visible = props.entries.filter((entry) => entry.item.minutes > 0);
  let angle = 0;
  return visible.map((entry) => {
    const sweep = (entry.item.minutes / total.value) * Math.PI * 2;
    const start = angle;
    angle += sweep;
    const mid = start + sweep / 2;
    let d: string;
    if (visible.length === 1) {
      d = RING;
    } else {
      const pad = Math.min(GAP / 2, sweep / 4);
      const a0 = start + pad, a1 = angle - pad, large = a1 - a0 > Math.PI ? 1 : 0;
      const [x0, y0] = point(OUTER, a0), [x1, y1] = point(OUTER, a1), [x2, y2] = point(INNER, a1), [x3, y3] = point(INNER, a0);
      d = `M${x0} ${y0}A${OUTER} ${OUTER} 0 ${large} 1 ${x1} ${y1}L${x2} ${y2}A${INNER} ${INNER} 0 ${large} 0 ${x3} ${y3}Z`;
    }
    return { ...entry, d, mid, sweep, lift: [LIFT * Math.sin(mid), -LIFT * Math.cos(mid)] as const };
  });
});

const chart = ref<HTMLElement | null>(null);
const width = ref(280);
let observer: ResizeObserver | null = null;
onMounted(() => {
  observer = new ResizeObserver(([entry]) => { if (entry) width.value = entry.contentRect.width; });
  if (chart.value) observer.observe(chart.value);
});
onBeforeUnmount(() => observer?.disconnect());
const scale = computed(() => width.value / 200);

// The tooltip keeps its last position while fading out so it never jumps.
const activeArc = computed(() => arcs.value.find((arc) => arc.item.key === hovered.value) ?? null);
const lastKey = ref<string | null>(null);
watch(activeArc, (arc) => { if (arc) lastKey.value = arc.item.key; });
const tooltipArc = computed(() => activeArc.value ?? arcs.value.find((arc) => arc.item.key === lastKey.value) ?? null);
const tooltipStyle = computed(() => {
  const arc = tooltipArc.value;
  if (!arc) return {};
  const [x, y] = point(OUTER + 8, arc.mid);
  // Anchor offsets vary continuously with the angle, so moving between
  // slices glides instead of flipping sides.
  return { transform: `translate(${x * scale.value}px, ${y * scale.value}px) translate(${-50 + 50 * Math.sin(arc.mid)}%, ${-50 - 50 * Math.cos(arc.mid)}%)` };
});

function badgeStyle(arc: (typeof arcs.value)[number]) {
  const [x, y] = point((OUTER + INNER) / 2, arc.mid);
  const lifted = hovered.value === arc.item.key;
  return { left: `${x / 2}%`, top: `${y / 2}%`, transform: `translate(-50%, -50%) translate(${lifted ? arc.lift[0] * scale.value : 0}px, ${lifted ? arc.lift[1] * scale.value : 0}px)` };
}

function outcomeLine(item: DayItem) {
  return outcomeOrder.filter((outcome) => item.outcomes[outcome]).map((outcome) => `${item.outcomes[outcome]} ${outcomeMeta[outcome].label.toLowerCase()}`).join(" · ");
}
const hint = (item: DayItem) => ("session" in item.open ? "Click to open the session" : "Click to zoom in");
</script>

<template>
  <div ref="chart" class="relative aspect-square w-full" @mouseleave="hovered = null">
    <svg viewBox="0 0 200 200" class="block size-full overflow-visible" role="group" :aria-label="`${agentTime(total)} ${caption}`">
      <path v-if="!arcs.length" :d="RING" fill-rule="evenodd" class="fill-stone-200 dark:fill-stone-800" />
      <path
        v-for="arc in arcs"
        :key="arc.item.key"
        :d="arc.d"
        fill-rule="evenodd"
        role="button"
        tabindex="0"
        :aria-label="`${arc.item.label}: ${agentTime(arc.item.minutes)}, ${share(arc.item.minutes, total)}. ${hint(arc.item)}.`"
        class="cursor-pointer stroke-[1.5] outline-none transition-[transform,opacity] duration-200 ease-[cubic-bezier(0.22,1,0.36,1)] focus-visible:stroke-teal-600 focus-visible:stroke-2 motion-reduce:transition-none dark:focus-visible:stroke-teal-400"
        :class="[arc.tone.fill, hovered && hovered !== arc.item.key ? 'opacity-40' : '']"
        :style="{ transform: hovered === arc.item.key ? `translate(${arc.lift[0]}px, ${arc.lift[1]}px)` : undefined }"
        @mouseenter="hovered = arc.item.key"
        @focus="hovered = arc.item.key"
        @blur="hovered = null"
        @click="emit('open', arc.item)"
        @keydown.enter.prevent="emit('open', arc.item)"
        @keydown.space.prevent="emit('open', arc.item)"
      />
    </svg>

    <template v-for="arc in arcs" :key="`badge-${arc.item.key}`">
      <span
        v-if="arc.item.app && arc.sweep >= ICON_MIN_SWEEP"
        aria-hidden="true"
        class="pointer-events-none absolute flex items-center gap-0.5 rounded-[5px] border border-zinc-200 bg-white p-0.5 shadow-[0_1px_2px_rgba(0,0,0,0.08)] transition-[transform,opacity] duration-200 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none dark:border-zinc-700 dark:bg-zinc-900"
        :class="hovered && hovered !== arc.item.key ? 'opacity-40' : ''"
        :style="badgeStyle(arc)"
      >
        <BrandIcon :label="harnessLabel(arc.item.app)" />
        <BrandIcon v-if="arc.item.model && arc.sweep >= MODEL_MIN_SWEEP" :label="arc.item.model" />
      </span>
    </template>

    <div class="pointer-events-none absolute inset-0 grid place-items-center">
      <div class="text-center">
        <p class="text-2xl font-semibold tracking-[-0.025em] text-zinc-950 dark:text-zinc-50">{{ agentTime(total) }}</p>
        <p class="mt-0.5 text-xs text-zinc-500 dark:text-zinc-400">{{ caption }}</p>
      </div>
    </div>

    <div
      v-if="tooltipArc"
      role="tooltip"
      class="pointer-events-none absolute left-0 top-0 z-20 w-64 rounded-[7px] border border-zinc-200 bg-white p-3 text-xs shadow-[0_18px_50px_rgba(0,0,0,0.18)] transition-[transform,opacity] duration-150 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none dark:border-zinc-700 dark:bg-zinc-900"
      :class="activeArc ? 'opacity-100' : 'opacity-0'"
      :style="tooltipStyle"
    >
      <div class="flex items-start gap-2">
        <BrandIcon v-if="tooltipArc.item.app" :label="harnessLabel(tooltipArc.item.app)" />
        <p class="line-clamp-2 min-w-0 text-[13px] font-medium text-zinc-950 dark:text-zinc-50">{{ tooltipArc.item.label }}</p>
      </div>
      <p v-if="tooltipArc.item.model" class="mt-2 flex min-w-0 items-center gap-2 text-zinc-700 dark:text-zinc-300"><BrandIcon :label="tooltipArc.item.model" /><span class="truncate font-mono">{{ tooltipArc.item.model }}</span></p>
      <p v-if="tooltipArc.item.repo" class="mt-1.5 truncate text-zinc-600 dark:text-zinc-400">{{ tooltipArc.item.repo }}<span v-if="tooltipArc.item.sourceRef"> · {{ tooltipArc.item.sourceRef }}</span></p>
      <p class="mt-2 text-zinc-800 dark:text-zinc-200"><span class="font-mono font-medium">{{ agentTime(tooltipArc.item.minutes) }}</span> · {{ share(tooltipArc.item.minutes, total) }}</p>
      <p class="mt-1 text-zinc-600 dark:text-zinc-400">
        <template v-if="tooltipArc.item.sessionIds.length > 1">{{ tooltipArc.item.sessionIds.length }} sessions · </template>{{ tooltipArc.item.requests }} {{ tooltipArc.item.requests === 1 ? "request" : "requests" }}<template v-if="tooltipArc.item.subAgents"> · {{ tooltipArc.item.subAgents }} sub-agents</template><template v-if="tooltipArc.item.commits"> · {{ tooltipArc.item.commits }} {{ tooltipArc.item.commits === 1 ? "commit" : "commits" }}</template>
      </p>
      <p v-if="tooltipArc.item.kind !== 'quick'" class="mt-1 text-zinc-600 dark:text-zinc-400">{{ outcomeLine(tooltipArc.item) }}</p>
      <p class="mt-2 border-t border-zinc-200 pt-2 text-zinc-500 dark:border-zinc-700">{{ hint(tooltipArc.item) }}</p>
    </div>
  </div>
</template>
