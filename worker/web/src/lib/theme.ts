import { ref } from "vue";

export type Theme = "light" | "dark";
export const theme = ref<Theme>("light");

export function setTheme(next: Theme) {
  theme.value = next;
  document.documentElement.classList.toggle("dark", next === "dark");
  localStorage.setItem("mimir-theme", next);
}

export function initializeTheme() {
  const saved = localStorage.getItem("mimir-theme");
  setTheme(saved === "dark" || saved === "light" ? saved : matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
}
