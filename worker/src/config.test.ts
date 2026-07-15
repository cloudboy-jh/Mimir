import { describe, expect, it } from "vitest";
import { shouldSave, validateConfigValues } from "./config";

describe("config", () => {
  it("applies repository and model exclusions", () => {
    const config = { enabled: true, excludeRepos: ["private-*"], excludeModels: ["*/free"], gapMinutes: 15 };
    expect(shouldSave(config, "private-app", "anthropic/claude")).toBe(false);
    expect(shouldSave(config, "public-app", "model/free")).toBe(false);
    expect(shouldSave(config, "public-app", "anthropic/claude")).toBe(true);
  });

  it("rejects unknown and malformed values", () => {
    expect(validateConfigValues({ unknown: true })).toContain("unknown config key");
    expect(validateConfigValues({ "session.gap_minutes": 0 })).toContain("1 to 1440");
    expect(validateConfigValues({ "save.enabled": "yes" })).toContain("boolean");
    expect(validateConfigValues({ "save.enabled": true, "save.exclude_repos": ["private-*"] })).toBe("");
  });
});
