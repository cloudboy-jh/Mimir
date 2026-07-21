import { onBeforeUnmount, onMounted } from "vue";

export function useAutoRefresh(refresh: () => void | Promise<void>, intervalMs = 10_000) {
  let timer: ReturnType<typeof setInterval> | undefined;
  let running = false;

  async function run() {
    if (document.visibilityState !== "visible" || running) return;
    running = true;
    try {
      await refresh();
    } finally {
      running = false;
    }
  }

  function handleVisibility() {
    if (document.visibilityState === "visible") void run();
  }

  onMounted(() => {
    timer = setInterval(() => void run(), intervalMs);
    document.addEventListener("visibilitychange", handleVisibility);
    window.addEventListener("focus", run);
  });

  onBeforeUnmount(() => {
    if (timer) clearInterval(timer);
    document.removeEventListener("visibilitychange", handleVisibility);
    window.removeEventListener("focus", run);
  });
}
