import type { Hono } from "hono";
import type { AppEnv } from "../env";

const MACHINE_API_VERSION = 1;
const MACHINE_CAPABILITIES = [
  "canonical_exchanges",
  "harness_build_identity",
  "hermes_authorization",
  "machine_identity_association",
  "mcp",
  "query_api",
  "session_events",
  "session_git_artifacts",
  "session_lifecycle",
  "session_titles",
] as const;
const MACHINE_ASSOCIATION_KEYS = [
  "version",
  "installation_id",
  "name",
  "platform",
  "arch",
] as const;

export function registerMachineRoutes(app: Hono<AppEnv>) {
  app.get("/whoami", async (c) => {
    const [sessions, exchanges] = await Promise.all([
      c.env.DB.prepare(
        "SELECT COUNT(*) AS count FROM sessions WHERE parent_session_id IS NULL",
      ).first<{ count: number }>(),
      c.env.DB.prepare(
        "SELECT COUNT(*) AS count FROM exchanges WHERE capture_status = 'saved'",
      ).first<{ count: number }>(),
    ]);
    return c.json({
      service: "mimir",
      api_version: MACHINE_API_VERSION,
      capabilities: MACHINE_CAPABILITIES,
      url: new URL(c.req.url).origin,
      bundle_version: c.env.MIMIR_BUNDLE_VERSION ?? null,
      bundle_sha256: c.env.MIMIR_BUNDLE_SHA256 ?? null,
      sessions: sessions?.count ?? 0,
      log: exchanges?.count ?? 0,
    });
  });

  app.post("/machine/associate", async (c) => {
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    if (!body || typeof body !== "object" || Array.isArray(body))
      return c.json({ error: "body must be an object" }, 400);
    const values = body as Record<string, unknown>;
    if (
      Object.keys(values).some(
        (key) => !(MACHINE_ASSOCIATION_KEYS as readonly string[]).includes(key),
      ) ||
      Object.keys(values).length !== MACHINE_ASSOCIATION_KEYS.length
    ) {
      return c.json(
        {
          error:
            "body must contain exactly version, installation_id, name, platform, and arch",
        },
        400,
      );
    }
    if (values.version !== 1)
      return c.json({ error: "version must be 1" }, 400);
    if (
      typeof values.installation_id !== "string" ||
      !/^[a-f0-9]{32}$/.test(values.installation_id)
    ) {
      return c.json(
        {
          error: "installation_id must be 32 lowercase hexadecimal characters",
        },
        400,
      );
    }
    if (
      typeof values.name !== "string" ||
      values.name.length === 0 ||
      values.name.length > 200 ||
      /[\p{Cc}]/u.test(values.name)
    ) {
      return c.json(
        {
          error:
            "name must be a non-empty string of at most 200 characters without control characters",
        },
        400,
      );
    }
    for (const key of ["platform", "arch"] as const) {
      if (
        typeof values[key] !== "string" ||
        !/^[a-z0-9][a-z0-9._-]{0,63}$/.test(values[key])
      ) {
        return c.json({ error: `${key} must be a lowercase identifier` }, 400);
      }
    }

    const now = new Date().toISOString();
    await c.env.DB.batch([
      c.env.DB.prepare(
        `INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at)
       SELECT ?, ?, ?, ?, ?, ?
       WHERE EXISTS (SELECT 1 FROM access_tokens WHERE token_hash = ? AND (installation_id IS NULL OR installation_id = ?))
        ON CONFLICT(installation_id) DO UPDATE SET platform = excluded.platform, arch = excluded.arch, updated_at = excluded.updated_at
        WHERE machines.revoked_at IS NULL`,
      ).bind(
        values.installation_id,
        values.name,
        values.platform,
        values.arch,
        now,
        now,
        c.get("tokenHash"),
        values.installation_id,
      ),
      c.env.DB.prepare(
        `UPDATE access_tokens SET installation_id = ?
        WHERE token_hash = ? AND (installation_id IS NULL OR installation_id = ?)
          AND EXISTS (SELECT 1 FROM machines WHERE installation_id = ? AND revoked_at IS NULL)`,
      ).bind(
        values.installation_id,
        c.get("tokenHash"),
        values.installation_id,
        values.installation_id,
      ),
    ]);
    const token = await c.env.DB.prepare(
      "SELECT installation_id FROM access_tokens WHERE token_hash = ?",
    )
      .bind(c.get("tokenHash"))
      .first<{ installation_id: string | null }>();
    if (token?.installation_id !== values.installation_id)
      return c.json(
        { error: "token is already associated with another installation" },
        409,
      );
    return c.json({
      associated: true,
      installation_id: values.installation_id,
    });
  });

  app.post("/machine/tailnet", async (c) => {
    const installationID = c.get("installationID");
    if (!installationID) return c.json({ error: "token is not associated with a machine" }, 409);
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    const report = parseTailnetReport(body);
    if (typeof report === "string") return c.json({ error: report }, 400);
    const now = new Date().toISOString();
    const { self, peers } = report;
    await c.env.DB.batch([
      c.env.DB.prepare(
        "UPDATE machines SET tailnet_host_name = ?, tailnet_dns_name = ?, tailnet_name = ?, tailnet_ips = ?, tailnet_online = 1, tailnet_observed_at = ? WHERE installation_id = ?",
      ).bind(self.host_name, self.dns_name, self.tailnet_name, JSON.stringify(self.ips), now, installationID),
      ...peers.map((peer) =>
        c.env.DB.prepare(
          "UPDATE machines SET tailnet_online = ?, tailnet_observed_at = ? WHERE tailnet_dns_name = ? AND installation_id <> ?",
        ).bind(peer.online ? 1 : 0, now, peer.dns_name, installationID),
      ),
    ]);
    return c.json({ ok: true });
  });
}

