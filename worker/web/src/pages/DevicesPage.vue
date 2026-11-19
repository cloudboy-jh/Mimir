<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { Check, Pencil, RotateCw, X } from "lucide-vue-next";
import Button from "@/components/ui/Button.vue";
import { errorMessage, listDevices, renameDevice, revokeDevice, type Device } from "@/lib/api";
import { harnessLabel, relativeDate, shortDate } from "@/lib/format";

const MAX_NAME_LENGTH = 200;
const platforms: Record<string, string> = { darwin: "macOS", macos: "macOS", windows: "Windows", win32: "Windows", linux: "Linux" };

const devices = ref<Device[]>([]);
const loading = ref(true);
const error = ref("");
const editingId = ref<string | null>(null);
const confirmingId = ref<string | null>(null);
const pendingId = ref<string | null>(null);
const actionError = ref("");
const draftName = ref("");
let controller: AbortController | null = null;

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
  actionError.value = "";
  editingId.value = device.id;
  draftName.value = device.name;
  await nextTick();
  document.getElementById(`device-name-${device.id}`)?.focus();
}

async function act(device: Device, action: () => Promise<Device>, failure: string) {
  pendingId.value = device.id;
  actionError.value = "";
  try {
    Object.assign(device, await action());
    editingId.value = null;
    confirmingId.value = null;
  } catch (cause) {
    actionError.value = errorMessage(cause, failure);
  } finally {
    pendingId.value = null;
  }
}

function saveName(device: Device) {
  if (!draftName.value.trim()) {
    actionError.value = "Enter a device name.";
    return;
  }
  void act(device, () => renameDevice(device.id, draftName.value), "The device name could not be saved.");
}

