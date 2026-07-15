import { describe, expect, it } from "vitest";
import { requestToken } from "./auth";

describe("machine authentication", () => {
  it("accepts bearer and Anthropic authentication", () => {
    expect(requestToken(new Headers({ authorization: "Bearer machine" }))).toBe("machine");
    expect(requestToken(new Headers({ "x-api-key": "machine" }))).toBe("machine");
  });
});
