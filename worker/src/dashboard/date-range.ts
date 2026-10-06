type DateBound = { value: string; exclusive: boolean };

function dateBound(value: string, end: boolean): DateBound | null {
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    const parsed = new Date(`${value}T00:00:00.000Z`);
    if (!Number.isFinite(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== value) return null;
    if (end) parsed.setUTCDate(parsed.getUTCDate() + 1);
    return { value: parsed.toISOString(), exclusive: end };
  }
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,3})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) return null;
  if (Number(value.slice(11, 13)) > 23 || Number(value.slice(14, 16)) > 59 || Number(value.slice(17, 19)) > 59) return null;
  const parsed = new Date(value);
  const calendar = new Date(`${value.slice(0, 10)}T00:00:00.000Z`);
  if (!Number.isFinite(parsed.getTime()) || !Number.isFinite(calendar.getTime()) || calendar.toISOString().slice(0, 10) !== value.slice(0, 10)) return null;
  return { value: parsed.toISOString(), exclusive: false };
}

export function dashboardDateRange(from?: string, to?: string) {
  const start = from ? dateBound(from, false) : undefined;
  const end = to ? dateBound(to, true) : undefined;
  if (from && !start) return { error: "invalid from" } as const;
  if (to && !end) return { error: "invalid to" } as const;
  if (start && end && (start.value > end.value || (end.exclusive && start.value === end.value))) return { error: "invalid date range" } as const;
  return { start, end } as const;
}
