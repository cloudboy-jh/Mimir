import type { SelectOption } from "./options";

export const requestKindOptions: SelectOption[] = [
  { value: "", label: "All request kinds" },
  { value: "primary", label: "Primary conversation" },
  { value: "title", label: "Title generation" },
  { value: "summary", label: "Summary generation" },
  { value: "compaction", label: "Context compaction" },
];
export const exchangeCaptureOptions: SelectOption[] = [
  { value: "", label: "All capture states" },
  { value: "saved", label: "Saved" },
  { value: "accepted", label: "Pending" },
  { value: "failed", label: "Capture failed" },
];
export const errorFilterOptions: SelectOption[] = [
  { value: "", label: "All requests" },
  { value: "true", label: "Errors or capture failures" },
];
