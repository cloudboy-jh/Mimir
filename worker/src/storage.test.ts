import { describe, expect, it } from "vitest";
import { captureReceipt, sessionStatusResponse, type CaptureSummary } from "./storage";

describe("captureReceipt", () => {
  const receipt = (status: CaptureSummary["status"], saved: number, failed: number, pending: number) => captureReceipt({
    status,
    saved_exchanges: saved,
    failed_exchanges: failed,
    pending_exchanges: pending,
    last_saved_at: null,
  });

  it("uses compact copy for every user-visible capture state", () => {
    expect(receipt("saved", 1, 0, 0)).toEqual({ label: "Saved to Mimir", detail: "1 exchange in this session", action_label: "View session" });
    expect(receipt("pending", 2, 0, 1)).toEqual({ label: "Saving to Mimir...", detail: "3 exchanges", action_label: "View session" });
    expect(receipt("partial", 2, 1, 0)).toEqual({ label: "Partially saved", detail: "2 of 3 exchanges", action_label: "View details" });
    expect(receipt("failed", 0, 2, 0)).toEqual({ label: "Mimir couldn't save this session", detail: "2 exchanges", action_label: "View details" });
    expect(receipt("empty", 0, 0, 0)).toEqual({ label: "Not captured", detail: "No exchanges in this session", action_label: "View session" });
    expect(receipt("pending", 1, 1, 1)).toEqual({ label: "Partially saved", detail: "1 saved · 1 failed · 1 pending", action_label: "View details" });
  });

  it("only advertises a deep link when dashboard Access is available", () => {
    const summary: CaptureSummary = { status: "saved", saved_exchanges: 1, failed_exchanges: 0, pending_exchanges: 0, last_saved_at: null };
    expect(sessionStatusResponse("https://mimir.example/sessions/id/status", "session/id", summary, { outcome: "unresolved" }, true)).toMatchObject({
      dashboard_url: "https://mimir.example/dashboard/sessions/session%2Fid",
      receipt: { action_label: "View session" },
    });
    expect(sessionStatusResponse("https://mimir.example/sessions/id/status", "session/id", summary, { outcome: "unresolved" }, false)).toMatchObject({
      dashboard_url: null,
      receipt: { action_label: null },
    });
  });
});
