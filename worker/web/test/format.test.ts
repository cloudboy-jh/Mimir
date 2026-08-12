import { describe, expect, it } from "vitest";

import { duration } from "../src/lib/format";

describe("duration", () => {
  const start = "2026-08-12T10:00:00Z";

  it("keeps short sessions in minutes", () => {
    expect(duration(start, "2026-08-12T10:42:00Z")).toBe("42m");
  });

  it("formats long sessions as hours and minutes", () => {
    expect(duration(start, "2026-08-12T12:56:00Z")).toBe("2h 56m");
  });

  it("omits zero trailing minutes", () => {
    expect(duration(start, "2026-08-12T13:00:00Z")).toBe("3h");
  });

  it("labels active sessions", () => {
    expect(duration(start, null)).toBe("In progress");
  });

  it("keeps sub-minute completed sessions visible", () => {
    expect(duration(start, "2026-08-12T10:00:10Z")).toBe("1m");
  });

  it("rejects malformed timestamps", () => {
    expect(duration("not-a-date", "2026-08-12T10:00:00Z")).toBe("-");
    expect(duration(start, "not-a-date")).toBe("-");
  });

  it("rejects end times before the start", () => {
    expect(duration(start, "2026-08-12T09:59:00Z")).toBe("-");
  });
});
