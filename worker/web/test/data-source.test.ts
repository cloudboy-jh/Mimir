import { describe, expect, it } from "vitest";
import { fixturesAllowed } from "../src/lib/data-source";

describe("fixturesAllowed", () => {
  it("allows fixtures in development", () => {
    expect(fixturesAllowed("fixtures", true, "development")).toBe(true);
  });

  it("allows fixtures only in explicit demo production builds", () => {
    expect(fixturesAllowed("fixtures", false, "demo")).toBe(true);
    expect(fixturesAllowed("fixtures", false, "production")).toBe(false);
  });

  it("keeps live and unspecified sources live", () => {
    expect(fixturesAllowed("live", true, "development")).toBe(false);
    expect(fixturesAllowed(undefined, false, "production")).toBe(false);
  });
});
