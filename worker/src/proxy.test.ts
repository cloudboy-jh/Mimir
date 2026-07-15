import { describe, expect, it } from "vitest";
import { buildUpstreamHeaders } from "./proxy";

describe("proxy boundaries", () => {
  it("replaces auth and strips private metadata upstream", () => {
    const headers = buildUpstreamHeaders(new Headers({ authorization: "Bearer machine", "x-api-key": "machine", "x-mimir-session": "session", "x-mimir-repo": "repo" }), "openrouter");
    expect(headers.get("authorization")).toBe("Bearer openrouter");
    expect(headers.get("x-api-key")).toBeNull();
    expect(headers.get("x-mimir-session")).toBeNull();
    expect(headers.get("x-mimir-repo")).toBeNull();
  });
});
