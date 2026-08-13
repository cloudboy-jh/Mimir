import { describe, expect, it } from "vitest";
import type { Device } from "../src/lib/api";
import { fixtureRequest } from "../src/lib/dev-fixtures";

describe("device fixtures", () => {
  it("lists, renames, and revokes devices", async () => {
    const listed = await fixtureRequest<{ devices: Device[] }>("/dashboard/api/devices");
    const device = listed.devices.find((item) => !item.revoked_at);
    expect(device).toBeDefined();

    const renamed = await fixtureRequest<{ device: Device }>(`/dashboard/api/devices/${device!.id}`, {
      method: "PATCH",
      body: JSON.stringify({ name: "Renamed fixture" }),
    });
    expect(renamed.device.name).toBe("Renamed fixture");

    const revoked = await fixtureRequest<{ device: Device }>(`/dashboard/api/devices/${device!.id}/revoke`, { method: "POST" });
    expect(revoked.device.revoked_at).not.toBeNull();
  });

  it("includes a device that has never been seen", async () => {
    const listed = await fixtureRequest<{ devices: Device[] }>("/dashboard/api/devices");
    expect(listed.devices.some((device) => device.last_seen_at === null)).toBe(true);
  });

  it("limits session devices to identity fields", async () => {
    const result = await fixtureRequest<{ sessions: Array<{ device: object | null }> }>("/dashboard/api/sessions?limit=1");
    expect(Object.keys(result.sessions[0].device ?? {})).toEqual(["id", "name", "platform", "arch"]);
  });
});
