import { createApp } from "vue";
import App from "./App.vue";
import { router } from "./router";
import { initializeTheme } from "./lib/theme";
import "@fontsource-variable/ibm-plex-sans";
import "@fontsource/ibm-plex-mono/400.css";
import "./styles.css";

initializeTheme();
window.addEventListener("mimir:auth-required", () => {
  if (window.location.pathname === "/login") return;
  const returnTo = router.resolve(router.currentRoute.value.fullPath).href;
  window.location.assign(`/login?returnTo=${encodeURIComponent(returnTo)}`);
});
createApp(App).use(router).mount("#app");
