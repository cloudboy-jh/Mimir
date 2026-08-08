package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteMimirExtension creates a private Pi extension that exposes Mimir's
// existing CLI/API operations as agent tools without sharing machine tokens
// with the subprocess.
func WriteMimirExtension(dir, executable string) (string, error) {
	if dir == "" || executable == "" {
		return "", fmt.Errorf("writing Mimir Pi extension: directory and executable are required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating Mimir Pi extension directory: %w", err)
	}
	bin, _ := json.Marshal(executable)
	source := fmt.Sprintf(`import { spawn } from "node:child_process";
import { Type } from "@sinclair/typebox";

const mimir = %s;
	const outputLimit = 7 * 1024 * 1024;
function run(args: string[], signal?: AbortSignal): Promise<string> {
  return new Promise((resolve, reject) => {
    const child = spawn(mimir, args, { stdio: ["ignore", "pipe", "pipe"], windowsHide: true });
    let stdout = "", stderr = "";
    child.stdout.on("data", chunk => { stdout += chunk; if (stdout.length > outputLimit) { child.kill(); reject(new Error("mimir tool output exceeded 7 MiB")); } });
    child.stderr.on("data", chunk => { stderr += chunk; if (stderr.length > outputLimit) { child.kill(); reject(new Error("mimir tool diagnostics exceeded 7 MiB")); } });
    signal?.addEventListener("abort", () => child.kill(), { once: true });
    child.on("error", reject);
    child.on("close", code => code === 0 ? resolve(stdout.trim()) : reject(new Error(stderr.trim() || "mimir exited " + code)));
  });
}
const text = (value: string) => ({ content: [{ type: "text" as const, text: value }], details: {} });

export default function(pi: any) {
  pi.registerTool({ name: "list_sessions", label: "List Mimir sessions", description: "List captured Mimir sessions, optionally filtered by repository or outcome.", parameters: Type.Object({ repo: Type.Optional(Type.String()), outcome: Type.Optional(Type.String()) }), async execute(_id: string, p: any, signal: AbortSignal) { const a = ["list", "--json"]; if (p.repo) a.push("--repo", p.repo); if (p.outcome) a.push("--outcome", p.outcome); return text(await run(a, signal)); } });
  pi.registerTool({ name: "get_session", label: "Get Mimir session", description: "Get complete evidence and metadata for a Mimir session.", parameters: Type.Object({ id: Type.String() }), async execute(_id: string, p: any, signal: AbortSignal) { return text(await run(["session", "get", p.id, "--json"], signal)); } });
  pi.registerTool({ name: "search_memory", label: "Search Mimir", description: "Search saved Mimir sessions and local code memory.", parameters: Type.Object({ query: Type.String() }), async execute(_id: string, p: any, signal: AbortSignal) { return text(await run(["search", p.query, "--json"], signal)); } });
  pi.registerTool({ name: "set_outcome", label: "Set Mimir outcome", description: "Record a canonical outcome and optional reason for a Mimir session.", parameters: Type.Object({ id: Type.String(), outcome: Type.Union([Type.Literal("landed"), Type.Literal("discarded"), Type.Literal("abandoned"), Type.Literal("unresolved")]), reason: Type.Optional(Type.String()) }), async execute(_id: string, p: any, signal: AbortSignal) { const a = ["session", "outcome", p.id, p.outcome, "--json"]; if (p.reason) a.push("--reason", p.reason); return text(await run(a, signal)); } });
  pi.registerTool({ name: "doctor_check", label: "Check Mimir", description: "Run Mimir TUI diagnostics.", parameters: Type.Object({}), async execute(_id: string, _p: any, signal: AbortSignal) { return text(await run(["doctor", "--tui", "--json"], signal)); } });
}
`, string(bin))
	path := filepath.Join(dir, "mimir-tools.ts")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		return "", fmt.Errorf("writing Mimir Pi extension: %w", err)
	}
	return path, nil
}
