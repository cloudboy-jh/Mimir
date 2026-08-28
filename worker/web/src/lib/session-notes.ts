import type { Session } from "@/lib/api";

export const DEFAULT_SESSION_NOTES_FOLDER = "Mimir";
export const OBSIDIAN_URI_EXCHANGE_LIMIT = 25;

const MAX_OBSIDIAN_URI_LENGTH = 1_900;
const URI_TRUNCATION_NOTICE = "\n\n_Note truncated for this browser's Obsidian handoff. Use Download Markdown in Mimir for complete request evidence._";

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
  vault: FileSystemDirectoryHandle | null;
  vaultName: string;
  folder: string;
};

export type SessionNoteDestination = {
  directories: string[];
  fileName: string;
  relativePath: string;
};

export type SessionNoteWriteResult = SessionNoteDestination & {
  created: boolean | null;
  truncated: boolean;
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
    && typeof (window as unknown as Partial<DirectoryPickerWindow>).showDirectoryPicker === "function";
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

export function obsidianVaultNameError(value: string): string | null {
  if (!value.trim()) return "Enter the Obsidian vault name or ID.";
  if (value.trim().length > 120) return "Keep the Obsidian vault identifier under 120 characters.";
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
  if (typeof indexedDB === "undefined") return null;
  const settings = await readPreference<Partial<SessionNoteSettings>>(SETTINGS_KEY);
  if (!settings) return null;
  const vault = settings.vault?.kind === "directory" ? settings.vault : null;
  const vaultName = settings.vaultName?.trim() || vault?.name || "";
  if (!vaultName) return null;
  return { vault, vaultName, folder: sanitizePathSegment(settings.folder ?? "", DEFAULT_SESSION_NOTES_FOLDER) };
}

export async function connectObsidianVault(folder = DEFAULT_SESSION_NOTES_FOLDER): Promise<SessionNoteSettings> {
  if (!supportsObsidianVaults()) throw new Error("Direct Obsidian vault access requires Chrome or Edge over HTTPS.");
  const vault = await (window as unknown as DirectoryPickerWindow).showDirectoryPicker({ mode: "readwrite" });
  const settings = { vault, vaultName: vault.name, folder: sanitizePathSegment(folder, DEFAULT_SESSION_NOTES_FOLDER) };
  await writePreference(SETTINGS_KEY, settings);
  return settings;
}

export async function saveObsidianURISettings(vaultName: string, folder: string): Promise<SessionNoteSettings> {
  const vaultError = obsidianVaultNameError(vaultName);
  if (vaultError) throw new Error(vaultError);
  const folderError = sessionNotesFolderError(folder);
  if (folderError) throw new Error(folderError);
  const settings = { vault: null, vaultName: vaultName.trim(), folder: folder.trim() };
  await writePreference(SETTINGS_KEY, settings);
  return settings;
}

export async function saveSessionNotesFolder(folder: string): Promise<SessionNoteSettings> {
  const error = sessionNotesFolderError(folder);
  if (error) throw new Error(error);
  const settings = await loadSessionNoteSettings();
  if (!settings) throw new Error("Configure an Obsidian vault before saving the notes folder.");
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

  return { ...destination, created, truncated: false };
}

export function obsidianOpenURL(vault: string, relativePath: string): string {
  return `obsidian://open?vault=${encodeURIComponent(vault)}&file=${encodeURIComponent(relativePath)}`;
}

function obsidianNewURL(vault: string, relativePath: string, content: string): string {
  return `obsidian://new?vault=${encodeURIComponent(vault)}&file=${encodeURIComponent(relativePath)}&content=${encodeURIComponent(content)}`;
}

export function obsidianClipboardURL(vault: string, relativePath: string): string {
  return `obsidian://new?vault=${encodeURIComponent(vault)}&file=${encodeURIComponent(relativePath)}&clipboard`;
}

export function boundedObsidianNewURL(vault: string, relativePath: string, markdown: string): { url: string; truncated: boolean } {
  const complete = obsidianNewURL(vault, relativePath, markdown);
  if (complete.length <= MAX_OBSIDIAN_URI_LENGTH) return { url: complete, truncated: false };

  let low = 0;
  let high = markdown.length;
  while (low < high) {
    const midpoint = Math.ceil((low + high) / 2);
    if (obsidianNewURL(vault, relativePath, markdown.slice(0, midpoint) + URI_TRUNCATION_NOTICE).length <= MAX_OBSIDIAN_URI_LENGTH) low = midpoint;
    else high = midpoint - 1;
  }
  let prefix = markdown.slice(0, low);
  const lineBoundary = prefix.lastIndexOf("\n");
  if (lineBoundary > 0) prefix = prefix.slice(0, lineBoundary);
  return { url: obsidianNewURL(vault, relativePath, prefix + URI_TRUNCATION_NOTICE), truncated: true };
}

export async function prepareObsidianVault(): Promise<SessionNoteSettings> {
  const settings = await loadSessionNoteSettings();
  if (!settings) throw new Error("Configure an Obsidian vault in Settings before opening session notes.");
  if (settings.vault) await ensureVaultWritePermission(settings.vault);
  return settings;
}

export async function writeAndOpenSessionNote(
  session: Pick<Session, "id" | "repo" | "started_at">,
  markdown: string,
  settings?: SessionNoteSettings,
): Promise<SessionNoteWriteResult> {
  const activeSettings = settings ?? await prepareObsidianVault();
  const destination = await sessionNoteDestination(session, activeSettings.folder);
  if (activeSettings.vault) {
    const result = await writeSessionNote(activeSettings.vault, destination, markdown);
    window.location.assign(obsidianOpenURL(activeSettings.vaultName, result.relativePath));
    return result;
  }
  if (typeof navigator.clipboard?.writeText === "function") {
    try {
      await navigator.clipboard.writeText(markdown);
      window.location.assign(obsidianClipboardURL(activeSettings.vaultName, destination.relativePath));
      return { ...destination, created: null, truncated: false };
    } catch {
      // Fall through to a URI-contained excerpt when clipboard permission is unavailable.
    }
  }
  const handoff = boundedObsidianNewURL(activeSettings.vaultName, destination.relativePath, markdown);
  window.location.assign(handoff.url);
  return { ...destination, created: null, truncated: handoff.truncated };
}
