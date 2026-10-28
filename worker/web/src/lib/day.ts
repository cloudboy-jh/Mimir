import type { DayCommit, DaySession, DaySnapshot, Outcome } from "./api";

/** Workstreams that get their own slice before the rest fold into "Other". */
export const TOP_WORKSTREAMS = 5;

export type DayItem = {
  key: string;
  kind: "stream" | "session" | "other" | "quick";
  label: string;
  /** Agent time: active 15-minute blocks summed over the item's sessions. */
  minutes: number;
  app: string | null;
  model: string | null;
  repo: string | null;
  sourceRef: string | null;
  sessionIds: string[];
  requests: number;
  subAgents: number;
  commits: number;
  outcomes: Record<Outcome, number>;
  /** Finished working sessions still waiting for an outcome. */
  unresolvedIds: string[];
  active: boolean;
  captureProblems: number;
  firstAt: string | null;
  lastAt: string | null;
  open: { session: string } | { focus: string };
};

export type DayLevel = {
  focus: string | null;
  title: string | null;
  caption: string;
  items: DayItem[];
  commits: DayCommit[];
};

function itemFrom(base: Pick<DayItem, "key" | "kind" | "label" | "open">, sessions: DaySession[], bucketMinutes: number, commits: number): DayItem {
  // Each session's active blocks count separately, so a workstream slice always
  // equals the total shown when zooming into its sessions.
  const blocks = sessions.reduce((sum, session) => sum + session.activity.length, 0);
  const lead = [...sessions].sort((a, b) => b.activity.length - a.activity.length || b.requests - a.requests)[0];
  const outcomes: Record<Outcome, number> = { landed: 0, discarded: 0, abandoned: 0, unresolved: 0 };
  for (const session of sessions) outcomes[session.outcome]++;
  const repos = new Set(sessions.map((session) => session.repo));
  const refs = new Set(sessions.map((session) => session.source_ref));
  // A workstream shows its busiest session's app and model; the Other and Quick
  // buckets mix unrelated work, so they show one only when every session agrees.
  const grouped = base.kind === "other" || base.kind === "quick";
  const apps = new Set(sessions.map((session) => session.harness));
  const models = new Set(sessions.map((session) => session.model_primary));
  return {
    ...base,
    minutes: blocks * bucketMinutes,
    app: grouped && apps.size > 1 ? null : lead?.harness ?? null,
    model: grouped && models.size > 1 ? null : lead?.model_primary ?? null,
    repo: repos.size === 1 ? lead?.repo ?? null : null,
    sourceRef: repos.size === 1 && refs.size === 1 ? lead?.source_ref ?? null : null,
    sessionIds: sessions.map((session) => session.id),
    requests: sessions.reduce((sum, session) => sum + session.requests, 0),
    subAgents: sessions.reduce((sum, session) => sum + session.sub_agents, 0),
    commits,
    outcomes,
    unresolvedIds: sessions.filter((session) => !session.quiet && session.state === "inactive" && session.outcome === "unresolved").map((session) => session.id),
    active: sessions.some((session) => session.state === "active"),
    captureProblems: sessions.filter((session) => session.capture_status === "failed" || session.capture_status === "partial").length,
    firstAt: sessions.reduce<string | null>((min, session) => (!min || session.first_at < min ? session.first_at : min), null),
    lastAt: sessions.reduce<string | null>((max, session) => (!max || session.last_at > max ? session.last_at : max), null),
  };
}

const byMinutes = (a: DayItem, b: DayItem) => b.minutes - a.minutes || b.requests - a.requests;

/**
 * One zoom level of the day chart. `focus` is null for the whole day, a
 * workstream id, "other", or "quick"; unknown values fall back to the day.
 */
export function dayLevel(day: DaySnapshot, focus: string | null): DayLevel {
  const sessions = new Map(day.sessions.map((session) => [session.id, session]));
  const lookup = (ids: string[]) => ids.flatMap((id) => sessions.get(id) ?? []);
  const sessionItem = (session: DaySession) =>
    itemFrom({ key: session.id, kind: "session", label: session.display_title ?? "Untitled session", open: { session: session.id } }, [session], day.bucket_minutes, session.commit_count);
  const streams = day.workstreams.map((stream) =>
    itemFrom({ key: stream.id, kind: "stream", label: stream.label, open: stream.session_ids.length === 1 ? { session: stream.id } : { focus: stream.id } }, lookup(stream.session_ids), day.bucket_minutes, stream.commits.length),
  ).sort(byMinutes);
  // Folding a single leftover workstream into "Other" would only hide it.
  const top = streams.length > TOP_WORKSTREAMS + 1 ? streams.slice(0, TOP_WORKSTREAMS) : streams;
  const rest = streams.slice(top.length);
  const quick = lookup(day.quiet_session_ids);

  const stream = day.workstreams.find((candidate) => candidate.id === focus && candidate.session_ids.length > 1);
  if (stream) {
    const items = lookup(stream.session_ids).map(sessionItem).sort(byMinutes);
    return { focus, title: stream.label, caption: `${items.length} sessions`, items, commits: stream.commits };
  }
  if (focus === "other" && rest.length) return { focus, title: "Other workstreams", caption: `${rest.length} workstreams`, items: rest, commits: [] };
  if (focus === "quick" && quick.length) return { focus, title: "Quick sessions", caption: `${quick.length} quick sessions`, items: quick.map(sessionItem).sort(byMinutes), commits: [] };

  const items = [...top];
  if (rest.length) {
    items.push(itemFrom({ key: "other", kind: "other", label: `${rest.length} more workstreams`, open: { focus: "other" } }, lookup(rest.flatMap((item) => item.sessionIds)), day.bucket_minutes, rest.reduce((sum, item) => sum + item.commits, 0)));
  }
  if (quick.length) {
    items.push(itemFrom({ key: "quick", kind: "quick", label: `${quick.length} quick ${quick.length === 1 ? "session" : "sessions"}`, open: { focus: "quick" } }, quick, day.bucket_minutes, 0));
  }
  return { focus: null, title: null, caption: "agent time", items, commits: [] };
}

