import type { Context, Next } from "hono";
import { createRemoteJWKSet, jwtVerify } from "jose";
import type { AppEnv, Bindings, DashboardIdentity } from "./types";

export async function authenticate(c: Context<AppEnv>, next: Next) {
  if (c.req.path === "/dashboard/auth" || c.req.path.startsWith("/dashboard/api/") || c.req.path.startsWith("/dashboard/log-objects/")) {
    const identity = await dashboardAccessIdentity(c.req.raw, c.env);
    if (!identity) return c.json({ error: "Cloudflare Access authentication required" }, 403);
    c.set("dashboardIdentity", identity);
    return next();
  }
  const token = requestToken(c.req.raw.headers);
  const accessToken = token ? await validToken(c.env.DB, token) : null;
  const hermesRoute = hermesCredentialRoute(c.req.path);
  if (accessToken?.installation_id && hermesRoute?.installationID && accessToken.installation_id !== hermesRoute.installationID) {
    return c.json({ error: "installation does not match authenticated machine" }, 403);
  }
  const hermesOpenRouter = !accessToken && token && hermesRoute
    ? await validHermesCredential(c.env.DB, token, hermesRoute.installationID)
    : null;
  if (!accessToken && !hermesOpenRouter) return c.json({ error: "unauthorized" }, 401);
  c.set("tokenHash", accessToken?.token_hash ?? "hermes-openrouter");
  c.set("tokenLabel", accessToken?.label ?? "hermes-openrouter");
  c.set("installationID", accessToken?.installation_id ?? hermesOpenRouter?.installation_id ?? null);
  if (hermesOpenRouter && token) c.set("upstreamOpenRouterKey", token);
  await recordLastSeen(c.env.DB, accessToken?.token_hash ?? null, c.get("installationID"));
  await next();
}

export function requestToken(headers: Headers) {
  const auth = headers.get("authorization");
  return auth?.startsWith("Bearer ") ? auth.slice(7) : headers.get("x-api-key") ?? undefined;
}

async function validToken(db: D1Database, token: string) {
  const hash = await sha256(token);
  return db.prepare("SELECT access_tokens.token_hash, access_tokens.label, access_tokens.installation_id FROM access_tokens LEFT JOIN machines ON machines.installation_id = access_tokens.installation_id WHERE access_tokens.token_hash = ? AND access_tokens.revoked_at IS NULL AND (access_tokens.installation_id IS NULL OR machines.revoked_at IS NULL)").bind(hash).first<{ token_hash: string; label: string; installation_id: string | null }>();
}

async function dashboardAccessIdentity(request: Request, env: Bindings): Promise<DashboardIdentity | null> {
  const hostname = new URL(request.url).hostname;
  if (hostname === "localhost" || hostname === "127.0.0.1") {
    return { email: null, name: "Local development", source: "local-development" };
  }
  if (!env.DASHBOARD_ACCESS_AUD || !env.DASHBOARD_ACCESS_TEAM_DOMAIN) return null;
  const token = request.headers.get("cf-access-jwt-assertion");
  if (!token) return null;
  try {
    const teamDomain = env.DASHBOARD_ACCESS_TEAM_DOMAIN.replace(/\/$/, "");
    const { payload } = await jwtVerify(token, createRemoteJWKSet(new URL(`${teamDomain}/cdn-cgi/access/certs`)), { issuer: teamDomain, audience: env.DASHBOARD_ACCESS_AUD });
    return {
      email: identityClaim(payload.email, 320),
      name: identityClaim(payload.name, 200),
      source: "cloudflare-access",
    };
  } catch {
    return null;
  }
}

function identityClaim(value: unknown, maxLength: number) {
  if (typeof value !== "string") return null;
  const normalized = value.trim();
  return normalized ? normalized.slice(0, maxLength) : null;
}

async function sha256(value: string) {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function hermesCredentialRoute(path: string): { installationID: string | null } | null {
  if (/^\/v1\/hermes\/(?:chat\/completions|models|key|credits)$/.test(path)) return { installationID: null };
  const scoped = path.match(/^\/v1\/hermes\/([a-f0-9]{32})\/(?:chat\/completions|models|key|credits)$/);
  return scoped ? { installationID: scoped[1] } : null;
}

async function validHermesCredential(db: D1Database, token: string, installationID: string | null) {
  const hash = await sha256(token);
  if (installationID) {
    return db.prepare("SELECT hermes_credentials.installation_id FROM hermes_credentials JOIN machines ON machines.installation_id = hermes_credentials.installation_id WHERE token_hash = ? AND hermes_credentials.installation_id = ? AND machines.revoked_at IS NULL")
      .bind(hash, installationID).first<{ installation_id: string }>();
  }
  const matches = await db.prepare("SELECT hermes_credentials.installation_id FROM hermes_credentials LEFT JOIN machines ON machines.installation_id = hermes_credentials.installation_id WHERE token_hash = ? AND (hermes_credentials.installation_id = '' OR machines.installation_id IS NOT NULL AND machines.revoked_at IS NULL) LIMIT 2")
    .bind(hash).all<{ installation_id: string }>();
  if (matches.results.length !== 1) return null;
  return { installation_id: matches.results[0].installation_id || null };
}

async function recordLastSeen(db: D1Database, tokenHash: string | null, installationID: string | null) {
  const now = new Date().toISOString();
  const cutoff = new Date(Date.now() - 5 * 60_000).toISOString();
  const statements: D1PreparedStatement[] = [];
  if (tokenHash) statements.push(db.prepare("UPDATE access_tokens SET last_used_at = ? WHERE token_hash = ? AND (last_used_at IS NULL OR last_used_at < ?)").bind(now, tokenHash, cutoff));
  if (installationID) statements.push(db.prepare("UPDATE machines SET last_seen_at = ?, updated_at = ? WHERE installation_id = ? AND revoked_at IS NULL AND (last_seen_at IS NULL OR last_seen_at < ?)").bind(now, now, installationID, cutoff));
  if (statements.length) await db.batch(statements);
}
