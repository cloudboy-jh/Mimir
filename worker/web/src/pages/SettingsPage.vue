<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef } from "vue";
import { Check, FolderOpen, Pencil, RotateCw, ShieldOff, Unplug, X } from "lucide-vue-next";
import DeviceIdentity from "@/components/DeviceIdentity.vue";
import Button from "@/components/ui/Button.vue";
import { errorMessage, listDevices, renameDevice, revokeDevice, type Device } from "@/lib/api";
import { connectObsidianVault, DEFAULT_SESSION_NOTES_FOLDER, disconnectObsidianVault, loadSessionNoteSettings, saveSessionNotesFolder, sessionNotesFolderError, supportsObsidianVaults, type SessionNoteSettings } from "@/lib/session-notes";
import { relativeDate, shortDate } from "@/lib/format";

const devices = ref<Device[]>([]);
const loading = ref(true);
const error = ref("");
const actionError = ref("");
const editingId = ref<string | null>(null);
const confirmingId = ref<string | null>(null);
const pendingId = ref<string | null>(null);
const draftName = ref("");
const nameInput = useTemplateRef<HTMLInputElement>("nameInput");
let controller: AbortController | null = null;

const sessionNotesSupported = supportsObsidianVaults();
const noteSettings = ref<SessionNoteSettings | null>(null);
const noteSettingsLoading = ref(sessionNotesSupported);
const notePending = ref(false);
const noteError = ref("");
const noteMessage = ref("");
const folderDraft = ref(DEFAULT_SESSION_NOTES_FOLDER);

