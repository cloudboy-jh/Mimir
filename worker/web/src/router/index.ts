import { createRouter, createWebHistory } from "vue-router";
import SessionsPage from "@/pages/SessionsPage.vue";
import SessionDetailPage from "@/pages/SessionDetailPage.vue";
import RequestsPage from "@/pages/RequestsPage.vue";
import RequestDetailPage from "@/pages/RequestDetailPage.vue";
import OverviewPage from "@/pages/OverviewPage.vue";

export const router = createRouter({
  history: createWebHistory("/dashboard/"),
  routes: [
    { path: "/", redirect: { name: "sessions" } },
    { path: "/sessions", name: "sessions", component: SessionsPage },
    { path: "/sessions/:id", name: "session-detail", component: SessionDetailPage },
    { path: "/requests", name: "requests", component: RequestsPage },
    { path: "/requests/:id", name: "request-detail", component: RequestDetailPage },
    { path: "/overview", name: "overview", component: OverviewPage },
  ],
  scrollBehavior: () => ({ top: 0 }),
});
