import hljs from "highlight.js/lib/core";
import javascript from "highlight.js/lib/languages/javascript";
import typescript from "highlight.js/lib/languages/typescript";
import json from "highlight.js/lib/languages/json";
import bash from "highlight.js/lib/languages/bash";
import python from "highlight.js/lib/languages/python";
import diff from "highlight.js/lib/languages/diff";
import css from "highlight.js/lib/languages/css";
import xml from "highlight.js/lib/languages/xml";
import go from "highlight.js/lib/languages/go";
import rust from "highlight.js/lib/languages/rust";
import sql from "highlight.js/lib/languages/sql";
import yaml from "highlight.js/lib/languages/yaml";

for (const [name, language] of Object.entries({ javascript, typescript, json, bash, python, diff, css, xml, go, rust, sql, yaml })) hljs.registerLanguage(name, language);

/** HTML-safe output, including when the archive names an unsupported language. */
export function highlightCode(source: string, language?: string): string {
  if (language && hljs.getLanguage(language)) return hljs.highlight(source, { language, ignoreIllegals: true }).value;
  return source.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#39;");
}
const languageByExtension: Record<string, string> = {
  js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript", ts: "typescript", tsx: "typescript", mts: "typescript", cts: "typescript",
  json: "json", sh: "bash", bash: "bash", zsh: "bash", py: "python", diff: "diff", patch: "diff", css: "css", html: "xml", xml: "xml", svg: "xml", vue: "xml", go: "go", rs: "rust", sql: "sql",
  yml: "yaml", yaml: "yaml",
};
export function languageForPath(path: string): string | undefined {
  const extension = path.split(".").at(-1)?.toLowerCase() ?? "";
  return Object.hasOwn(languageByExtension, extension) ? languageByExtension[extension] : undefined;
}
