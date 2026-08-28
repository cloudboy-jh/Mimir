declare module "*.vue" {
  import type { DefineComponent } from "vue";
  const component: DefineComponent<Record<string, never>, Record<string, never>, unknown>;
  export default component;
}

declare const __MIMIR_FIXTURES__: boolean;

interface ImportMetaEnv {
  readonly VITE_MIMIR_DATA_SOURCE?: "fixtures" | "live";
  readonly DEV: boolean;
  readonly MODE: string;
}
