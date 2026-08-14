import { createRouter, createWebHistory } from "vue-router";
import SessionsPage from "@/pages/SessionsPage.vue";
import SessionDetailPage from "@/pages/SessionDetailPage.vue";
import SessionDiffPage from "@/pages/SessionDiffPage.vue";
import RequestsPage from "@/pages/RequestsPage.vue";
import RequestDetailPage from "@/pages/RequestDetailPage.vue";
import OverviewPage from "@/pages/OverviewPage.vue";
import LoginPage from "@/pages/LoginPage.vue";
import SettingsPage from "@/pages/SettingsPage.vue";

export const router = createRouter({
  history: createWebHistory(window.location.pathname.startsWith("/dashboard") ? "/dashboard/" : "/"),
  routes: [
    { path: "/", redirect: { name: "sessions" } },
    { path: "/login", name: "login", component: LoginPage, meta: { standalone: true } },
    { path: "/sessions", name: "sessions", component: SessionsPage },
    { path: "/sessions/:id", name: "session-detail", component: SessionDetailPage },
    { path: "/sessions/:id/diff", name: "session-diff", component: SessionDiffPage },
    { path: "/requests", name: "requests", component: RequestsPage },
    { path: "/requests/:id", name: "request-detail", component: RequestDetailPage },
    { path: "/overview", name: "overview", component: OverviewPage },
    { path: "/settings", name: "settings", component: SettingsPage },
  ],
  scrollBehavior: (to, from, savedPosition) => {
    if (savedPosition) return savedPosition;
    if (to.hash) return false;
    if (to.path === from.path) return false;
    return { top: 0 };
  },
});
