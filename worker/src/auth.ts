import type { Context, Next } from "hono";
import { createRemoteJWKSet, jwtVerify } from "jose";
import type { AppEnv, Bindings } from "./types";

export async function authenticate(c: Context<AppEnv>, next: Next) {
  if (c.req.path.startsWith("/dashboard/api/") || c.req.path.startsWith("/dashboard/log-objects/")) {
    if (!(await validDashboardAccess(c.req.raw, c.env))) return c.json({ error: "Cloudflare Access authentication required" }, 403);
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

async function validDashboardAccess(request: Request, env: Bindings) {
  const hostname = new URL(request.url).hostname;
  if (hostname === "localhost" || hostname === "127.0.0.1") return true;
  if (!env.DASHBOARD_ACCESS_AUD || !env.DASHBOARD_ACCESS_TEAM_DOMAIN) return false;
  const token = request.headers.get("cf-access-jwt-assertion");
  if (!token) return false;
  try {
    const teamDomain = env.DASHBOARD_ACCESS_TEAM_DOMAIN.replace(/\/$/, "");
    await jwtVerify(token, createRemoteJWKSet(new URL(`${teamDomain}/cdn-cgi/access/certs`)), { issuer: teamDomain, audience: env.DASHBOARD_ACCESS_AUD });
    return true;
  } catch {
    return false;
  }
}

async function sha256(value: string) {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

async function validHermesCredential(db: D1Database, token: string) {
  const hash = await sha256(token);
  return !!(await db.prepare("SELECT token_hash FROM hermes_credentials WHERE token_hash = ?").bind(hash).first());
}
