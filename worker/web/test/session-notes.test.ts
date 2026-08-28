import { describe, expect, it, vi } from "vitest";

import { boundedObsidianNewURL, obsidianClipboardURL, obsidianOpenURL, obsidianVaultNameError, sanitizePathSegment, sessionNoteDestination, sessionNotesFolderError, writeSessionNote, type SessionNoteDestination } from "../src/lib/session-notes";

describe("session note destinations", () => {
  it("uses the session date and a stable hash", async () => {
    const session = { id: "ses_example", repo: "mimir", started_at: "2026-08-27T23:45:12Z" };
    const first = await sessionNoteDestination(session);
    const second = await sessionNoteDestination(session);

    expect(first).toEqual(second);
    expect(first.directories).toEqual(["Mimir", "mimir"]);
    expect(first.fileName).toMatch(/^2026-08-27-[0-9a-f]{8}\.md$/);
    expect(first.relativePath).toBe(`Mimir/mimir/${first.fileName}`);
  });

  it("sanitizes project names without allowing path traversal", () => {
    expect(sanitizePathSegment("../../team\\repo:*?")).toBe("-..-team-repo---");
    expect(sanitizePathSegment("CON")).toBe("CON-project");
    expect(sanitizePathSegment("  ")).toBe("Unassigned");
  });

  it("rejects invalid configured folder names", () => {
    expect(sessionNotesFolderError("")).toBe("Enter a notes folder.");
    expect(sessionNotesFolderError("../Notes")).toBe("Use one folder name without slashes or filesystem special characters.");
    expect(sessionNotesFolderError("Session Notes")).toBeNull();
  });

  it("requires a vault name for URI handoff", () => {
    expect(obsidianVaultNameError("")).toBe("Enter the Obsidian vault name or ID.");
    expect(obsidianVaultNameError("Developer Notes")).toBeNull();
  });

  it("encodes Obsidian vault and file parameters", () => {
    expect(obsidianOpenURL("Dev Notes", "Mimir/my project/note.md")).toBe("obsidian://open?vault=Dev%20Notes&file=Mimir%2Fmy%20project%2Fnote.md");
  });

  it("keeps clipboard handoffs short", () => {
    expect(obsidianClipboardURL("Dev Notes", "Mimir/my project/note.md")).toBe("obsidian://new?vault=Dev%20Notes&file=Mimir%2Fmy%20project%2Fnote.md&clipboard");
  });

  it("keeps small Brave handoffs complete", () => {
    const handoff = boundedObsidianNewURL("Dev Notes", "Mimir/mimir/note.md", "# Session\n\nReadable evidence.");

    expect(handoff.truncated).toBe(false);
    expect(handoff.url).toContain("obsidian://new?vault=Dev%20Notes");
    expect(decodeURIComponent(handoff.url)).toContain("# Session\n\nReadable evidence.");
  });

  it("bounds large Brave handoffs while preserving the readable beginning", () => {
    const handoff = boundedObsidianNewURL("Dev Notes", "Mimir/mimir/note.md", `# Session\n\nSummary first.\n${"request evidence\n".repeat(10_000)}`);

    expect(handoff.truncated).toBe(true);
    expect(handoff.url.length).toBeLessThanOrEqual(1_900);
    expect(decodeURIComponent(handoff.url)).toContain("# Session\n\nSummary first.");
    expect(decodeURIComponent(handoff.url)).toContain("Note truncated for this browser's Obsidian handoff.");
  });
});

describe("writeSessionNote", () => {
  const destination: SessionNoteDestination = {
    directories: ["Mimir", "mimir"],
    fileName: "2026-08-27-a1b2c3d4.md",
    relativePath: "Mimir/mimir/2026-08-27-a1b2c3d4.md",
  };

  it("creates directories before writing a missing note", async () => {
    const calls: string[] = [];
    const write = vi.fn(async () => undefined);
    const close = vi.fn(async () => undefined);
    const abort = vi.fn(async () => undefined);
    const file = { kind: "file", name: destination.fileName, createWritable: async () => ({ write, close, abort }) };
    const project = {
      kind: "directory",
      name: "mimir",
      getFileHandle: async (name: string, options?: { create?: boolean }) => {
        calls.push(options?.create ? `create-file:${name}` : `find-file:${name}`);
        if (!options?.create) throw new DOMException("Missing", "NotFoundError");
        return file;
      },
    };
    const root = {
      kind: "directory",
      name: "Mimir",
      getDirectoryHandle: async (name: string, options?: { create?: boolean }) => {
        calls.push(`directory:${name}:${String(options?.create)}`);
        return project;
      },
    };
    const vault = {
      kind: "directory",
      name: "Vault",
      getDirectoryHandle: async (name: string, options?: { create?: boolean }) => {
        calls.push(`directory:${name}:${String(options?.create)}`);
        return root;
      },
    };

    const result = await writeSessionNote(vault as unknown as FileSystemDirectoryHandle, destination, "# Session");

    expect(calls).toEqual([
      "directory:Mimir:true",
      "directory:mimir:true",
      `find-file:${destination.fileName}`,
      `create-file:${destination.fileName}`,
    ]);
    expect(write).toHaveBeenCalledWith("# Session");
    expect(close).toHaveBeenCalledOnce();
    expect(result.created).toBe(true);
    expect(result.truncated).toBe(false);
  });

  it("opens an existing note without overwriting it", async () => {
    const createWritable = vi.fn();
    const file = { kind: "file", name: destination.fileName, createWritable };
    const project = { kind: "directory", name: "mimir", getFileHandle: vi.fn(async () => file) };
    const root = { kind: "directory", name: "Mimir", getDirectoryHandle: vi.fn(async () => project) };
    const vault = { kind: "directory", name: "Vault", getDirectoryHandle: vi.fn(async () => root) };

    const result = await writeSessionNote(vault as unknown as FileSystemDirectoryHandle, destination, "replacement");

    expect(createWritable).not.toHaveBeenCalled();
    expect(result.created).toBe(false);
  });

  it("removes a newly created file when writing fails", async () => {
    const failure = new Error("Disk full");
    const abort = vi.fn(async () => undefined);
    const removeEntry = vi.fn(async () => undefined);
    const file = {
      kind: "file",
      name: destination.fileName,
      createWritable: async () => ({
        write: async () => { throw failure; },
        close: vi.fn(),
        abort,
      }),
    };
    const project = {
      kind: "directory",
      name: "mimir",
      getFileHandle: async (_name: string, options?: { create?: boolean }) => {
        if (!options?.create) throw new DOMException("Missing", "NotFoundError");
        return file;
      },
      removeEntry,
    };
    const root = { kind: "directory", name: "Mimir", getDirectoryHandle: vi.fn(async () => project) };
    const vault = { kind: "directory", name: "Vault", getDirectoryHandle: vi.fn(async () => root) };

    await expect(writeSessionNote(vault as unknown as FileSystemDirectoryHandle, destination, "content")).rejects.toThrow("Disk full");
    expect(abort).toHaveBeenCalledOnce();
    expect(removeEntry).toHaveBeenCalledWith(destination.fileName);
  });
});
