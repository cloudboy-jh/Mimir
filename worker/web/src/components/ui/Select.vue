<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Check, ChevronDown, Search } from "lucide-vue-next";
import { ComboboxAnchor, ComboboxContent, ComboboxEmpty, ComboboxInput, ComboboxItem, ComboboxItemIndicator, ComboboxPortal, ComboboxRoot, ComboboxTrigger, ComboboxViewport, SelectContent, SelectIcon, SelectItem, SelectItemIndicator, SelectItemText, SelectPortal, SelectRoot, SelectTrigger, SelectViewport } from "reka-ui";
import { repositoryOption, type SelectOption } from "@/lib/options";
import { cn } from "@/lib/utils";

const props = withDefaults(defineProps<{ modelValue: string; options: SelectOption[]; label: string; placeholder?: string; class?: string; disabled?: boolean; searchable?: boolean }>(), { placeholder: "Select", searchable: undefined });
const emit = defineEmits<{ "update:modelValue": [string] }>();
const open = ref(false);
const query = ref("");
const repository = computed(() => props.label.toLowerCase() === "repository");
const searchable = computed(() => props.searchable ?? (repository.value || props.options.length > 8));
// Encode every value, including the empty "all" value, rather than reserving a
// sentinel that could also be a real repository, model, ref, or session ID.
const selected = computed({
  get: () => `value:${props.modelValue}`,
  set: (value: string) => emit("update:modelValue", value.slice(6)),
});
const remembered = ref<SelectOption | undefined>();
const options = computed(() => props.options.map((option) => repository.value && !option.description ? repositoryOption(option.value, option.label) : option));
watch([options, () => props.modelValue], ([items, value]) => {
  const current = items.find((item) => item.value === value);
  if (current) remembered.value = current;
  else if (remembered.value?.value !== value) remembered.value = value ? { value, label: value, description: "Selected, not in the loaded options" } : undefined;
}, { immediate: true });
const items = computed(() => {
  const retained = remembered.value;
  const all = retained && !options.value.some((item) => item.value === retained.value) ? [retained, ...options.value] : options.value;
  return all.map((option) => ({ ...option, key: `value:${option.value}`, searchText: `${option.label} ${option.description ?? ""} ${option.value}` }));
});
const display = computed(() => remembered.value?.label ?? props.placeholder);
watch(open, () => { query.value = ""; });
watch(() => props.disabled, (disabled) => { if (disabled) open.value = false; });
const triggerClass = computed(() => cn('inline-flex h-8.5 min-w-0 max-w-full items-center justify-between gap-2 rounded-[5px] border border-zinc-300 bg-white px-2.5 text-left text-[13px] text-zinc-800 transition-colors duration-150 ease-out hover:border-zinc-400 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 disabled:pointer-events-none disabled:opacity-45 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-200 dark:hover:border-zinc-600 dark:focus-visible:outline-teal-400', props.class));
const itemClass = 'relative flex cursor-pointer items-center rounded-[4px] py-1.5 pl-7 pr-2.5 text-[13px] text-zinc-700 outline-none select-none data-[highlighted]:bg-stone-100 data-[highlighted]:text-zinc-950 dark:text-zinc-300 dark:data-[highlighted]:bg-zinc-800 dark:data-[highlighted]:text-zinc-50';
</script>

