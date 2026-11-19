export type SelectOption = { value: string; label: string; description?: string };

export function repositoryOption(value: string, label = value, description?: string): SelectOption {
  if (!value) return { value, label, description };
  const path = label.replace(/^https?:\/\//, "").replace(/\/$/, "");
  const parts = path.split("/");
  const name = parts.at(-1) || label;
  return { value, label: name, description: description ?? (parts.length > 1 ? parts.slice(0, -1).join("/") : value !== label ? value : undefined) };
}

export const orderOptions: SelectOption[] = [
  { value: "asc", label: "Oldest first" },
  { value: "desc", label: "Newest first" },
];

export const pageSizeOptions: SelectOption[] = [20, 50, 100].map((size) => ({ value: String(size), label: `${size} rows` }));