onMounted(load);
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section>
    <div class="mb-4 flex flex-wrap items-baseline justify-between gap-2">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Devices</h1>
        <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Machines connected to Mimir. Revoking a device invalidates its token; it cannot be undone here.</p>
      </div>
      <span v-if="!loading" class="font-mono text-xs text-zinc-600 dark:text-zinc-400">{{ devices.length }} total</span>
    </div>

    <div class="overflow-x-auto rounded-[7px] border border-zinc-200 bg-white dark:border-zinc-800 dark:bg-zinc-900">
      <table class="w-full min-w-[1000px] border-collapse text-left text-xs">
        <caption class="sr-only">Registered devices</caption>
        <thead class="border-b border-zinc-200 bg-zinc-50 text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400">
          <tr>
            <th scope="col" class="px-4 py-2.5 font-medium">Device</th>
            <th scope="col" class="px-3 py-2.5 font-medium">Platform</th>
            <th scope="col" class="px-3 py-2.5 font-medium">Tailnet</th>
            <th scope="col" class="px-3 py-2.5 font-medium">Last seen</th>
            <th scope="col" class="px-3 py-2.5 text-right font-medium">Sessions</th>
            <th scope="col" class="px-3 py-2.5 font-medium">Apps</th>
            <th scope="col" class="px-4 py-2.5 text-right font-medium">Access</th>
          </tr>
        </thead>
        <tbody v-if="loading" aria-busy="true">
          <tr v-for="index in 3" :key="index" class="border-b border-zinc-200 last:border-b-0 dark:border-zinc-800"><td colspan="7" class="px-4 py-4"><div class="h-4 w-2/5 animate-pulse bg-zinc-200 motion-reduce:animate-none dark:bg-zinc-800" /></td></tr>
        </tbody>
        <tbody v-else-if="error">
          <tr><td colspan="7" class="px-4 py-14 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">Devices unavailable</p><p class="mt-1 text-sm text-zinc-500">{{ error }}</p><Button class="mt-4" variant="outline" @click="load"><RotateCw class="size-3.5" />Retry</Button></td></tr>
        </tbody>
        <tbody v-else-if="!devices.length">
          <tr><td colspan="7" class="px-4 py-14 text-center"><p class="text-sm font-medium text-zinc-800 dark:text-zinc-200">No devices registered</p><p class="mt-1 text-sm text-zinc-500">Devices appear here after <code class="font-mono">mimir login</code> on a machine.</p></td></tr>
        </tbody>
        <tbody v-else>
          <tr v-for="device in devices" :key="device.id" class="border-b border-zinc-200 align-top last:border-b-0 dark:border-zinc-800" :class="device.revoked_at ? 'text-zinc-500 dark:text-zinc-500' : ''">
            <td class="max-w-[18rem] px-4 py-3">
              <form v-if="editingId === device.id" class="flex items-center gap-2" @submit.prevent="saveName(device)" @keydown.esc="editingId = null">
                <label :for="`device-name-${device.id}`" class="sr-only">Device name</label>
                <input :id="`device-name-${device.id}`" v-model="draftName" :maxlength="MAX_NAME_LENGTH" :disabled="pendingId === device.id" class="h-8.5 min-w-0 flex-1 rounded-[5px] border border-zinc-300 bg-white px-2.5 text-[13px] text-zinc-900 focus:border-teal-700 focus:outline-none focus:ring-1 focus:ring-teal-700 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-100" />
                <Button type="submit" size="icon" aria-label="Save device name" :disabled="pendingId === device.id"><Check class="size-3.5" /></Button>
                <Button variant="ghost" size="icon" aria-label="Cancel rename" @click="editingId = null"><X class="size-4" /></Button>
              </form>
              <div v-else class="flex min-w-0 items-center gap-1.5">
                <span class="truncate text-[13px] font-medium text-zinc-900 dark:text-zinc-100">{{ device.name }}</span>
                <button v-if="!device.revoked_at" type="button" class="grid size-7 shrink-0 place-items-center rounded-[5px] text-zinc-500 hover:bg-zinc-100 hover:text-zinc-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-teal-600 dark:hover:bg-zinc-800 dark:hover:text-zinc-100" :aria-label="`Rename ${device.name}`" @click="startRename(device)"><Pencil class="size-3.5" aria-hidden="true" /></button>
              </div>
              <p class="mt-1 break-all font-mono text-[11px] text-zinc-500">{{ device.id }}</p>
              <p v-if="actionError && (editingId === device.id || confirmingId === device.id)" class="mt-1 text-red-700 dark:text-red-400" role="alert">{{ actionError }}</p>
            </td>
            <td class="px-3 py-3 text-zinc-700 dark:text-zinc-300">{{ platforms[device.platform.toLowerCase()] ?? (device.platform || "Unknown") }} <span class="font-mono text-[11px] text-zinc-500">{{ device.arch }}</span></td>
            <td class="max-w-[16rem] px-3 py-3">
              <template v-if="device.tailnet">
                <p class="flex items-center gap-1.5">
                  <span class="size-1.5 shrink-0" :class="device.tailnet.online ? 'bg-emerald-600 dark:bg-emerald-400' : 'bg-zinc-400 dark:bg-zinc-600'" aria-hidden="true" />
                  <span class="font-medium text-zinc-800 dark:text-zinc-200">{{ device.tailnet.online === null ? "Unknown" : device.tailnet.online ? "Online" : "Offline" }}</span>
                  <span v-if="device.tailnet.tailnet_name" class="truncate text-zinc-600 dark:text-zinc-400">· {{ device.tailnet.tailnet_name }}</span>
                </p>
                <p v-if="device.tailnet.dns_name || device.tailnet.host_name" class="mt-1 truncate font-mono text-[11px] text-zinc-600 dark:text-zinc-400" :title="device.tailnet.dns_name ?? undefined">{{ device.tailnet.dns_name || device.tailnet.host_name }}</p>
                <p v-if="device.tailnet.ips.length" class="mt-0.5 break-all font-mono text-[11px] text-zinc-600 dark:text-zinc-400">{{ device.tailnet.ips.join(", ") }}</p>
                <p v-if="device.tailnet.observed_at" class="mt-0.5 text-[11px] text-zinc-500">Observed {{ relativeDate(device.tailnet.observed_at) }}</p>
              </template>
              <span v-else class="text-zinc-500">Not reported</span>
            </td>
            <td class="whitespace-nowrap px-3 py-3 text-zinc-700 dark:text-zinc-300"><time v-if="device.last_seen_at" :datetime="device.last_seen_at" :title="device.last_seen_at">{{ relativeDate(device.last_seen_at) }}</time><span v-else class="text-zinc-500">Never</span></td>
            <td class="px-3 py-3 text-right font-mono text-zinc-700 dark:text-zinc-300">{{ device.session_count }}</td>
            <td class="max-w-[12rem] px-3 py-3 text-zinc-700 dark:text-zinc-300">{{ device.harnesses.length ? device.harnesses.map(harnessLabel).join(", ") : "None" }}</td>
            <td class="px-4 py-3 text-right">
              <span v-if="device.revoked_at" class="font-medium text-red-700 dark:text-red-400">Revoked {{ shortDate(device.revoked_at) }}</span>
              <div v-else-if="confirmingId === device.id" class="flex flex-wrap items-center justify-end gap-2" role="group" :aria-label="`Confirm revoking ${device.name}`">
                <span class="font-medium text-zinc-700 dark:text-zinc-300">Revoke {{ device.name }}?</span>
                <Button :disabled="pendingId === device.id" class="border-red-700 bg-red-700 hover:bg-red-800 dark:border-red-500 dark:bg-red-600 dark:text-white dark:hover:bg-red-700" @click="act(device, () => revokeDevice(device.id), 'The device could not be revoked.')">{{ pendingId === device.id ? "Revoking…" : "Revoke" }}</Button>
                <Button variant="ghost" :disabled="pendingId === device.id" @click="confirmingId = null">Cancel</Button>
              </div>
              <Button v-else variant="outline" @click="editingId = null; actionError = ''; confirmingId = device.id">Revoke</Button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
