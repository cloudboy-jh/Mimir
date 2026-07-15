import { createRouter, createWebHistory } from "vue-router";
import SessionsPage from "@/pages/SessionsPage.vue";
import SessionDetailPage from "@/pages/SessionDetailPage.vue";
import RequestsPage from "@/pages/RequestsPage.vue";
import RequestDetailPage from "@/pages/RequestDetailPage.vue";
import OverviewPage from "@/pages/OverviewPage.vue";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: "/sessions" },
    { path: "/dashboard", redirect: "/sessions" },
    { path: "/sessions", component: SessionsPage },
    { path: "/sessions/:id", component: SessionDetailPage },
    { path: "/requests", component: RequestsPage },
    { path: "/requests/:id", component: RequestDetailPage },
    { path: "/overview", component: OverviewPage },
  ],
  scrollBehavior: () => ({ top: 0 }),
});
