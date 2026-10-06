export type DiffLine = { type: "add" | "del" | "context" | "meta"; text: string; oldLine: number | null; newLine: number | null };
export type FileDiff = {
  file: string; oldFile: string | null; newFile: string | null;
  status: "modified" | "added" | "deleted" | "renamed" | "copied";
  binary: boolean; oldMode: string | null; newMode: string | null;
  added: number; removed: number; lines: DiffLine[];
};
export type SplitDiffRow = { left: DiffLine | null; right: DiffLine | null; meta?: string };

// Git quotes unusual paths using C escapes and octal UTF-8 bytes.
function decodePath(value: string): string {
  if (!value.startsWith('"')) return value;
  const bytes: number[] = [];
  const encoder = new TextEncoder();
  const escapes: Record<string, string> = { t: "\t", n: "\n", r: "\r", b: "\b", f: "\f", v: "\v", a: "\x07" };
  const source = value.slice(1, -1);
  for (let i = 0; i < source.length;) {
    if (source[i] !== "\\") {
      const point = String.fromCodePoint(source.codePointAt(i)!);
      bytes.push(...encoder.encode(point)); i += point.length; continue;
    }
    i++;
    const octal = /^[0-7]{1,3}/.exec(source.slice(i));
    if (octal) { bytes.push(parseInt(octal[0], 8)); i += octal[0].length; }
    else { bytes.push(...encoder.encode(escapes[source[i]!] ?? source[i] ?? "\\")); i++; }
  }
  return new TextDecoder().decode(new Uint8Array(bytes));
}
function path(value: string, prefix = false): string | null {
  const decoded = decodePath(value.replace(/\t.*$/, ""));
  if (decoded === "/dev/null") return null;
  return prefix ? decoded.replace(/^[ab]\//, "") : decoded;
}
function fileDiff(oldFile: string | null, newFile: string | null): FileDiff {
  return { file: newFile ?? oldFile ?? "Unknown file", oldFile, newFile, status: "modified", binary: false, oldMode: null, newMode: null, added: 0, removed: 0, lines: [] };
}

export function parsePatch(patch: string): FileDiff[] {
  const files: FileDiff[] = [];
  let current: FileDiff | null = null;
  let oldLine = 0, newLine = 0, inHunk = false;
  let oldRemaining = 0, newRemaining = 0;
  const lines = patch.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  for (const raw of lines) {
    const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
    if (line.startsWith("diff --git ")) {
      const paths = /^diff --git ("(?:[^"\\]|\\.)*"|a\/.*?) ("(?:[^"\\]|\\.)*"|b\/.*)$/.exec(line);
      current = fileDiff(paths ? path(paths[1]!, true) : null, paths ? path(paths[2]!, true) : null);
      files.push(current); inHunk = false; continue;
    }
    if (!inHunk && line.startsWith("--- ")) {
      if (!current || current.lines.some((item) => item.type !== "meta")) { current = fileDiff(null, null); files.push(current); }
      current.oldFile = path(line.slice(4), true); continue;
    }
    if (!current) continue;
    if (!inHunk && line.startsWith("+++ ")) {
      current.newFile = path(line.slice(4), true);
      current.file = current.newFile ?? current.oldFile ?? current.file;
      if (!current.newFile) current.status = "deleted";
      else if (!current.oldFile) current.status = "added";
      continue;
    }
    const hunk = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/.exec(line);
    if (hunk) {
      oldLine = Number(hunk[1]); newLine = Number(hunk[3]);
      oldRemaining = hunk[2] === undefined ? 1 : Number(hunk[2]);
      newRemaining = hunk[4] === undefined ? 1 : Number(hunk[4]);
      inHunk = oldRemaining > 0 || newRemaining > 0;
    }
    if (inHunk && !hunk) {
      if (line.startsWith("+")) { current.added++; current.lines.push({ type: "add", text: line, oldLine: null, newLine: newLine++ }); newRemaining--; inHunk = oldRemaining > 0 || newRemaining > 0; continue; }
      if (line.startsWith("-")) { current.removed++; current.lines.push({ type: "del", text: line, oldLine: oldLine++, newLine: null }); oldRemaining--; inHunk = oldRemaining > 0 || newRemaining > 0; continue; }
      if (line.startsWith(" ")) { current.lines.push({ type: "context", text: line, oldLine: oldLine++, newLine: newLine++ }); oldRemaining--; newRemaining--; inHunk = oldRemaining > 0 || newRemaining > 0; continue; }
    }
    if (/^(?:Binary files |GIT binary patch)/.test(line)) { current.binary = true; inHunk = false; }
    if (line.startsWith("rename from ") || line.startsWith("copy from ")) { current.oldFile = path(line.slice(line.indexOf("from ") + 5)); current.status = line.startsWith("rename") ? "renamed" : "copied"; }
    if (line.startsWith("rename to ") || line.startsWith("copy to ")) { current.newFile = path(line.slice(line.indexOf("to ") + 3)); current.file = current.newFile ?? current.file; }
    if (line.startsWith("new file mode ")) { current.status = "added"; current.oldFile = null; current.newMode = line.slice(14); }
    if (line.startsWith("deleted file mode ")) { current.status = "deleted"; current.newFile = null; current.oldMode = line.slice(18); }
    if (line.startsWith("old mode ")) current.oldMode = line.slice(9);
    if (line.startsWith("new mode ")) current.newMode = line.slice(9);
    // Binary payload remains in the exact raw patch; it is not a source hunk.
    if (!current.binary || line.startsWith("Binary files ") || line === "GIT binary patch") current.lines.push({ type: "meta", text: line, oldLine: null, newLine: null });
  }
  return files;
}

export function splitDiffLines(lines: DiffLine[]): SplitDiffRow[] {
  const rows: SplitDiffRow[] = [];
  for (let i = 0; i < lines.length;) {
    const line = lines[i]!;
    if (line.type === "meta") { rows.push({ left: null, right: null, meta: line.text }); i++; continue; }
    if (line.type === "context") { rows.push({ left: line, right: line }); i++; continue; }
    const removed: DiffLine[] = [], added: DiffLine[] = [];
    while (i < lines.length && (lines[i]!.type === "del" || lines[i]!.type === "add")) {
      const change = lines[i++]!;
      (change.type === "del" ? removed : added).push(change);
    }
    for (let index = 0; index < Math.max(removed.length, added.length); index++) rows.push({ left: removed[index] ?? null, right: added[index] ?? null });
  }
  return rows;
}
