export type Bindings = Env & {
  OPENROUTER_API_KEY: string;
  DASHBOARD_ACCESS_AUD?: string;
  DASHBOARD_ACCESS_TEAM_DOMAIN?: string;
  MIMIR_BUNDLE_VERSION?: string;
  MIMIR_BUNDLE_SHA256?: string;
};

export type AppEnv = {
  Bindings: Bindings;
  Variables: { tokenHash: string; tokenLabel: string; upstreamOpenRouterKey?: string };
};
