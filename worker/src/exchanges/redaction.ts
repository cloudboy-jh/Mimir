import { parseJSON } from "../config/config-store";
export function redact(value: unknown, patterns: string[]): unknown {
  let text = JSON.stringify(value)
    .replace(/(?:sk|pk|rk)_[A-Za-z0-9_-]{16,}/g, "[REDACTED]")
    .replace(/(?:Bearer\s+)[A-Za-z0-9._-]+/gi, "$1[REDACTED]")
    .replace(
      /((?:api[_-]?key|token|secret|password)["']?\s*[:=]\s*["']?)[^\s,"'}]+/gi,
      "$1[REDACTED]",
    );
  for (const pattern of patterns) {
    if (pattern === "builtin") continue;
    try {
      text = text.replace(new RegExp(pattern, "g"), "[REDACTED]");
    } catch {
      // Invalid patterns are inert rather than blocking the proxy.
    }
  }
  return parseJSON(text);
}
