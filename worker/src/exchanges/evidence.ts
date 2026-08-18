import { parseJSON } from "../config/config-store";
import type { RequestKind } from "./exchange-types";
const MAX_FILES = 40;
const MAX_ERRORS = 10;
const MAX_WALK_DEPTH = 12;
const TRAILING_MESSAGES = 3;
const FILE_KEYS = new Set([
  "path",
  "file",
  "filepath",
  "file_path",
  "filename",
  "file_name",
  "notebook_path",
  "target_file",
  "abs_path",
  "absolute_path",
  "new_path",
  "old_path",
]);
const DEPENDENCY_PATH =
  /(?:^|\/)(?:node_modules|\.git|dist|build|out|vendor|\.venv|venv|__pycache__|\.next|\.nuxt|coverage|target)\//i;
// Diagnostics are line-anchored and require punctuation a compiler or runtime
// emits, so source code that merely contains the word "error" cannot match.
const DIAGNOSTICS = [
  /^[A-Z][A-Za-z.]*(?:Error|Exception)\b:[^\n]{1,180}/m,
  /^Traceback \(most recent call last\)/m,
  /^panic: [^\n]{1,180}/m,
  /^error(?:\[[A-Z0-9]+\])?: [^\n]{1,180}/m,
  /^[^\n]{0,90}error TS\d+: [^\n]{1,180}/m,
  /^\s*at [^\n]{1,120}:\d+:\d+/m,
];

// deriveSessionFields projects searchable facets from one exchange. Files come
// only from tool activity and errors only from explicit failure signals, so the
// facets describe what the agent did rather than what its prompt contained.
export function deriveSessionFields(
  request: unknown,
  response?: unknown,
  toolActivity?: unknown,
) {
  return {
    files: unique(deriveFiles(request, response, toolActivity), MAX_FILES),
    errors: unique(deriveErrors(request, response, toolActivity), MAX_ERRORS),
  };
}

function deriveFiles(
  request: unknown,
  response: unknown,
  toolActivity: unknown,
): string[] {
  const files: string[] = [];
  for (const input of toolInputs(request).concat(
    toolInputs(response),
    toolInputs(toolActivity),
  )) {
    for (const candidate of filePaths(input, 0)) {
      const normalized = normalizeFilePath(candidate);
      if (normalized) files.push(normalized);
    }
  }
  return files;
}

// toolInputs collects argument objects from Anthropic tool_use blocks, OpenAI
// function calls, and normalized harness toolCall blocks.
function toolInputs(value: unknown, depth = 0): unknown[] {
  if (depth > MAX_WALK_DEPTH || !value || typeof value !== "object") return [];
  if (Array.isArray(value))
    return value.flatMap((item) => toolInputs(item, depth + 1));
  const record = value as Record<string, unknown>;
  const found: unknown[] = [];
  if (
    (record.status === "succeeded" || record.status === "failed") &&
    typeof record.name === "string" &&
    record.input &&
    typeof record.input === "object"
  )
    found.push(record.input);
  if (
    record.type === "tool_use" &&
    record.input &&
    typeof record.input === "object"
  )
    found.push(record.input);
  if (record.type === "toolCall") {
    const argumentsValue =
      typeof record.arguments === "string"
        ? parseJSON(record.arguments)
        : record.arguments;
    if (argumentsValue && typeof argumentsValue === "object")
      found.push(argumentsValue);
  }
  const fn =
    record.function && typeof record.function === "object"
      ? (record.function as Record<string, unknown>)
      : null;
  if (fn && typeof fn.arguments === "string") {
    const parsed = parseJSON(fn.arguments);
    if (parsed && typeof parsed === "object") found.push(parsed);
  }
  for (const nested of Object.values(record))
    found.push(...toolInputs(nested, depth + 1));
  return found;
}

function filePaths(value: unknown, depth: number): string[] {
  if (depth > MAX_WALK_DEPTH || !value || typeof value !== "object") return [];
  if (Array.isArray(value))
    return value.flatMap((item) => filePaths(item, depth + 1));
  const paths: string[] = [];
  for (const [key, nested] of Object.entries(
    value as Record<string, unknown>,
  )) {
    if (typeof nested === "string") {
      if (FILE_KEYS.has(key.toLowerCase())) paths.push(nested);
      continue;
    }
    paths.push(...filePaths(nested, depth + 1));
  }
  return paths;
}