export type SliceTone = { fill: string; swatch: string };

// Each app owns one stock Tailwind hue so the same agent reads the same color
// every day. Teal (focus) and emerald/red/amber (outcomes) are never used.
// Slices sharing an app step through the hue's shades so neighbors stay apart.
const tones = (shades: [string, string][]): SliceTone[] => shades.map(([fill, swatch]) => ({ fill, swatch }));
const APP_HUES: Record<string, SliceTone[]> = {
  "oh-my-pi": tones([["fill-violet-500", "bg-violet-500"], ["fill-violet-700", "bg-violet-700"], ["fill-violet-300", "bg-violet-300"]]),
  pi: tones([["fill-indigo-500", "bg-indigo-500"], ["fill-indigo-700", "bg-indigo-700"], ["fill-indigo-300", "bg-indigo-300"]]),
  hermes: tones([["fill-sky-500", "bg-sky-500"], ["fill-sky-700", "bg-sky-700"], ["fill-sky-300", "bg-sky-300"]]),
  opencode: tones([["fill-pink-500", "bg-pink-500"], ["fill-pink-700", "bg-pink-700"], ["fill-pink-300", "bg-pink-300"]]),
  "claude-code": tones([["fill-orange-500", "bg-orange-500"], ["fill-orange-700", "bg-orange-700"], ["fill-orange-300", "bg-orange-300"]]),
  codex: tones([["fill-blue-500", "bg-blue-500"], ["fill-blue-700", "bg-blue-700"], ["fill-blue-300", "bg-blue-300"]]),
  cursor: tones([["fill-purple-500", "bg-purple-500"], ["fill-purple-700", "bg-purple-700"], ["fill-purple-300", "bg-purple-300"]]),
};
// Apps without a reserved hue hash into these, deterministically by name.
const SPARE_HUES: SliceTone[][] = [
  tones([["fill-fuchsia-500", "bg-fuchsia-500"], ["fill-fuchsia-700", "bg-fuchsia-700"], ["fill-fuchsia-300", "bg-fuchsia-300"]]),
  tones([["fill-lime-500", "bg-lime-500"], ["fill-lime-700", "bg-lime-700"], ["fill-lime-300", "bg-lime-300"]]),
  tones([["fill-yellow-500", "bg-yellow-500"], ["fill-yellow-700", "bg-yellow-700"], ["fill-yellow-300", "bg-yellow-300"]]),
];
const OTHER: SliceTone = { fill: "fill-zinc-400 dark:fill-zinc-500", swatch: "bg-zinc-400 dark:bg-zinc-500" };
const QUICK: SliceTone = { fill: "fill-stone-50 stroke-stone-400 dark:fill-stone-900 dark:stroke-stone-600", swatch: "border border-stone-400 bg-stone-50 dark:border-stone-600 dark:bg-stone-900" };

function appHue(app: string | null): SliceTone[] {
  const key = (app ?? "").toLowerCase();
  const reserved = Object.hasOwn(APP_HUES, key) ? APP_HUES[key] : undefined;
  if (reserved) return reserved;
  let hash = 0;
  for (const char of key) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  return SPARE_HUES[hash % SPARE_HUES.length]!;
}

/** Tones for one level, in item order. */
export function sliceTones(items: DayItem[]): SliceTone[] {
  const seen = new Map<string, number>();
  return items.map((item) => {
    if (item.kind === "other") return OTHER;
    if (item.kind === "quick") return QUICK;
    const hue = appHue(item.app);
    const occurrence = seen.get(hue[0]!.fill) ?? 0;
    seen.set(hue[0]!.fill, occurrence + 1);
    return hue[occurrence % hue.length]!;
  });
}

export function agentTime(minutes: number) {
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60), rest = minutes % 60;
  return rest ? `${hours}h ${rest}m` : `${hours}h`;
}

export function share(minutes: number, total: number) {
  if (!total || !minutes) return "0%";
  const percent = Math.round((minutes / total) * 100);
  return percent === 0 ? "<1%" : `${percent}%`;
}
