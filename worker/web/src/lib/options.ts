export type SelectOption = { value: string; label: string; description?: string };

export function repositoryOption(value: string, label = value, description?: string): SelectOption {
  if (!value) return { value, label, description };
  const path = label.replace(/^https?:\/\//, "").replace(/\/$/, "");
  const parts = path.split("/");
  const name = parts.at(-1) || label;
  return { value, label: name, description: description ?? (parts.length > 1 ? parts.slice(0, -1).join("/") : value !== label ? value : undefined) };
}

export const orderOptions: SelectOption[] = [
  { value: "desc", label: "Newest first" },
  { value: "asc", label: "Oldest first" },
];

export const pageSizeOptions: SelectOption[] = [10, 25, 50, 100].map((size) => ({ value: String(size), label: `${size} rows` }));

export const outcomeOptions: SelectOption[] = [
  { value: "", label: "All outcomes" },
  { value: "landed", label: "Landed" },
  { value: "discarded", label: "Discarded" },
  { value: "abandoned", label: "Abandoned" },
  { value: "unresolved", label: "Unresolved" },
];

export const evidenceKindOptions: SelectOption[] = [
  { value: "none", label: "No evidence" },
  { value: "commit", label: "Commit" },
  { value: "url", label: "URL" },
  { value: "note", label: "Note" },
];