function normalizeFilePath(raw: string): string | null {
  const value = raw.trim().replace(/\\/g, "/").replace(/^\.\//, "");
  if (!value || value.length > 240) return null;
  if ((value.match(/ /g)?.length ?? 0) > 3) return null;
  if (!value.includes("/") && !/\.[A-Za-z0-9]{1,10}$/.test(value)) return null;
  if (DEPENDENCY_PATH.test(value)) return null;
  return value;
}

function deriveErrors(
  request: unknown,
  response: unknown,
  toolActivity: unknown,
): string[] {
  const errors: string[] = [];
  pushEnvelopeError(response, errors);
  const record =
    response && typeof response === "object"
      ? (response as Record<string, unknown>)
      : {};
  if (Array.isArray(record.events)) {
    for (const event of record.events) pushEnvelopeError(event, errors);
  }
  for (const message of trailingMessages(request))
    pushToolFailure(message, errors);
  if (Array.isArray(toolActivity)) {
    for (const activity of toolActivity) {
      if (!activity || typeof activity !== "object") continue;
      const normalized = activity as Record<string, unknown>;
      if (
        normalized.status === "failed" &&
        typeof normalized.output === "string"
      )
        pushSignature(firstDiagnostic(normalized.output), errors);
    }
  }
  return errors;
}

// pushEnvelopeError reads provider failure envelopes, which are the errors the
// proxy is actually positioned to observe.
function pushEnvelopeError(value: unknown, errors: string[]) {
  if (!value || typeof value !== "object") return;
  const record = value as Record<string, unknown>;
  const error = record.error;
  if (typeof error === "string") {
    pushSignature(error, errors);
    return;
  }
  if (!error || typeof error !== "object") return;
  const detail = error as Record<string, unknown>;
  const message = [detail.message, detail.type, detail.code].find(
    (part) => typeof part === "string" && part,
  ) as string | undefined;
  if (message)
    pushSignature(
      typeof detail.code === "string" && detail.code && detail.code !== message
        ? `${detail.code}: ${message}`
        : message,
      errors,
    );
}

// pushToolFailure only trusts explicitly flagged tool failures. An unflagged
// tool result is usually file content, and scanning it produced false errors.
function pushToolFailure(message: unknown, errors: string[]) {
  if (!message || typeof message !== "object") return;
  const record = message as Record<string, unknown>;
  const blocks = Array.isArray(record.content) ? record.content : [];
  for (const block of blocks) {
    if (!block || typeof block !== "object") continue;
    const detail = block as Record<string, unknown>;
    if (detail.type !== "tool_result" || detail.is_error !== true) continue;
    pushSignature(
      firstDiagnostic(
        messageText(detail.content) ||
          (typeof detail.content === "string" ? detail.content : ""),
      ),
      errors,
    );
  }
  if (record.role !== "tool") return;
  const flagged =
    record.is_error === true ||
    (typeof record.exit_code === "number" && record.exit_code !== 0);
  if (flagged)
    pushSignature(firstDiagnostic(messageText(record.content)), errors);
}

function firstDiagnostic(text: string): string {
  if (!text) return "";
  for (const pattern of DIAGNOSTICS) {
    const match = pattern.exec(text);
    if (match) return match[0];
  }
  return text.split("\n").find((line) => line.trim()) ?? "";
}

function pushSignature(raw: string, errors: string[]) {
  const signature = raw.replace(/\s+/g, " ").trim().slice(0, 200);
  if (signature && /[A-Za-z]/.test(signature)) errors.push(signature);
}

// trailingMessages limits error detection to the newest turn so a failure is
// not re-counted against every later exchange that replays the transcript.
function trailingMessages(request: unknown): unknown[] {
  const messages =
    request && typeof request === "object"
      ? (request as Record<string, unknown>).messages
      : null;
  return Array.isArray(messages) ? messages.slice(-TRAILING_MESSAGES) : [];
}

// deriveIntent summarizes the session's purpose from the first user message
// of the redacted request. Only the first captured exchange wins; later
// exchanges cannot overwrite the session intent.
export function deriveIntent(request: unknown): string | null {
  const messages =
    typeof request === "object" && request
      ? (request as Record<string, unknown>).messages
      : null;
  if (!Array.isArray(messages)) return null;
  for (const message of messages) {
    const record =
      typeof message === "object" && message
        ? (message as Record<string, unknown>)
        : {};
    if (record.role !== "user") continue;
    const collapsed = messageText(record.content).replace(/\s+/g, " ").trim();
    if (collapsed) return collapsed.slice(0, 200);
  }
  return null;
}

export function classifyRequestKind(
  declared: RequestKind,
  request: unknown,
): RequestKind {
  if (declared !== "primary") return declared;
  const messages =
    typeof request === "object" && request
      ? (request as Record<string, unknown>).messages
      : null;
  if (!Array.isArray(messages)) return declared;
  for (const message of messages) {
    const record =
      typeof message === "object" && message
        ? (message as Record<string, unknown>)
        : {};
    if (record.role !== "system" && record.role !== "developer") continue;
    const content = messageText(record.content).toLowerCase();
    if (
      content.includes("you are a title generator") ||
      (content.includes("generate a brief title") &&
        content.includes("output only"))
    )
      return "title";
  }
  return declared;
}

function messageText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .map((part) => {
      const record =
        typeof part === "object" && part
          ? (part as Record<string, unknown>)
          : {};
      return typeof record.text === "string" ? record.text : "";
    })
    .join(" ");
}

export function extractProvider(response: unknown) {
  const records =
    typeof response === "object" && response
      ? (response as Record<string, unknown>)
      : {};
  const events = Array.isArray(records.events) ? records.events : [response];
  for (const event of events) {
    const record =
      typeof event === "object" && event
        ? (event as Record<string, unknown>)
        : {};
    const provider = record.provider;
    if (typeof provider === "string") return provider;
    if (
      typeof provider === "object" &&
      provider &&
      typeof (provider as Record<string, unknown>).name === "string"
    )
      return (provider as Record<string, unknown>).name as string;
  }
  return null;
}

export function extractFinishReason(response: unknown) {
  const records =
    typeof response === "object" && response
      ? (response as Record<string, unknown>)
      : {};
  const events = Array.isArray(records.events) ? records.events : [response];
  for (const event of [...events].reverse()) {
    const record =
      typeof event === "object" && event
        ? (event as Record<string, unknown>)
        : {};
    const choices = Array.isArray(record.choices) ? record.choices : [];
    for (const choice of choices)
      if (
        typeof choice === "object" &&
        choice &&
        typeof (choice as Record<string, unknown>).finish_reason === "string"
      )
        return (choice as Record<string, unknown>).finish_reason as string;
    if (typeof record.stop_reason === "string") return record.stop_reason;
  }
  return null;
}

export function excerpt(value: string) {
  return value.slice(0, 8_000);
}

function unique(values: string[], limit: number) {
  return [
    ...new Set(values.map((value) => value.trim()).filter(Boolean)),
  ].slice(0, limit);
}
