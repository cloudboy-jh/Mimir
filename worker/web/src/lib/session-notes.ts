import type { Session } from "@/lib/api";

export const DEFAULT_SESSION_NOTES_FOLDER = "Mimir";

const DATABASE_NAME = "mimir-session-notes";
const DATABASE_VERSION = 1;
const PREFERENCES_STORE = "preferences";
const SETTINGS_KEY = "session-notes";
const INVALID_SEGMENT_CHARACTERS = /[<>:"/\\|?*\u0000-\u001f]/g;
const WINDOWS_RESERVED_NAME = /^(?:con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\..*)?$/i;

type WritableDirectoryHandle = FileSystemDirectoryHandle & {
  queryPermission(options: { mode: "readwrite" }): Promise<PermissionState>;
  requestPermission(options: { mode: "readwrite" }): Promise<PermissionState>;
};

type DirectoryPickerWindow = Window & {
  showDirectoryPicker(options?: { mode?: "read" | "readwrite" }): Promise<FileSystemDirectoryHandle>;
};

export type SessionNoteSettings = {
  vault: FileSystemDirectoryHandle;
  folder: string;
};

export type SessionNoteDestination = {
  directories: string[];
  fileName: string;
  relativePath: string;
};

export type SessionNoteWriteResult = SessionNoteDestination & {
  created: boolean;
};

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DATABASE_NAME, DATABASE_VERSION);
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(PREFERENCES_STORE)) request.result.createObjectStore(PREFERENCES_STORE);
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("Session note settings could not be opened."));
  });
}

async function readPreference<T>(key: string): Promise<T | null> {
  const database = await openDatabase();
  try {
    return await new Promise<T | null>((resolve, reject) => {
      const request = database.transaction(PREFERENCES_STORE, "readonly").objectStore(PREFERENCES_STORE).get(key);
      request.onsuccess = () => resolve((request.result as T | undefined) ?? null);
      request.onerror = () => reject(request.error ?? new Error("Session note settings could not be read."));
    });
  } finally {
    database.close();
  }
}

async function writePreference<T>(key: string, value: T | null): Promise<void> {
  const database = await openDatabase();
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(PREFERENCES_STORE, "readwrite");
      if (value === null) transaction.objectStore(PREFERENCES_STORE).delete(key);
      else transaction.objectStore(PREFERENCES_STORE).put(value, key);
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(transaction.error ?? new Error("Session note settings could not be saved."));
      transaction.onabort = () => reject(transaction.error ?? new Error("Session note settings could not be saved."));
    });
  } finally {
    database.close();
  }
}

export function supportsObsidianVaults(): boolean {
  return typeof window !== "undefined"
    && typeof indexedDB !== "undefined"
    && window.isSecureContext
    && "showDirectoryPicker" in window;
}

export function sanitizePathSegment(value: string, fallback = "Unassigned"): string {
  let segment = value
    .normalize("NFC")
    .replace(INVALID_SEGMENT_CHARACTERS, "-")
    .replace(/\s+/g, " ")
    .replace(/^[. ]+|[. ]+$/g, "")
    .slice(0, 80)
    .replace(/[. ]+$/g, "");
  if (!segment || segment === "." || segment === "..") segment = fallback;
  if (WINDOWS_RESERVED_NAME.test(segment)) segment = `${segment}-project`;
  return segment;
}

export function sessionNotesFolderError(value: string): string | null {
  const folder = value.trim();
  if (!folder) return "Enter a notes folder.";
  if (folder !== sanitizePathSegment(folder, "")) return "Use one folder name without slashes or filesystem special characters.";
  return null;
}

async function sessionHash(id: string): Promise<string> {
  const bytes = new TextEncoder().encode(id);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest).slice(0, 4), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function sessionDate(startedAt: string): string {
  const date = new Date(startedAt);
  return Number.isNaN(date.getTime()) ? "unknown-date" : date.toISOString().slice(0, 10);
}

export async function sessionNoteDestination(
  session: Pick<Session, "id" | "repo" | "started_at">,
  folder = DEFAULT_SESSION_NOTES_FOLDER,
): Promise<SessionNoteDestination> {
  const root = sanitizePathSegment(folder, DEFAULT_SESSION_NOTES_FOLDER);
  const project = sanitizePathSegment(session.repo ?? "", "Unassigned");
  const fileName = `${sessionDate(session.started_at)}-${await sessionHash(session.id)}.md`;
  const directories = [root, project];
  return { directories, fileName, relativePath: [...directories, fileName].join("/") };
}

