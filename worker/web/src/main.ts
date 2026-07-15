import { createApp } from "vue";
import App from "./App.vue";
import { router } from "./router";
import { initializeTheme } from "./lib/theme";
import "@fontsource-variable/ibm-plex-sans";
import "@fontsource/ibm-plex-mono/400.css";
import "./styles.css";

initializeTheme();
createApp(App).use(router).mount("#app");
