<script setup lang="ts">
import { computed } from "vue";
import { Check, ChevronDown } from "lucide-vue-next";
import { SelectContent, SelectIcon, SelectItem, SelectItemIndicator, SelectItemText, SelectPortal, SelectRoot, SelectTrigger, SelectValue, SelectViewport } from "reka-ui";
import type { SelectOption } from "@/lib/options";
import { cn } from "@/lib/utils";

// Reka's select treats an empty string as "no value", so an explicit "all"
// option is carried through the listbox under a sentinel key.
const ANY = "__any__";

const props = withDefaults(defineProps<{ modelValue: string; options: SelectOption[]; label: string; placeholder?: string; class?: string; disabled?: boolean }>(), { placeholder: "Select" });
const emit = defineEmits<{ "update:modelValue": [string] }>();

const selected = computed({
  get: () => props.modelValue === "" ? ANY : props.modelValue,
  set: (value: string) => emit("update:modelValue", value === ANY ? "" : value),
});
const items = computed(() => props.options.map((option) => ({ label: option.label, value: option.value === "" ? ANY : option.value })));
</script>

<template>
  <SelectRoot v-model="selected" :disabled="disabled">
    <SelectTrigger :aria-label="label" :class="cn('inline-flex h-8.5 items-center justify-between gap-2 rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] text-zinc-800 transition-colors duration-150 ease-out hover:border-zinc-400 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:pointer-events-none disabled:opacity-45 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:border-zinc-600 dark:focus-visible:outline-teal-400', props.class)">
      <SelectValue class="truncate" :placeholder="placeholder" />
      <SelectIcon as-child><ChevronDown class="size-3.5 shrink-0 text-zinc-500" aria-hidden="true" /></SelectIcon>
    </SelectTrigger>
    <SelectPortal>
      <SelectContent position="popper" :side-offset="5" class="z-50 max-h-64 min-w-(--reka-select-trigger-width) overflow-hidden rounded-[5px] border border-zinc-200 bg-white shadow-[0_18px_50px_rgba(0,0,0,0.18)] dark:border-zinc-700 dark:bg-zinc-900">
        <SelectViewport class="p-1">
          <SelectItem v-for="item in items" :key="item.value" :value="item.value" class="relative flex cursor-pointer items-center rounded-[4px] py-1.5 pl-7 pr-2.5 text-[13px] text-zinc-700 outline-none select-none data-[highlighted]:bg-stone-100 data-[highlighted]:text-zinc-950 dark:text-zinc-300 dark:data-[highlighted]:bg-zinc-800 dark:data-[highlighted]:text-zinc-50">
            <SelectItemIndicator class="absolute left-2 inline-flex"><Check class="size-3.5 text-teal-700 dark:text-teal-400" aria-hidden="true" /></SelectItemIndicator>
            <SelectItemText>{{ item.label }}</SelectItemText>
          </SelectItem>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>