async function load() {
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loading.value = true;
  error.value = "";
  try {
    devices.value = (await listDevices(active.signal)).devices;
  } catch (cause) {
    if (!active.signal.aborted) error.value = errorMessage(cause, "Devices could not be loaded.");
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}

async function startRename(device: Device) {
  confirmingId.value = null;
  editingId.value = device.id;
  draftName.value = device.name;
  actionError.value = "";
  await nextTick();
  nameInput.value?.select();
}

async function saveName(device: Device) {
  const name = draftName.value.trim();
  if (!name) {
    actionError.value = "Enter a device name.";
    nameInput.value?.focus();
    return;
  }
  pendingId.value = device.id;
  actionError.value = "";
  try {
    const result = await renameDevice(device.id, name);
    Object.assign(device, result.device);
    editingId.value = null;
  } catch (cause) {
    actionError.value = errorMessage(cause, "The device name could not be saved.");
  } finally {
    pendingId.value = null;
  }
}

async function revoke(device: Device) {
  pendingId.value = device.id;
  actionError.value = "";
  try {
    const result = await revokeDevice(device.id);
    Object.assign(device, result.device);
    confirmingId.value = null;
  } catch (cause) {
    actionError.value = errorMessage(cause, "The device could not be revoked.");
  } finally {
    pendingId.value = null;
  }
}

async function loadSessionNotes() {
  if (!sessionNotesSupported) return;
  noteSettingsLoading.value = true;
  noteError.value = "";
  try {
    noteSettings.value = await loadSessionNoteSettings();
    folderDraft.value = noteSettings.value?.folder ?? DEFAULT_SESSION_NOTES_FOLDER;
  } catch (cause) {
    noteError.value = errorMessage(cause, "The Obsidian vault connection could not be loaded.");
  } finally {
    noteSettingsLoading.value = false;
  }
}

async function connectVault() {
  notePending.value = true;
  noteError.value = "";
  noteMessage.value = "";
  try {
    noteSettings.value = await connectObsidianVault(folderDraft.value);
    folderDraft.value = noteSettings.value.folder;
    noteMessage.value = `Connected ${noteSettings.value.vault.name}.`;
  } catch (cause) {
    if (!(cause instanceof DOMException && cause.name === "AbortError")) noteError.value = errorMessage(cause, "The Obsidian vault could not be connected.");
  } finally {
    notePending.value = false;
  }
}

async function saveNotesFolder() {
  const validationError = sessionNotesFolderError(folderDraft.value);
  if (validationError) {
    noteError.value = validationError;
    return;
  }
  notePending.value = true;
  noteError.value = "";
  noteMessage.value = "";
  try {
    noteSettings.value = await saveSessionNotesFolder(folderDraft.value);
    folderDraft.value = noteSettings.value.folder;
    noteMessage.value = "Notes folder saved.";
  } catch (cause) {
    noteError.value = errorMessage(cause, "The notes folder could not be saved.");
  } finally {
    notePending.value = false;
  }
}

async function disconnectVault() {
  notePending.value = true;
  noteError.value = "";
  noteMessage.value = "";
  try {
    await disconnectObsidianVault();
    noteSettings.value = null;
    noteMessage.value = "Obsidian vault disconnected.";
  } catch (cause) {
    noteError.value = errorMessage(cause, "The Obsidian vault could not be disconnected.");
  } finally {
    notePending.value = false;
  }
}

onMounted(() => {
  void load();
  void loadSessionNotes();
});
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section>
    <div class="mb-8"><h1 class="text-[28px] font-semibold tracking-[-0.025em]">Settings</h1><p class="mt-1.5 max-w-2xl text-sm leading-6 text-zinc-600 dark:text-zinc-400">Manage local session-note preferences and the machines allowed to connect to this Mimir deployment.</p></div>
    <section id="session-notes" aria-labelledby="session-notes-heading" class="mb-10 scroll-mt-6">
      <div class="mb-3">
        <h2 id="session-notes-heading" class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Session notes</h2>
        <p class="mt-1 text-xs text-zinc-500">Write readable session notes directly into Obsidian. The vault connection is stored only in this browser.</p>
      </div>
      <div class="border-y border-zinc-200 py-5 dark:border-zinc-800">
        <div v-if="!sessionNotesSupported" class="max-w-2xl">
          <p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Direct vault access is unavailable</p>
          <p class="mt-1 text-sm leading-6 text-zinc-500">Use a Chromium browser over HTTPS to connect an Obsidian vault. Markdown downloads remain available from every session.</p>
        </div>
        <div v-else-if="noteSettingsLoading" class="grid gap-3" aria-busy="true" aria-label="Loading session note settings">
          <div class="h-4 w-44 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" />
          <div class="h-8.5 w-full max-w-md animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" />
        </div>
        <div v-else class="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(280px,420px)] lg:gap-10">
          <div>
            <p class="text-xs font-medium text-zinc-500">Obsidian vault</p>
            <div class="mt-2 flex flex-wrap items-center gap-2">
              <span v-if="noteSettings" class="inline-flex min-w-0 items-center gap-2 text-sm font-medium text-zinc-900 dark:text-zinc-100"><FolderOpen class="size-4 shrink-0 text-zinc-500" aria-hidden="true" /><span class="truncate">{{ noteSettings.vault.name }}</span></span>
              <span v-else class="text-sm text-zinc-500">No vault connected</span>
              <Button variant="outline" :disabled="notePending" @click="connectVault"><FolderOpen class="size-3.5" />{{ noteSettings ? "Change vault" : "Connect vault" }}</Button>
              <Button v-if="noteSettings" variant="ghost" :disabled="notePending" @click="disconnectVault"><Unplug class="size-3.5" />Disconnect</Button>
            </div>
            <p class="mt-2 max-w-xl text-xs leading-5 text-zinc-500">Mimir can write only inside the directory you select. Each browser and machine must connect its own local vault.</p>
          </div>
          <form class="min-w-0" @submit.prevent="saveNotesFolder">
            <label for="session-notes-folder" class="text-xs font-medium text-zinc-700 dark:text-zinc-300">Notes folder</label>
            <div class="mt-2 flex items-center gap-2">
              <input id="session-notes-folder" v-model="folderDraft" maxlength="80" :disabled="!noteSettings || notePending" class="h-8.5 min-w-0 flex-1 rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 disabled:cursor-not-allowed disabled:opacity-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" />
              <Button type="submit" :disabled="!noteSettings || notePending || folderDraft.trim() === noteSettings.folder">Save</Button>
            </div>
            <p class="mt-2 break-all font-mono text-[11px] text-zinc-500">{{ folderDraft.trim() || DEFAULT_SESSION_NOTES_FOLDER }}/example-project/YYYY-MM-DD-a1b2c3d4.md</p>
          </form>
        </div>
        <p v-if="noteError" class="mt-4 text-xs text-red-700 dark:text-red-400" role="alert">{{ noteError }}</p>
        <p v-else-if="noteMessage" class="mt-4 text-xs text-zinc-600 dark:text-zinc-400" role="status">{{ noteMessage }}</p>
      </div>
    </section>
    <section aria-labelledby="devices-heading">
      <div class="mb-3 flex items-end justify-between gap-4"><div><h2 id="devices-heading" class="text-base font-semibold text-zinc-950 dark:text-zinc-50">Devices</h2><p class="mt-1 text-xs text-zinc-500">Names help distinguish where sessions originated. Revocation cannot be undone here.</p></div><span v-if="!loading" class="font-mono text-xs text-zinc-500">{{ devices.length }} total</span></div>
      <div class="border-y border-zinc-200 dark:border-zinc-800">
        <div v-if="loading" class="divide-y divide-zinc-200 dark:divide-zinc-800" aria-busy="true" aria-label="Loading devices"><div v-for="index in 3" :key="index" class="grid gap-3 py-5 sm:grid-cols-[minmax(0,1fr)_180px_120px]"><div class="h-4 w-48 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /><div class="h-4 w-28 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></div></div>
        <div v-else-if="error" class="py-14 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Devices unavailable</p><p class="mt-1 text-sm text-zinc-500">{{ error }}</p><Button class="mt-4" variant="outline" @click="load"><RotateCw class="size-3.5" />Retry</Button></div>
        <div v-else-if="!devices.length" class="py-14 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">No devices registered</p><p class="mt-1 text-sm text-zinc-500">Devices appear here after a harness connects to Mimir.</p></div>
        <div v-else class="divide-y divide-zinc-200 dark:divide-zinc-800">
          <article v-for="device in devices" :key="device.id" class="py-4 sm:py-5">
            <div class="grid gap-4 sm:grid-cols-[minmax(0,1fr)_180px_auto] sm:items-start">
              <div class="min-w-0">
                <form v-if="editingId === device.id" class="max-w-md" @submit.prevent="saveName(device)" @keydown.esc="editingId = null">
                  <label :for="`device-name-${device.id}`" class="sr-only">Device name</label>
                  <div class="flex items-center gap-2"><input :id="`device-name-${device.id}`" ref="nameInput" v-model="draftName" maxlength="120" :disabled="pendingId === device.id" class="h-8.5 min-w-0 flex-1 rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" /><Button type="submit" :disabled="pendingId === device.id"><Check class="size-3.5" />Save</Button><Button variant="ghost" size="icon" aria-label="Cancel rename" @click="editingId = null"><X class="size-4" /></Button></div>
                </form>
                <div v-else class="flex min-w-0 items-center gap-2"><DeviceIdentity :device="device" /><button v-if="!device.revoked_at" type="button" class="grid size-7 shrink-0 place-items-center rounded-[5px] text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:hover:bg-zinc-800 dark:hover:text-zinc-100" :aria-label="`Rename ${device.name}`" @click="startRename(device)"><Pencil class="size-3.5" /></button></div>
                <p class="mt-2 break-all font-mono text-[11px] text-zinc-500">{{ device.id }}</p>
                <p v-if="actionError && (editingId === device.id || confirmingId === device.id)" class="mt-2 text-xs text-red-700 dark:text-red-400" role="alert">{{ actionError }}</p>
              </div>
              <dl class="grid grid-cols-2 gap-x-4 gap-y-2 text-xs sm:block sm:space-y-2"><div><dt class="inline text-zinc-500">Status </dt><dd class="inline font-medium" :class="device.revoked_at ? 'text-red-700 dark:text-red-400' : 'text-zinc-800 dark:text-zinc-200'">{{ device.revoked_at ? "Revoked" : "Active" }}</dd></div><div><dt class="inline text-zinc-500">Last seen </dt><dd class="inline text-zinc-700 dark:text-zinc-300">{{ device.last_seen_at ? relativeDate(device.last_seen_at) : "Never" }}</dd></div><div><dt class="inline text-zinc-500">Sessions </dt><dd class="inline font-mono text-zinc-700 dark:text-zinc-300">{{ device.session_count }}</dd></div></dl>
              <div class="sm:min-w-52 sm:text-right"><p class="text-xs text-zinc-500">Observed harnesses</p><p class="mt-1 text-[13px] text-zinc-700 dark:text-zinc-300">{{ device.harnesses.length ? device.harnesses.join(", ") : "None observed" }}</p><p v-if="device.revoked_at" class="mt-2 text-xs text-zinc-500">Revoked {{ shortDate(device.revoked_at) }}</p><div v-else-if="confirmingId === device.id" class="mt-3 flex flex-wrap items-center gap-2 sm:justify-end"><span class="text-xs font-medium text-zinc-700 dark:text-zinc-300">Revoke this device?</span><Button :disabled="pendingId === device.id" class="border-red-700 bg-red-700 hover:bg-red-800 dark:border-red-500 dark:bg-red-600 dark:text-white dark:hover:bg-red-700" @click="revoke(device)">{{ pendingId === device.id ? "Revoking..." : "Confirm" }}</Button><Button variant="ghost" @click="confirmingId = null">Cancel</Button></div><Button v-else class="mt-3 text-red-700 hover:text-red-800 dark:text-red-400 dark:hover:text-red-300" variant="ghost" @click="editingId = null; confirmingId = device.id; actionError = ''"><ShieldOff class="size-3.5" />Revoke</Button></div>
            </div>
          </article>
        </div>
      </div>
    </section>
  </section>
</template>
