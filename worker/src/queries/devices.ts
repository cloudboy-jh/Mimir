import { QueryError } from "../shared/query-error";
import type { QueryContext } from "./query-context";
import type { DeviceIdentity } from "./session-records";

export type Tailnet = {
  host_name: string | null;
  dns_name: string | null;
  tailnet_name: string | null;
  ips: string[];
  online: boolean | null;
  observed_at: string | null;
};
export type Device = DeviceIdentity & {
  created_at: string;
  updated_at: string;
  last_seen_at: string | null;
  revoked_at: string | null;
  session_count: number;
  harnesses: string[];
  tailnet: Tailnet | null;
};

type DeviceRow = Omit<Device, "harnesses" | "tailnet"> & {
  harnesses: string;
  tailnet_host_name: string | null;
  tailnet_dns_name: string | null;
  tailnet_name: string | null;
  tailnet_ips: string | null;
  tailnet_online: number | null;
  tailnet_observed_at: string | null;
};

async function deviceRows(db: D1Database, installationID?: string): Promise<Device[]> {
  const statement = db.prepare(`
    WITH root_sessions AS (
      SELECT installation_id, COUNT(*) AS session_count FROM sessions
      WHERE installation_id IS NOT NULL AND parent_session_id IS NULL GROUP BY installation_id
    ), device_harnesses AS (
      SELECT installation_id, json_group_array(harness) AS harnesses FROM (
        SELECT DISTINCT installation_id, harness FROM sessions
        WHERE installation_id IS NOT NULL AND harness IS NOT NULL AND harness <> '' ORDER BY installation_id, harness
      ) GROUP BY installation_id
    )
    SELECT machines.installation_id AS id, machines.name, machines.platform, machines.arch,
      machines.created_at, machines.updated_at, machines.last_seen_at, machines.revoked_at,
      COALESCE(root_sessions.session_count, 0) AS session_count, COALESCE(device_harnesses.harnesses, '[]') AS harnesses,
      machines.tailnet_host_name, machines.tailnet_dns_name, machines.tailnet_name, machines.tailnet_ips,
      machines.tailnet_online, machines.tailnet_observed_at
    FROM machines
    LEFT JOIN root_sessions ON root_sessions.installation_id = machines.installation_id
    LEFT JOIN device_harnesses ON device_harnesses.installation_id = machines.installation_id
    ${installationID ? "WHERE machines.installation_id = ?" : ""}
    ORDER BY COALESCE(machines.last_seen_at, machines.created_at) DESC, machines.installation_id`);
  const rows = await (installationID ? statement.bind(installationID) : statement).all<DeviceRow>();
  return rows.results.map(({ harnesses, tailnet_host_name, tailnet_dns_name, tailnet_name, tailnet_ips, tailnet_online, tailnet_observed_at, ...device }) => ({
    ...device,
    harnesses: JSON.parse(harnesses) as string[],
    tailnet: tailnet_observed_at
      ? {
          host_name: tailnet_host_name,
          dns_name: tailnet_dns_name,
          tailnet_name,
          ips: tailnet_ips ? (JSON.parse(tailnet_ips) as string[]) : [],
          online: tailnet_online === null ? null : tailnet_online === 1,
          observed_at: tailnet_observed_at,
        }
      : null,
  }));
}

export async function listDevices(ctx: QueryContext) {
  return { devices: await deviceRows(ctx.db) };
}

export async function renameDevice(ctx: QueryContext, id: string, body: unknown): Promise<Device> {
  if (!body || typeof body !== "object" || Array.isArray(body) || Object.keys(body).some((key) => key !== "name"))
    throw new QueryError(400, "body must contain only name");
  const value = (body as Record<string, unknown>).name;
  const name = typeof value === "string" ? value.trim() : "";
  if (!name || name.length > 200 || /[\p{Cc}]/u.test(name))
    throw new QueryError(400, "name must be a non-empty string of at most 200 characters");
  const updated = await ctx.db
    .prepare("UPDATE machines SET name = ?, updated_at = ? WHERE installation_id = ?")
    .bind(name, new Date().toISOString(), id)
    .run();
  if (updated.meta.changes === 0) throw new QueryError(404, "device not found");
  return (await deviceRows(ctx.db, id))[0]!;
}

export async function revokeDevice(ctx: QueryContext, id: string): Promise<Device> {
  const now = new Date().toISOString();
  if (!(await ctx.db.prepare("SELECT 1 FROM machines WHERE installation_id = ?").bind(id).first()))
    throw new QueryError(404, "device not found");
  await ctx.db.batch([
    ctx.db.prepare("UPDATE machines SET revoked_at = COALESCE(revoked_at, ?), updated_at = ? WHERE installation_id = ?").bind(now, now, id),
    ctx.db.prepare("UPDATE access_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE installation_id = ?").bind(now, id),
  ]);
  return (await deviceRows(ctx.db, id))[0]!;
}
