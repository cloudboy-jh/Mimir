export type Bindings = Env & {
  OPENROUTER_API_KEY: string;
  DASHBOARD_ACCESS_AUD?: string;
  DASHBOARD_ACCESS_TEAM_DOMAIN?: string;
  MIMIR_BUNDLE_VERSION?: string;
  MIMIR_BUNDLE_SHA256?: string;
};

export type DashboardIdentity = {
  email: string | null;
  name: string | null;
  source: "cloudflare-access" | "local-development";
};

export type AppEnv = {
  Bindings: Bindings;
  Variables: {
    tokenHash: string;
    tokenLabel: string;
    installationID: string | null;
    upstreamOpenRouterKey?: string;
    dashboardIdentity: DashboardIdentity;
  };
};