<template>
  <ComboboxRoot v-if="searchable" v-model="selected" v-model:open="open" :disabled="disabled" :reset-search-term-on-select="false" :reset-search-term-on-blur="false" class="contents">
    <ComboboxAnchor as-child>
      <ComboboxTrigger :aria-label="label" tabindex="0" :class="triggerClass" @keydown.down.up.prevent="open = true">
        <span class="min-w-0 truncate">{{ display }}</span><ChevronDown class="size-3.5 shrink-0 text-zinc-500" aria-hidden="true" />
      </ComboboxTrigger>
    </ComboboxAnchor>
    <ComboboxPortal>
      <ComboboxContent position="popper" align="start" :side-offset="5" :collision-padding="12" class="z-50 max-h-[min(24rem,var(--reka-combobox-content-available-height))] w-[max(16rem,var(--reka-combobox-trigger-width))] max-w-[calc(100vw-1.5rem)] origin-(--reka-combobox-content-transform-origin) overflow-hidden rounded-[5px] border border-zinc-200 bg-white shadow-[0_18px_50px_rgba(0,0,0,0.18)] data-[state=closed]:animate-popover-out data-[state=open]:animate-popover-in motion-reduce:animate-none dark:border-zinc-700 dark:bg-zinc-900">
        <div class="relative shrink-0 border-b border-zinc-200 dark:border-zinc-700">
          <Search class="pointer-events-none absolute left-3 top-3 size-3.5 text-zinc-500" aria-hidden="true" />
          <ComboboxInput v-model="query" :aria-label="`Search ${label.toLowerCase()}`" :placeholder="`Search ${label.toLowerCase()}…`" class="h-10 w-full bg-transparent pl-9 pr-3 text-[13px] text-zinc-900 outline-none placeholder:text-zinc-500 dark:text-zinc-100" />
        </div>
        <ComboboxViewport class="min-h-0 overflow-y-auto overscroll-contain p-1">
          <ComboboxEmpty class="px-3 py-5 text-[13px] text-zinc-600 dark:text-zinc-400" role="status">No matching options.</ComboboxEmpty>
          <ComboboxItem v-for="item in items" :key="item.key" :value="item.key" :text-value="item.searchText" :class="itemClass">
            <ComboboxItemIndicator class="absolute left-2 inline-flex"><Check class="size-3.5 text-teal-700 dark:text-teal-400" aria-hidden="true" /></ComboboxItemIndicator>
            <span class="min-w-0"><span class="block break-words font-medium">{{ item.label }}</span><span v-if="item.description" class="mt-0.5 block break-all text-xs leading-4 text-zinc-500 dark:text-zinc-400">{{ item.description }}</span></span>
          </ComboboxItem>
        </ComboboxViewport>
      </ComboboxContent>
    </ComboboxPortal>
  </ComboboxRoot>
  <SelectRoot v-else v-model="selected" :disabled="disabled">
    <SelectTrigger :aria-label="label" :class="triggerClass">
      <span class="min-w-0 truncate">{{ display }}</span>
      <SelectIcon as-child><ChevronDown class="size-3.5 shrink-0 text-zinc-500" aria-hidden="true" /></SelectIcon>
    </SelectTrigger>
    <SelectPortal>
      <SelectContent position="popper" :side-offset="5" :collision-padding="12" class="z-50 max-h-[min(16rem,var(--reka-select-content-available-height))] min-w-(--reka-select-trigger-width) max-w-[calc(100vw-1.5rem)] origin-(--reka-select-content-transform-origin) overflow-hidden rounded-[5px] border border-zinc-200 bg-white shadow-[0_18px_50px_rgba(0,0,0,0.18)] data-[state=closed]:animate-popover-out data-[state=open]:animate-popover-in motion-reduce:animate-none dark:border-zinc-700 dark:bg-zinc-900">
        <SelectViewport class="p-1">
          <SelectItem v-for="item in items" :key="item.key" :value="item.key" :text-value="item.label" :class="itemClass">
            <SelectItemIndicator class="absolute left-2 inline-flex"><Check class="size-3.5 text-teal-700 dark:text-teal-400" aria-hidden="true" /></SelectItemIndicator>
            <span class="min-w-0"><SelectItemText class="break-words">{{ item.label }}</SelectItemText><span v-if="item.description" class="mt-0.5 block break-all text-xs leading-4 text-zinc-500 dark:text-zinc-400">{{ item.description }}</span></span>
          </SelectItem>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>