const MAX_TAILNET_PEERS = 500;
type TailnetReport = {
  self: { host_name: string | null; dns_name: string | null; tailnet_name: string | null; ips: string[] };
  peers: Array<{ dns_name: string; online: boolean }>;
};

function tailnetText(value: unknown): string | null | undefined {
  if (value === undefined || value === null || value === "") return null;
  return typeof value === "string" && value.length <= 255 && !/[\p{Cc}]/u.test(value) ? value : undefined;
}

// Tailscale DNS names are fully qualified ("host.tailnet.ts.net."); store
// them without the trailing dot so peers match stored devices.
function dnsName(value: unknown) {
  const text = tailnetText(value);
  return text ? text.replace(/\.$/, "").toLowerCase() : text;
}

function parseTailnetReport(body: unknown): TailnetReport | string {
  const record = body && typeof body === "object" && !Array.isArray(body) ? (body as Record<string, unknown>) : null;
  const self = record?.self && typeof record.self === "object" && !Array.isArray(record.self) ? (record.self as Record<string, unknown>) : null;
  if (!record || !self) return "body must contain self";
  const hostName = tailnetText(self.host_name);
  const selfDNSName = dnsName(self.dns_name);
  const tailnetName = tailnetText(self.tailnet_name);
  if (hostName === undefined || selfDNSName === undefined || tailnetName === undefined) return "invalid self";
  const ips = self.ips ?? [];
  if (!Array.isArray(ips) || ips.length > 16 || ips.some((ip) => typeof ip !== "string" || ip.length > 64)) return "invalid self.ips";
  const rawPeers = record.peers ?? [];
  if (!Array.isArray(rawPeers) || rawPeers.length > MAX_TAILNET_PEERS) return "invalid peers";
  const peers: TailnetReport["peers"] = [];
  for (const raw of rawPeers) {
    const peer = raw && typeof raw === "object" && !Array.isArray(raw) ? (raw as Record<string, unknown>) : null;
    const name = dnsName(peer?.dns_name);
    if (!peer || name === undefined || typeof peer.online !== "boolean") return "invalid peer";
    if (name) peers.push({ dns_name: name, online: peer.online });
  }
  return { self: { host_name: hostName, dns_name: selfDNSName, tailnet_name: tailnetName, ips: ips as string[] }, peers };
}
