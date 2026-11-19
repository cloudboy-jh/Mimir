import { createRouter, createWebHistory } from "vue-router";
import DayPage from "@/pages/DayPage.vue";
import SessionsPage from "@/pages/SessionsPage.vue";
import SessionPage from "@/pages/SessionPage.vue";
import DevicesPage from "@/pages/DevicesPage.vue";
import LoginPage from "@/pages/LoginPage.vue";

// `/sessions/*` and other root paths belong to the machine API, so the SPA
// always runs under `/dashboard/`; a visit to the bare origin moves there.
if (!window.location.pathname.startsWith("/dashboard")) {
  window.location.replace(`/dashboard${window.location.pathname}${window.location.search}`);
}

export const router = createRouter({
  history: createWebHistory("/dashboard/"),
  routes: [
    { path: "/", redirect: { name: "day" } },
    { path: "/login", name: "login", component: LoginPage, meta: { standalone: true } },
    { path: "/days/:date?", name: "day", component: DayPage },
    { path: "/sessions", name: "sessions", component: SessionsPage },
    { path: "/sessions/:id", name: "session", component: SessionPage },
    { path: "/devices", name: "devices", component: DevicesPage },
    { path: "/:unknown(.*)*", redirect: { name: "day" } },
  ],
  scrollBehavior: (to, from, savedPosition) => {
    if (savedPosition) return savedPosition;
    if (to.path === from.path) return false;
    return { top: 0 };
  },
});
