import { CircleCheck, CircleDashed, CircleOff, CircleX } from "lucide-vue-next";
import type { FunctionalComponent } from "vue";
import type { Outcome } from "@/lib/api";

export type OutcomeMeta = {
  label: string;
  description: string;
  icon: FunctionalComponent;
  // badge is the resting chip treatment; selected is the committed fill used by
  // the outcome selector. Both stay in one place so they cannot drift apart.
  badge: string;
  selected: string;
  accent: string;
};

export const outcomeOrder: Outcome[] = ["landed", "discarded", "abandoned", "unresolved"];

export const outcomeMeta: Record<Outcome, OutcomeMeta> = {
  landed: {
    label: "Landed",
    description: "The result was kept or shipped.",
    icon: CircleCheck,
    badge: "border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-300",
    selected: "border-emerald-700 bg-emerald-700 text-white dark:border-emerald-400 dark:bg-emerald-500 dark:text-emerald-950",
    accent: "text-emerald-700 dark:text-emerald-400",
  },
  discarded: {
    label: "Discarded",
    description: "The result was deliberately rejected or reverted.",
    icon: CircleX,
    badge: "border-red-200 bg-red-50 text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-300",
    selected: "border-red-700 bg-red-700 text-white dark:border-red-400 dark:bg-red-500 dark:text-red-950",
    accent: "text-red-700 dark:text-red-400",
  },
  abandoned: {
    label: "Abandoned",
    description: "Work stopped without a result.",
    icon: CircleOff,
    badge: "border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-300",
    selected: "border-amber-600 bg-amber-600 text-white dark:border-amber-400 dark:bg-amber-500 dark:text-amber-950",
    accent: "text-amber-700 dark:text-amber-400",
  },
  unresolved: {
    label: "Unresolved",
    description: "No evidenced result has been recorded yet.",
    icon: CircleDashed,
    badge: "border-zinc-200 bg-zinc-50 text-zinc-600 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-300",
    selected: "border-zinc-900 bg-zinc-900 text-white dark:border-zinc-100 dark:bg-zinc-100 dark:text-zinc-950",
    accent: "text-zinc-600 dark:text-zinc-400",
  },
};
