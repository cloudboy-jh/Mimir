import { highlightCode, languageForPath } from "./code-highlight";
import type { DiffLine } from "./diff";

export function highlightDiffLine(file: string, line: DiffLine): string {
  if (line.type === "meta") return highlightCode(line.text);
  const source = line.text.slice(1);
  // Generated lines stay plain to keep interaction bounded.
  return highlightCode(source, source.length <= 4_000 ? languageForPath(file) : undefined);
}
