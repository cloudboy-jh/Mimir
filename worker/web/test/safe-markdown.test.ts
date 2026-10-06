import { describe, expect, it } from "vitest";
import { renderSafeMarkdown } from "../src/lib/safe-markdown";
import { highlightCode, languageForPath } from "../src/lib/code-highlight";

describe("saved Markdown safety", () => {
  it("renders readable Markdown without activating raw HTML or script links", () => {
    const html = renderSafeMarkdown('**Important**\n\n<script>alert(1)</script>\n\n<img src=x onerror="alert(1)">\n\n[bad](javascript:alert(1))');
    expect(html).toContain("<strong>Important</strong>");
    expect(html).not.toMatch(/<script|<img|href=["']javascript:/i);
    expect(html).toContain("&lt;script&gt;");
    expect(html).toContain("onerror");
  });
  it("escapes fenced code for supported and unknown language names", () => {
    for (const language of ["typescript", "not-a-language"]) {
      const html = renderSafeMarkdown(`\`\`\`${language}\nconst value = "</code><script>attack()</script>";\n\`\`\``);
      expect(html).toContain("<pre><code");
      expect(html).not.toContain("<script>");
      expect(html).toContain("&lt;");
    }
  });
  it("keeps relative evidence links and hardens external links", () => {
    const html = renderSafeMarkdown("[capture](/requests/exchange-1) [source](https://example.com/commit/123)");
    expect(html).toContain('href="/requests/exchange-1"');
    expect(html).toContain('href="https://example.com/commit/123"');
    expect(html).toContain('rel="noopener noreferrer"');
  });
  it("shares HTML-safe syntax rendering with the diff reader", () => {
    expect(highlightCode('<img src="x" onerror="alert(1)">')).toBe('&lt;img src=&quot;x&quot; onerror=&quot;alert(1)&quot;&gt;');
    expect(highlightCode("<script>", "unknown")).toBe("&lt;script&gt;");
    expect(languageForPath("archive.__proto__")).toBeUndefined();
  });
});
