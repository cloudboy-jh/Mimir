import { normalizeSessionTitle } from "../sessions/titles";
import type { RequestKind } from "./exchange-types";
export const MAX_REPORTED_EXCHANGE_BYTES = 20 * 1024 * 1024;
const MAX_REQUEST_BYTES = 10 * 1024 * 1024;
const MAX_JSON_DEPTH = 64;
const MAX_JSON_VALUES = 100_000;
const MAX_STRING_CHARS = 2 * 1024 * 1024;
const EXCHANGE_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
const REQUEST_KINDS: Record<RequestKind, true> = {
  primary: true,
  title: true,
  summary: true,
  compaction: true,
};
const PAYLOAD_FIELDS: Record<string, true> = {
  exchange_id: true,
  ts: true,
  model: true,
  provider: true,
  request: true,
  response: true,
  tool_activity: true,
  usage: true,
  latency_ms: true,
  request_kind: true,
  title: true,
};
const USAGE_FIELDS: Record<string, true> = {
  input_tokens: true,
  output_tokens: true,
  cache_read_tokens: true,
  cache_write_tokens: true,
};
const TOOL_ACTIVITY_FIELDS: Record<string, true> = {
  name: true,
  input: true,
  status: true,
  output: true,
};
const TOOL_NAME = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
const MAX_TOOL_ACTIVITIES = 1_000;

export type NormalizedToolActivity = {
  name: string;
  input: Record<string, unknown>;
  status: "succeeded" | "failed";
  output?: string;
};

type ReportedExchange = {
  exchange_id: string;
  ts: string;
  model: string;
  provider: string | null;
  request: unknown;
  response: unknown;
  tool_activity: NormalizedToolActivity[];
  usage: {
    input_tokens: number;
    output_tokens: number;
    cache_read_tokens: number;
    cache_write_tokens: number;
  };
  latency_ms: number;
  request_kind: RequestKind;
  title: string | null;
};
export function parseReportedExchange(
  input: unknown,
): ReportedExchange | { error: string } {
  if (!input || typeof input !== "object" || Array.isArray(input))
    return { error: "exchange must be an object" };
  const body = input as Record<string, unknown>;
  if (Object.keys(body).some((field) => !PAYLOAD_FIELDS[field]))
    return { error: "exchange contains unknown fields" };
  if (
    typeof body.exchange_id !== "string" ||
    !EXCHANGE_ID.test(body.exchange_id)
  )
    return { error: "invalid exchange_id" };
  if (typeof body.ts !== "string" || Number.isNaN(Date.parse(body.ts)))
    return { error: "invalid ts" };
  if (
    typeof body.model !== "string" ||
    body.model.length === 0 ||
    body.model.length > 256
  )
    return { error: "invalid model" };
  if (
    body.provider !== undefined &&
    body.provider !== null &&
    (typeof body.provider !== "string" ||
      body.provider.length === 0 ||
      body.provider.length > 256)
  )
    return { error: "invalid provider" };
  if (!("request" in body) || !("response" in body))
    return { error: "request and response are required" };
  const requestError = validateJSONValue(body.request, MAX_REQUEST_BYTES);
  if (requestError) return { error: `invalid request: ${requestError}` };
  const responseError = validateJSONValue(
    body.response,
    MAX_REPORTED_EXCHANGE_BYTES,
  );
  if (responseError) return { error: `invalid response: ${responseError}` };
  const toolActivity = parseToolActivity(body.tool_activity);
  if (typeof toolActivity === "string") return { error: toolActivity };
  if (
    !body.usage ||
    typeof body.usage !== "object" ||
    Array.isArray(body.usage)
  )
    return { error: "invalid usage" };
  const usage = body.usage as Record<string, unknown>;
  if (
    Object.keys(usage).some((field) => !USAGE_FIELDS[field]) ||
    !("input_tokens" in usage) ||
    !("output_tokens" in usage)
  )
    return { error: "usage must contain input_tokens and output_tokens" };
  if (
    !boundedInteger(usage.input_tokens) ||
    !boundedInteger(usage.output_tokens) ||
    (usage.cache_read_tokens !== undefined &&
      !boundedInteger(usage.cache_read_tokens)) ||
    (usage.cache_write_tokens !== undefined &&
      !boundedInteger(usage.cache_write_tokens))
  )
    return { error: "invalid usage token counts" };
  if (!boundedInteger(body.latency_ms)) return { error: "invalid latency_ms" };
  if (
    typeof body.request_kind !== "string" ||
    !REQUEST_KINDS[body.request_kind as RequestKind]
  )
    return { error: "invalid request_kind" };
  if (body.title !== undefined && normalizeSessionTitle(body.title) === null)
    return { error: "invalid title" };
  return {
    exchange_id: body.exchange_id,
    ts: new Date(body.ts).toISOString(),
    model: body.model,
    provider: typeof body.provider === "string" ? body.provider : null,
    request: body.request,
    response: body.response,
    tool_activity: toolActivity,
    usage: {
      input_tokens: usage.input_tokens as number,
      output_tokens: usage.output_tokens as number,
      cache_read_tokens: (usage.cache_read_tokens as number | undefined) ?? 0,
      cache_write_tokens:
        (usage.cache_write_tokens as number | undefined) ?? 0,
    },
    latency_ms: body.latency_ms as number,
    request_kind: body.request_kind as RequestKind,
    title: body.title === undefined ? null : normalizeSessionTitle(body.title),
  };
}

