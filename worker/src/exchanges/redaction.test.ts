import { describe, expect, it } from "vitest";
import { redact } from "./redaction";

describe("value-level redaction", () => {
  it.each([
    ["token=`first second`;", "token=`[REDACTED]`;"],
    ['password="""first second"""', 'password="""[REDACTED]"""'],
    ["secret='''first second'''", "secret='''[REDACTED]'''"],
    ['apiKey =\n  "first second";', 'apiKey =\n  "[REDACTED]";'],
    ["token =\r\n  first-second;", "token =\r\n  [REDACTED];"],
    ["token=`first \\`second\\` ${third}`;", "token=`[REDACTED]`;"],
    ['password="""first "second" and \\"third\\""""', 'password="""[REDACTED]"""'],
    ["token=`first\nsecond`;\nkeep();", "token=`[REDACTED]\n[REDACTED]`;\nkeep();"],
    ['+password =\r\n+  """first\r\n+second\r\n+third""";\r\n+keep();', '+password =\r\n+  """[REDACTED]\r\n+[REDACTED]\r\n+[REDACTED]""";\r\n+keep();'],
    ['-token =\n-  "first second";\n+keep();', '-token =\n-  "[REDACTED]";\n+keep();'],
  ])("fully redacts source credentials: %s", (source, expected) => {
    expect(redact(source, [])).toBe(expected);
  });

  it("keeps every colliding key without leaking secret key text or overwriting literal names", () => {
    const input = {
      "customer-123": { value: 1 },
      "customer-456": [2],
      "[REDACTED]": "literal",
      "[REDACTED]#2": "reserved suffix",
      nested: { "customer-789": false, "customer-012": null },
    };
    const result = redact(input, ["customer-[0-9]+"]);
    expect(result).toEqual({
      "[REDACTED]#3": { value: 1 },
      "[REDACTED]#4": [2],
      "[REDACTED]": "literal",
      "[REDACTED]#2": "reserved suffix",
      nested: { "[REDACTED]": false, "[REDACTED]#2": null },
    });
    expect(JSON.stringify(result)).not.toContain("customer-");
    expect(input["customer-123"]).toEqual({ value: 1 });
    expect(redact({ "sk_abcdefghijklmnop": 1, "pk_abcdefghijklmnop": 2 }, [])).toEqual({
      "[REDACTED]": 1, "[REDACTED]#2": 2,
    });
  });

  it("preserves nested JSON types while redacting credential string fields", () => {
    const input = {
      token: "secret with spaces, quotes \" and \\ slashes",
      nested: [{ api_key: "private", password: 123, secret: false, token: null }],
      values: [null, true, false, 123, "123", "Bearer private", { access_token: "private" }],
    };
    expect(redact(input, ["123"])).toEqual({
      token: "[REDACTED]",
      nested: [{ api_key: "[REDACTED]", password: 123, secret: false, token: null }],
      values: [null, true, false, 123, "[REDACTED]", "Bearer [REDACTED]", { access_token: "[REDACTED]" }],
    });
    expect(input.token).toContain("secret with spaces");
    for (const scalar of [null, true, false, 123]) {
      expect(redact(scalar, [".*"])).toBe(scalar);
    }
  });

  it("redacts source credentials without consuming multiline patch structure", () => {
    const source = [
      '+const apiKey = "hello \\"world\\" and C:\\private";',
      "+password = 'two words';",
      '+const config = { "secret": "private", token: "other" };',
      "+TOKEN=unquoted-secret",
      '+const path = "C:\\Users\\name\\file.ts";',
      "+const message = 'quotes \" and backslashes \\ and café';",
      "",
    ].join("\n");
    expect(redact(source, [])).toBe([
      '+const apiKey = "[REDACTED]";',
      "+password = '[REDACTED]';",
      '+const config = { "secret": "[REDACTED]", token: "[REDACTED]" };',
      "+TOKEN=[REDACTED]",
      '+const path = "C:\\Users\\name\\file.ts";',
      "+const message = 'quotes \" and backslashes \\ and café';",
      "",
    ].join("\n"));
  });

  it("applies configured expressions only to strings and keeps invalid expressions inert", () => {
    const input = { text: 'customer-123\n"quoted" C:\\Users', count: 123, array: ["customer-456", "customer-789"] };
    expect(redact(input, ["builtin", "[", "customer-[0-9]+", '"quoted"', "C:\\\\Users"])).toEqual({
      text: "[REDACTED]\n[REDACTED] [REDACTED]", count: 123, array: ["[REDACTED]", "[REDACTED]"],
    });
    expect(redact({ text: "unchanged", count: 1 }, ["[", "[{}:,]"])).toEqual({ text: "unchanged", count: 1 });
  });

  it("redacts secrets in keys and safely retains own special-name properties", () => {
    const input = JSON.parse('{"sk_abcdefghijklmnop":"value","__proto__":{"token":"private"},"constructor":null}');
    const result = redact(input, ["customer-[0-9]+"]);
    expect(result).toEqual(JSON.parse('{"[REDACTED]":"value","__proto__":{"token":"[REDACTED]"},"constructor":null}'));
    expect(redact({ "customer-123": true }, ["customer-[0-9]+"])).toEqual({ "[REDACTED]": true });
    expect(Object.hasOwn(result as object, "__proto__")).toBe(true);
    expect(redact("pk_abcdefghijklmnop rk_abcdefghijklmnop", [])).toBe("[REDACTED] [REDACTED]");
  });

  it("preserves CRLF and literal escape sequences around redacted credentials", () => {
    expect(redact("TOKEN=private\r\n+path=C:\\new\\test\r\n", [])).toBe(
      "TOKEN=[REDACTED]\r\n+path=C:\\new\\test\r\n",
    );
    expect(redact("", ["["])).toBe("");
  });
});
