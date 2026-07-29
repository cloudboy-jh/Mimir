import { onBeforeUnmount, ref, watch, type Ref } from "vue";
import { getFacets, type Facets } from "@/lib/api";
import type { SelectOption } from "@/lib/options";

const empty: Facets = { repos: [], apps: [], models: [], providers: [], finish_reasons: [] };

// useFacets loads the filter vocabulary for a surface. Failures stay silent:
// a filter dropdown with no options is a degraded control, not a page error.
export function useFacets(sessionId?: Ref<string | undefined>) {
  const facets = ref<Facets>({ ...empty });
  let controller: AbortController | null = null;

  async function load() {
    controller?.abort();
    const active = new AbortController();
    controller = active;
    try {
      const result = await getFacets(sessionId?.value, active.signal);
      if (!active.signal.aborted) facets.value = result;
    } catch {
      if (!active.signal.aborted) facets.value = { ...empty };
    }
  }

  watch(() => sessionId?.value, () => void load(), { immediate: true });
  onBeforeUnmount(() => controller?.abort());
  return { facets, reloadFacets: load };
}

// facetSelectOptions keeps an already-selected value usable even when it falls
// outside the bounded facet list, so a URL filter never renders as blank.
export function facetSelectOptions(values: string[], selected: string, allLabel: string): SelectOption[] {
  const present = selected && !values.includes(selected) ? [selected, ...values] : values;
  return [{ value: "", label: allLabel }, ...present.map((value) => ({ value, label: value }))];
}