function parseToolActivity(input: unknown): NormalizedToolActivity[] | string {
  if (!Array.isArray(input)) return "tool_activity must be an array";
  if (input.length > MAX_TOOL_ACTIVITIES)
    return "too many tool_activity entries";
  const activities: NormalizedToolActivity[] = [];
  for (const value of input) {
    if (!value || typeof value !== "object" || Array.isArray(value))
      return "invalid tool_activity entry";
    const activity = value as Record<string, unknown>;
    if (Object.keys(activity).some((field) => !TOOL_ACTIVITY_FIELDS[field]))
      return "tool_activity contains unknown fields";
    if (typeof activity.name !== "string" || !TOOL_NAME.test(activity.name))
      return "invalid tool_activity name";
    if (
      !activity.input ||
      typeof activity.input !== "object" ||
      Array.isArray(activity.input)
    )
      return "tool_activity input must be an object";
    const inputError = validateJSONValue(activity.input, MAX_REQUEST_BYTES);
    if (inputError) return `invalid tool_activity input: ${inputError}`;
    if (activity.status !== "succeeded" && activity.status !== "failed")
      return "invalid tool_activity status";
    if (
      activity.output !== undefined &&
      (typeof activity.output !== "string" ||
        activity.output.length > MAX_STRING_CHARS)
    )
      return "invalid tool_activity output";
    activities.push({
      name: activity.name,
      input: activity.input as Record<string, unknown>,
      status: activity.status,
      ...(typeof activity.output === "string"
        ? { output: activity.output }
        : {}),
    });
  }
  return activities;
}

function validateJSONValue(root: unknown, byteLimit: number): string | null {
  let encoded: string;
  try {
    encoded = JSON.stringify(root);
  } catch {
    return "not JSON-safe";
  }
  if (encoded === undefined) return "not JSON-safe";
  if (new TextEncoder().encode(encoded).byteLength > byteLimit)
    return "too large";
  const stack: Array<{ value: unknown; depth: number }> = [
    { value: root, depth: 0 },
  ];
  let count = 0;
  while (stack.length) {
    const { value, depth } = stack.pop()!;
    if (++count > MAX_JSON_VALUES) return "too many values";
    if (depth > MAX_JSON_DEPTH) return "too deeply nested";
    if (typeof value === "number" && !Number.isFinite(value))
      return "numbers must be finite";
    if (typeof value === "string" && value.length > MAX_STRING_CHARS)
      return "string too large";
    if (Array.isArray(value)) {
      for (const child of value) stack.push({ value: child, depth: depth + 1 });
    } else if (value && typeof value === "object") {
      for (const [key, child] of Object.entries(
        value as Record<string, unknown>,
      )) {
        if (key.length > 1_000) return "object key too large";
        stack.push({ value: child, depth: depth + 1 });
      }
    } else if (
      value !== null &&
      !["string", "number", "boolean"].includes(typeof value)
    ) {
      return "not JSON-safe";
    }
  }
  return null;
}

function boundedInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}
