export function redact(value: unknown, patterns: string[]): unknown {
  const expressions: RegExp[] = [];
  for (const pattern of patterns) {
    if (pattern === "builtin") continue;
    try {
      expressions.push(new RegExp(pattern, "g"));
    } catch {
      // Invalid patterns are inert rather than blocking the proxy.
    }
  }

  function redactString(text: string): string {
    text = text
      .replace(/(?:sk|pk|rk)_[A-Za-z0-9_-]{16,}/g, "[REDACTED]")
      .replace(/(Bearer\s+)[A-Za-z0-9._-]+/gi, "$1[REDACTED]")
      .replace(
        /((?:api[_-]?key|token|secret|password)["']?[ \t]*[:=][ \t]*(?:(?:\r\n|\n|\r)[+-]?[ \t]*)*)(("""|'''|`|"|')(?:\\[\s\S]|(?!\3)[^\\])*\3|[^\s,"'`};]+)/gi,
        (_match, prefix: string, credential: string, delimiter: string | undefined) => {
          if (!delimiter) return prefix + "[REDACTED]";
          const content = credential.slice(delimiter.length, -delimiter.length);
          // Keep line endings and continuation-line diff markers so multiline
          // credentials cannot collapse the patch or change its line stats.
          const redacted = content.replace(/[^\r\n]+/g, (line, offset: number) => {
            const marker = offset > 0 ? /^[+ -]?[ \t]*/.exec(line)![0] : "";
            return marker + "[REDACTED]";
          });
          return prefix + delimiter + (content ? redacted : "[REDACTED]") + delimiter;
        },
      );
    for (const expression of expressions) {
      text = text.replace(expression, "[REDACTED]");
    }
    return text;
  }

  function visit(item: unknown): unknown {
    if (typeof item === "string") return redactString(item);
    if (Array.isArray(item)) return item.map(visit);
    if (item === null || typeof item !== "object") return item;
    // fromEntries creates own data properties even for keys such as __proto__.
    // Redact keys too: credentials and configured matches can occur there.
    const entries = Object.entries(item).map(([key, child]) => ({ key, child, name: redactString(key) }));
    // Reserve unchanged names before allocating suffixes, including literal
    // [REDACTED] keys and names that already look like generated suffixes.
    const used = new Set(entries.filter(({ key, name }) => key === name).map(({ name }) => name));
    return Object.fromEntries(entries.map(({ key, child, name }) => {
      let uniqueName = name;
      if (key !== name) {
        let suffix = 2;
        while (used.has(uniqueName)) uniqueName = `${name}#${suffix++}`;
        used.add(uniqueName);
      }
      return [
        uniqueName,
        typeof child === "string" && /(?:api[_-]?key|token|secret|password)$/i.test(key)
          ? "[REDACTED]"
          : visit(child),
      ];
    }));
  }

  return visit(value);
}
