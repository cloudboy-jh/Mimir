import { describe, expect, it } from "vitest";
import { extractGeneratedTitle, normalizeSessionTitle } from "./session-titles";

describe("session titles", () => {
  it("normalizes human and model title forms", () => {
    expect(normalizeSessionTitle("  ## Title:  Fix   capture races  ")).toBe("Fix capture races");
    expect(normalizeSessionTitle('"Ship session titles"')).toBe("Ship session titles");
    expect(normalizeSessionTitle(" ")).toBeNull();
    expect(normalizeSessionTitle("x".repeat(201))).toBeNull();
  });

  it("extracts OpenAI, Anthropic, and reconstructed streaming titles", () => {
    expect(extractGeneratedTitle({ choices: [{ message: { content: "Generated title" } }] })).toBe("Generated title");
    expect(extractGeneratedTitle({ content: [{ type: "text", text: "Anthropic title" }] })).toBe("Anthropic title");
    expect(extractGeneratedTitle({ stream: true, content: "Streamed title" })).toBe("Streamed title");
    expect(extractGeneratedTitle({ choices: [] })).toBeNull();
  });
});
