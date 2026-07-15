export function compactNumber(value: number) {
  return new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 }).format(value);
}

export function shortDate(value: string) {
  return new Intl.DateTimeFormat("en", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }).format(new Date(value));
}

export function relativeDate(value: string) {
  const minutes = Math.round((Date.now() - new Date(value).getTime()) / 60_000);
  if (minutes < 60) return `${Math.max(minutes, 1)}m ago`;
  if (minutes < 1440) return `${Math.round(minutes / 60)}h ago`;
  return `${Math.round(minutes / 1440)}d ago`;
}

export function duration(start: string, end: string | null) {
  if (!end) return "In progress";
  const minutes = Math.max(1, Math.round((new Date(end).getTime() - new Date(start).getTime()) / 60_000));
  return `${minutes}m`;
}

export function outputSpeed(tokens: number, latency: number) {
  return tokens && latency ? `${Math.round(tokens / latency * 1000)} tok/s` : "-";
}
