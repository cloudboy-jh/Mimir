export type SaveConfig = {
  enabled: boolean;
  excludeRepos: string[];
  excludeModels: string[];
  gapMinutes: number;
};

export type CaptureDecision =
  | { capture: "scheduled"; reason: "enabled" }
  | { capture: "skipped"; reason: "disabled" | "excluded_repository" | "excluded_model" | "missing_response_body" };

export async function readConfig(db: D1Database) {
  const result = await db.prepare("SELECT key, value FROM config").all<{ key: string; value: string }>();
  const config: Record<string, unknown> = {
    "save.enabled": true,
    "save.exclude_repos": [],
    "save.exclude_models": [],
    "redact.patterns": ["builtin"],
    "session.gap_minutes": 15,
    "session.abandon_days": 7,
  };
  for (const row of result.results) config[row.key] = parseJSON(row.value);
  return config;
}

export async function readSaveConfig(db: D1Database): Promise<SaveConfig> {
  const config = await readConfig(db);
  return {
    enabled: config["save.enabled"] !== false,
    excludeRepos: stringArray(config["save.exclude_repos"]),
    excludeModels: stringArray(config["save.exclude_models"]),
    gapMinutes: finiteNumber(config["session.gap_minutes"], 15),
  };
}

export function shouldSave(config: SaveConfig, repo: string | null, model: string) {
  return config.enabled && !config.excludeRepos.some((value) => matches(value, repo ?? "")) && !config.excludeModels.some((value) => matches(value, model));
}

export function decideCapture(config: SaveConfig, repo: string | null, model: string, hasResponseBody: boolean): CaptureDecision {
  if (!config.enabled) return { capture: "skipped", reason: "disabled" };
  if (config.excludeRepos.some((value) => matches(value, repo ?? ""))) return { capture: "skipped", reason: "excluded_repository" };
  if (config.excludeModels.some((value) => matches(value, model))) return { capture: "skipped", reason: "excluded_model" };
  if (!hasResponseBody) return { capture: "skipped", reason: "missing_response_body" };
  return { capture: "scheduled", reason: "enabled" };
}

export function validateConfigValues(values: Record<string, unknown>) {
  const allowed = new Set(["save.enabled", "save.exclude_repos", "save.exclude_models", "redact.patterns", "session.gap_minutes", "session.abandon_days"]);
  for (const [key, value] of Object.entries(values)) {
    if (!allowed.has(key)) return `unknown config key: ${key}`;
    if (key === "save.enabled" && typeof value !== "boolean") return `${key} must be boolean`;
    if (["save.exclude_repos", "save.exclude_models", "redact.patterns"].includes(key)) {
      if (!Array.isArray(value) || value.length > 100 || value.some((item) => typeof item !== "string" || item.length > 256)) return `${key} must be an array of strings up to 256 characters`;
    }
    if (key === "session.gap_minutes" && (typeof value !== "number" || !Number.isInteger(value) || value < 1 || value > 1440)) return `${key} must be an integer from 1 to 1440`;
    if (key === "session.abandon_days" && (typeof value !== "number" || !Number.isInteger(value) || value < 1 || value > 365)) return `${key} must be an integer from 1 to 365`;
  }
  return "";
}

export function stringArray(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

export function finiteNumber(value: unknown, fallback: number) {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

export function parseJSON(value: string) {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return value;
  }
}

function matches(pattern: string, value: string) {
  return new RegExp(`^${pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&").replaceAll("*", ".*")}$`).test(value);
}
