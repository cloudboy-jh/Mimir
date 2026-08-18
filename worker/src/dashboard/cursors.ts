export function encodeCursor(ts: string, id: string) {
  return btoa(JSON.stringify({ ts, id }))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
}

export function encodeExchangeCursor(
  ts: string,
  id: string,
  order: "asc" | "desc",
) {
  return btoa(JSON.stringify({ ts, id, order }))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
}

export function decodeCursor(value: string | undefined) {
  if (!value) return null;
  try {
    const padded =
      value.replaceAll("-", "+").replaceAll("_", "/") +
      "===".slice((value.length + 3) % 4);
    const cursor = JSON.parse(atob(padded)) as { ts?: unknown; id?: unknown };
    return typeof cursor.ts === "string" &&
      cursor.ts.length > 0 &&
      typeof cursor.id === "string" &&
      cursor.id.length > 0
      ? { ts: cursor.ts, id: cursor.id }
      : null;
  } catch {
    return null;
  }
}

export function decodeExchangeCursor(value: string | undefined) {
  if (!value) return null;
  try {
    const padded =
      value.replaceAll("-", "+").replaceAll("_", "/") +
      "===".slice((value.length + 3) % 4);
    const cursor = JSON.parse(atob(padded)) as {
      ts?: unknown;
      id?: unknown;
      order?: unknown;
    };
    return typeof cursor.ts === "string" &&
      cursor.ts.length > 0 &&
      typeof cursor.id === "string" &&
      cursor.id.length > 0 &&
      (cursor.order === "asc" || cursor.order === "desc")
      ? { ts: cursor.ts, id: cursor.id, order: cursor.order }
      : null;
  } catch {
    return null;
  }
}

export function boundedLimit(value: string | undefined) {
  const parsed = value === undefined ? 25 : Number(value);
  return Math.max(
    1,
    Math.min(Number.isFinite(parsed) ? Math.trunc(parsed) : 25, 100),
  );
}