export async function loadSessionNoteSettings(): Promise<SessionNoteSettings | null> {
  if (!supportsObsidianVaults()) return null;
  const settings = await readPreference<SessionNoteSettings>(SETTINGS_KEY);
  if (!settings?.vault || settings.vault.kind !== "directory") return null;
  return { vault: settings.vault, folder: sanitizePathSegment(settings.folder, DEFAULT_SESSION_NOTES_FOLDER) };
}

export async function connectObsidianVault(folder = DEFAULT_SESSION_NOTES_FOLDER): Promise<SessionNoteSettings> {
  if (!supportsObsidianVaults()) throw new Error("Direct Obsidian vault access requires a Chromium browser over HTTPS.");
  const vault = await (window as unknown as DirectoryPickerWindow).showDirectoryPicker({ mode: "readwrite" });
  const settings = { vault, folder: sanitizePathSegment(folder, DEFAULT_SESSION_NOTES_FOLDER) };
  await writePreference(SETTINGS_KEY, settings);
  return settings;
}

export async function saveSessionNotesFolder(folder: string): Promise<SessionNoteSettings> {
  const error = sessionNotesFolderError(folder);
  if (error) throw new Error(error);
  const settings = await loadSessionNoteSettings();
  if (!settings) throw new Error("Connect an Obsidian vault before saving the notes folder.");
  const next = { ...settings, folder: folder.trim() };
  await writePreference(SETTINGS_KEY, next);
  return next;
}

export async function disconnectObsidianVault(): Promise<void> {
  if (typeof indexedDB === "undefined") return;
  await writePreference(SETTINGS_KEY, null);
}

export async function ensureVaultWritePermission(vault: FileSystemDirectoryHandle): Promise<void> {
  const handle = vault as WritableDirectoryHandle;
  if (typeof handle.queryPermission !== "function" || typeof handle.requestPermission !== "function") {
    throw new Error("This browser cannot restore write access to the connected vault.");
  }
  let permission = await handle.queryPermission({ mode: "readwrite" });
  if (permission === "prompt") permission = await handle.requestPermission({ mode: "readwrite" });
  if (permission !== "granted") throw new Error("Allow write access to the connected Obsidian vault to create this note.");
}

export async function writeSessionNote(
  vault: FileSystemDirectoryHandle,
  destination: SessionNoteDestination,
  markdown: string,
): Promise<SessionNoteWriteResult> {
  let directory = vault;
  for (const name of destination.directories) directory = await directory.getDirectoryHandle(name, { create: true });

  let file: FileSystemFileHandle;
  let created = false;
  try {
    file = await directory.getFileHandle(destination.fileName);
  } catch (cause) {
    if (!(cause instanceof DOMException) || cause.name !== "NotFoundError") throw cause;
    file = await directory.getFileHandle(destination.fileName, { create: true });
    created = true;
  }

  if (created) {
    const writable = await file.createWritable();
    try {
      await writable.write(markdown);
      await writable.close();
    } catch (cause) {
      await writable.abort(cause).catch(() => undefined);
      await directory.removeEntry(destination.fileName).catch(() => undefined);
      throw cause;
    }
  }

  return { ...destination, created };
}

export function obsidianOpenURL(vault: string, relativePath: string): string {
  return `obsidian://open?vault=${encodeURIComponent(vault)}&file=${encodeURIComponent(relativePath)}`;
}

export async function prepareObsidianVault(): Promise<SessionNoteSettings> {
  const settings = await loadSessionNoteSettings();
  if (!settings) throw new Error("Connect an Obsidian vault in Settings before opening session notes.");
  await ensureVaultWritePermission(settings.vault);
  return settings;
}

export async function writeAndOpenSessionNote(
  session: Pick<Session, "id" | "repo" | "started_at">,
  markdown: string,
  settings?: SessionNoteSettings,
): Promise<SessionNoteWriteResult> {
  const activeSettings = settings ?? await prepareObsidianVault();
  const destination = await sessionNoteDestination(session, activeSettings.folder);
  const result = await writeSessionNote(activeSettings.vault, destination, markdown);
  window.location.assign(obsidianOpenURL(activeSettings.vault.name, result.relativePath));
  return result;
}
