import { QueryError } from "./query-error";

export type DateRange = { start: string | null; end: string | null };

const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/i;

function validCalendarDate(value: string) {
  const parsed = new Date(`${value}T00:00:00.000Z`);
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
}

// Returns an instant; `end` bounds are exclusive. A `YYYY-MM-DD` end covers
// that whole UTC day, and an RFC3339 end includes its own millisecond.
function bound(value: string, end: boolean, now: number): string | null {
  const relative = /^(\d{1,5})([hd])$/.exec(value);
  if (relative) {
    const hours = Number(relative[1]) * (relative[2] === "d" ? 24 : 1);
    return new Date(now - hours * 3_600_000).toISOString();
  }
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) {
    if (!validCalendarDate(value)) return null;
    const parsed = new Date(`${value}T00:00:00.000Z`);
    if (end) parsed.setUTCDate(parsed.getUTCDate() + 1);
    return parsed.toISOString();
  }
  if (!RFC3339.test(value) || !validCalendarDate(value.slice(0, 10))) return null;
  if (Number(value.slice(11, 13)) > 23 || Number(value.slice(14, 16)) > 59 || Number(value.slice(17, 19)) > 59) return null;
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) return null;
  return new Date(end ? parsed + 1 : parsed).toISOString();
}

// dateRange parses `from`/`to` query values: RFC3339, `YYYY-MM-DD` (UTC day,
// `to` inclusive) or relative `Nh`/`Nd` (now minus N).
export function dateRange(from: string | undefined, to: string | undefined, now = Date.now()): DateRange {
  const start = from ? bound(from, false, now) : null;
  const end = to ? bound(to, true, now) : null;
  if (from && !start) throw new QueryError(400, "invalid from");
  if (to && !end) throw new QueryError(400, "invalid to");
  if (start && end && start >= end) throw new QueryError(400, "invalid date range");
  return { start, end };
}
