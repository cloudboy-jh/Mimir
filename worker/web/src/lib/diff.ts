export type DiffLine = { type: "add" | "del" | "context" | "meta"; text: string; oldLine: number | null; newLine: number | null };
export type FileDiff = { file: string; added: number; removed: number; lines: DiffLine[] };

// parsePatch parses a unified diff into per-file blocks with line stats. It
// tolerates missing headers and truncates nothing; the caller bounds size.
export function parsePatch(patch: string): FileDiff[] {
  const files: FileDiff[] = [];
  let current: FileDiff | null = null;
  let oldLine = 0;
  let newLine = 0;
  for (const line of patch.split("\n")) {
    if (line.startsWith("diff --git ")) {
      const paths = line.match(/^diff --git a\/(.+) b\/(.+)$/);
      current = { file: paths?.[2] ?? "", added: 0, removed: 0, lines: [] };
      files.push(current);
      continue;
    }
    if (!current) continue;
    if (line.startsWith("+++ ")) {
      const path = line.slice(4).trim();
      current.file = path.startsWith("b/") ? path.slice(2) : path;
      continue;
    }
    if (line.startsWith("--- ") || line.startsWith("index ") || line.startsWith("new file") || line.startsWith("deleted file") || line.startsWith("similarity") || line.startsWith("rename")) continue;
    if (line.startsWith("@@")) {
      const hunk = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
      if (hunk) { oldLine = Number(hunk[1]); newLine = Number(hunk[2]); }
      current.lines.push({ type: "meta", text: line, oldLine: null, newLine: null });
      continue;
    }
    if (line.startsWith("+")) {
      current.added += 1;
      current.lines.push({ type: "add", text: line, oldLine: null, newLine: newLine++ });
      continue;
    }
    if (line.startsWith("-")) {
      current.removed += 1;
      current.lines.push({ type: "del", text: line, oldLine: oldLine++, newLine: null });
      continue;
    }
    if (line.startsWith("\\")) {
      current.lines.push({ type: "meta", text: line, oldLine: null, newLine: null });
      continue;
    }
    current.lines.push({ type: "context", text: line, oldLine: oldLine++, newLine: newLine++ });
  }
  return files.filter((file) => file.file);
}
