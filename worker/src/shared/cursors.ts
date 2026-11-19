import { QueryError } from "./query-error";

// Opaque list cursors: base64url of URI-escaped JSON, so any UTF-8 value
// (repository keys, timestamps, identifiers) survives the round trip.
export function encodeCursor(value: unknown): string {
  return btoa(encodeURIComponent(JSON.stringify(value)))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
}

export function decodeCursor<T>(
  value: string | undefined,
  valid: (decoded: Record<string, unknown>) => T | null,
): T | null {
  if (!value) return null;
  if (value.length > 16_384) throw new QueryError(400, "invalid cursor");
  let decoded: unknown;
  try {
    decoded = JSON.parse(decodeURIComponent(atob(value.replaceAll("-", "+").replaceAll("_", "/"))));
  } catch {
    throw new QueryError(400, "invalid cursor");
  }
  const parsed = decoded && typeof decoded === "object" && !Array.isArray(decoded)
    ? valid(decoded as Record<string, unknown>)
    : null;
  if (!parsed) throw new QueryError(400, "invalid cursor");
  return parsed;
}

export function boundedLimit(value: string | undefined, fallback: number, max: number) {
  if (value === undefined || value === "") return fallback;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1) throw new QueryError(400, "invalid limit");
  return Math.min(parsed, max);
}
