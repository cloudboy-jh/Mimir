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
  const hermesOpenRouter = !accessToken && token && c.req.path.startsWith("/v1/hermes/")
    ? await validHermesCredential(c.env.DB, token)
    : false;
  if (!accessToken && !hermesOpenRouter) return c.json({ error: "unauthorized" }, 401);
  c.set("tokenHash", accessToken?.token_hash ?? "hermes-openrouter");
  c.set("tokenLabel", accessToken?.label ?? "hermes-openrouter");
  if (hermesOpenRouter && token) c.set("upstreamOpenRouterKey", token);
  await next();
}

export function requestToken(headers: Headers) {
  const auth = headers.get("authorization");
  return auth?.startsWith("Bearer ") ? auth.slice(7) : headers.get("x-api-key") ?? undefined;
}

async function validToken(db: D1Database, token: string) {
  const hash = await sha256(token);
  return db.prepare("SELECT token_hash, label FROM access_tokens WHERE token_hash = ? AND revoked_at IS NULL").bind(hash).first<{ token_hash: string; label: string }>();
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

async function validHermesCredential(db: D1Database, token: string) {
  const hash = await sha256(token);
  return !!(await db.prepare("SELECT token_hash FROM hermes_credentials WHERE token_hash = ?").bind(hash).first());
}
