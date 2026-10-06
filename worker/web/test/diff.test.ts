import { describe, expect, it } from "vitest";
import { parsePatch, splitDiffLines } from "../src/lib/diff";
import { highlightDiffLine } from "../src/lib/diff-highlight";

describe("captured patch reader", () => {
  it("keeps deleted filenames and hunk numbers without creating a trailing context line", () => {
    const [file] = parsePatch('diff --git a/removed.ts b/removed.ts\ndeleted file mode 100644\n--- a/removed.ts\n+++ /dev/null\n@@ -3,2 +0,0 @@\n-one\n-two\n');
    expect(file).toMatchObject({ file: "removed.ts", oldFile: "removed.ts", newFile: null, status: "deleted", added: 0, removed: 2 });
    expect(file!.lines.filter((line) => line.type === "del").map((line) => line.oldLine)).toEqual([3, 4]);
    expect(file!.lines.filter((line) => line.type === "context")).toEqual([]);
  });
  it("decodes quoted Git octal UTF-8 paths and preserves rename metadata", () => {
    const [file] = parsePatch('diff --git "a/old name.ts" "b/caf\\303\\251.ts"\nsimilarity index 100%\nrename from old name.ts\nrename to "caf\\303\\251.ts"\n');
    expect(file).toMatchObject({ file: "café.ts", oldFile: "old name.ts", newFile: "café.ts", status: "renamed", added: 0, removed: 0 });
  });
  it("distinguishes binary and mode-only captures without treating binary payload as source", () => {
    const files = parsePatch('diff --git a/logo.png b/logo.png\nGIT binary patch\nliteral 4\nAbCd\n\ndiff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n');
    expect(files[0]).toMatchObject({ file: "logo.png", binary: true, added: 0, removed: 0 });
    expect(files[0]!.lines.some((line) => line.type === "context" || line.text === "AbCd")).toBe(false);
    expect(files[1]).toMatchObject({ file: "run.sh", oldMode: "100644", newMode: "100755", binary: false, added: 0, removed: 0 });
  });
  it("does not interpret header-looking source lines as filenames inside a hunk", () => {
    const [file] = parsePatch('diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n--- old\n+++ new\n');
    expect(file).toMatchObject({ file: "a.txt", added: 1, removed: 1 });
    expect(file!.lines.filter((line) => line.type !== "meta").map((line) => line.text)).toEqual(["--- old", "+++ new"]);
  });
  it("separates plain unified files at completed hunk boundaries", () => {
    const files = parsePatch('--- a/first.txt\n+++ b/first.txt\n@@ -1 +1 @@\n-old\n+new\n--- /dev/null\n+++ b/second.txt\n@@ -0,0 +4,2 @@\n+one\n+two\n');
    expect(files.map((file) => [file.file, file.status, file.added, file.removed])).toEqual([["first.txt", "modified", 1, 1], ["second.txt", "added", 2, 0]]);
    expect(files[1]!.lines.filter((line) => line.type === "add").map((line) => line.newLine)).toEqual([4, 5]);
  });
  it("aligns unequal replacement runs while preserving hunk and no-newline evidence", () => {
    const [file] = parsePatch('diff --git a/a.ts b/a.ts\n@@ -2,2 +2,3 @@\n-old\n+new\n+extra\n same\n\\ No newline at end of file\n');
    const rows = splitDiffLines(file!.lines);
    expect(rows.map((row) => [row.left?.oldLine ?? null, row.right?.newLine ?? null, row.meta ?? null])).toEqual([[null, null, "@@ -2,2 +2,3 @@"], [2, 2, null], [null, 3, null], [3, 4, null], [null, null, "\\ No newline at end of file"]]);
  });
  it("escapes HTML in highlighted source and unknown-language metadata", () => {
    const line = { type: "add" as const, text: '+const x = "<img src=x onerror=alert(1)>";', oldLine: null, newLine: 1 };
    const html = highlightDiffLine("source.ts", line);
    expect(html).not.toContain("<img");
    expect(html).toContain("&lt;img");
    expect(highlightDiffLine("unknown.file", { ...line, type: "meta", text: "<script>evil</script>" })).toBe("&lt;script&gt;evil&lt;/script&gt;");
  });
});
