const MAX_TOOLS = 100;
const MAX_DEPTH = 16;
const MAX_NODES = 100_000;
const CALL_TYPES: Record<string, true> = { tool_use: true, toolCall: true, tool_call: true, function_call: true, tool: true };
const SKIP_FIELDS: Record<string, true> = { tools: true, parameters: true, arguments: true, schema: true, function: true };

// Index observed structured calls/results, never declared tools or prose names.
// Inputs and outputs can contain arbitrary source data, so do not recurse into them.
export function extractToolNames(request: unknown, response: unknown, toolActivity?: unknown): string[] {
  const names = new Set<string>();
  let remaining = MAX_NODES;
  function add(value: unknown) {
    if (typeof value !== "string") return;
    const name = value.trim();
    if (name && name.length <= 128 && !/[\u0000-\u001f\u007f]/.test(name)) names.add(name);
  }
  function walk(value: unknown, depth: number, calls = false) {
    if (depth > MAX_DEPTH || remaining-- <= 0 || names.size >= MAX_TOOLS || !value || typeof value !== "object") return;
    if (Array.isArray(value)) {
      for (const item of value) {
        walk(item, depth + 1, calls);
        if (remaining <= 0 || names.size >= MAX_TOOLS) break;
      }
      return;
    }
    const record = value as Record<string, unknown>;
    const kind = typeof record.type === "string" ? record.type : "";
    if (Object.hasOwn(CALL_TYPES, kind)) {
      add(record.name ?? record.tool ?? record.tool_name);
      return;
    }
    if (record.role === "tool" || kind === "toolResult" || kind === "tool_result") {
      add(record.name ?? record.tool_name);
      return;
    }
    if ((record.status === "succeeded" || record.status === "failed") && record.input && typeof record.input === "object") {
      add(record.name);
      return;
    }
    const fn = record.function;
    if (fn && typeof fn === "object" && !Array.isArray(fn)) {
      const detail = fn as Record<string, unknown>;
      if (calls || "arguments" in detail) add(detail.name);
    }
    for (const key in record) {
      if (!Object.hasOwn(record, key) || Object.hasOwn(SKIP_FIELDS, key)) continue;
      const nested = record[key];
      if ((key === "input" || key === "output") && !Array.isArray(nested)) continue;
      walk(nested, depth + 1, key === "tool_calls");
    }
  }
  walk(request, 0);
  walk(response, 0);
  walk(toolActivity, 0);
  return [...names].sort();
}

export function toolNameStatements(db: D1Database, exchangeId: string, names: string[]): D1PreparedStatement[] {
  return names.map((name) => db.prepare("INSERT OR IGNORE INTO exchange_tools(exchange_id, name) VALUES (?, ?)").bind(exchangeId, name));
}
