import { createHash } from "node:crypto";
import { readdir, readFile, stat, writeFile } from "node:fs/promises";
import { basename, join } from "node:path";

const requiredChecks = [
  "bootstrap-install-clean-home",
  "install-human",
  "install-json",
  "update-human",
  "update-json",
  "doctor-human",
  "doctor-json",
  "deploy-real-cloudflare",
  "failed-deploy",
  "failed-deploy-recovery",
  "existing-install-receipts",
  "custom-cloudflare-resource-names",
  "stale-cached-metadata",
  "owned-artifacts-doctor-clean",
] as const;

type Asset = { name: string; size: number; sha256: string };
type Candidate = {
  schema_version: 1;
  repository: string;
  tag: string;
  commit: string;
  candidate_run_id: number;
  generated_at: string;
  assets: Asset[];
};
type Evidence = {
  schema_version: 1;
  repository: string;
  tag: string;
  commit: string;
  candidate_run_id: number;
  candidate_manifest_sha256: string;
  validated_asset_sha256: string;
  performed_by: string;
  performed_at: string;
  checks: Array<{
    id: string;
    status: "pass";
    transcript_sha256: string;
    transcript_size: number;
  }>;
};

const hash = (data: Uint8Array) =>
  createHash("sha256").update(data).digest("hex");
const exactKeys = (
  value: Record<string, unknown>,
  keys: string[],
  label: string,
) => {
  const got = Object.keys(value).sort().join(",");
  const want = [...keys].sort().join(",");
  if (got !== want) throw new Error(`${label} has unknown or missing fields`);
};
const assertHash = (value: string, label: string) => {
  if (!/^[0-9a-f]{64}$/.test(value))
    throw new Error(`${label} must be lowercase sha256`);
};

export async function buildCandidate(
  payload: string,
  tag: string,
  commit: string,
  runID: number,
  repository = "cloudboy-jh/Mimir",
): Promise<Candidate> {
  if (!/^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(tag))
    throw new Error("invalid release tag");
  if (!/^[0-9a-f]{40}$/.test(commit)) throw new Error("invalid commit sha");
  const names = (await readdir(payload)).sort();
  const assets: Asset[] = [];
  for (const name of names) {
    const path = join(payload, name);
    const info = await stat(path);
    if (!info.isFile())
      throw new Error(`payload entry is not a regular file: ${name}`);
    const data = await readFile(path);
    assets.push({ name, size: data.length, sha256: hash(data) });
  }
  if (
    !assets.some((asset) => asset.name === "checksums.txt") ||
    !assets.some((asset) => asset.name === "install.sh") ||
    !assets.some((asset) => asset.name === "install.ps1")
  )
    throw new Error("candidate payload is incomplete");
  return {
    schema_version: 1,
    repository,
    tag,
    commit,
    candidate_run_id: runID,
    generated_at: new Date().toISOString(),
    assets,
  };
}

export async function validateEvidence(
  candidatePath: string,
  evidencePath: string,
  payload: string,
  expected?: { tag?: string; commit?: string; actor?: string },
) {
  const candidateData = await readFile(candidatePath);
  const candidate = JSON.parse(candidateData.toString()) as Candidate;
  const evidence = JSON.parse(await readFile(evidencePath, "utf8")) as Evidence;
  exactKeys(
    candidate as unknown as Record<string, unknown>,
    [
      "schema_version",
      "repository",
      "tag",
      "commit",
      "candidate_run_id",
      "generated_at",
      "assets",
    ],
    "candidate",
  );
  exactKeys(
    evidence as unknown as Record<string, unknown>,
    [
      "schema_version",
      "repository",
      "tag",
      "commit",
      "candidate_run_id",
      "candidate_manifest_sha256",
      "validated_asset_sha256",
      "performed_by",
      "performed_at",
      "checks",
    ],
    "evidence",
  );
  if (candidate.schema_version !== 1 || evidence.schema_version !== 1)
    throw new Error("unsupported evidence schema");
  if (
    candidate.repository !== evidence.repository ||
    candidate.tag !== evidence.tag ||
    candidate.commit !== evidence.commit ||
    candidate.candidate_run_id !== evidence.candidate_run_id
  )
    throw new Error("evidence does not match candidate");
  if (
    !/^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(candidate.tag) ||
    !/^[0-9a-f]{40}$/.test(candidate.commit) ||
    !Number.isSafeInteger(candidate.candidate_run_id) ||
    candidate.candidate_run_id <= 0
  )
    throw new Error("invalid candidate identity");
  if (
    !Number.isFinite(Date.parse(candidate.generated_at)) ||
    !Number.isFinite(Date.parse(evidence.performed_at)) ||
    Date.parse(evidence.performed_at) < Date.parse(candidate.generated_at) ||
    Date.parse(evidence.performed_at) > Date.now() + 300_000
  )
    throw new Error("invalid evidence timestamp");
  if (expected?.tag && candidate.tag !== expected.tag)
    throw new Error("candidate tag mismatch");
  if (expected?.commit && candidate.commit !== expected.commit)
    throw new Error("candidate commit mismatch");
  if (expected?.actor && evidence.performed_by !== expected.actor)
    throw new Error("evidence actor mismatch");
  assertHash(evidence.candidate_manifest_sha256, "candidate manifest hash");
  if (evidence.candidate_manifest_sha256 !== hash(candidateData))
    throw new Error("candidate manifest hash mismatch");
  assertHash(evidence.validated_asset_sha256, "validated asset hash");
  if (
    !candidate.assets.some(
      (asset) => asset.sha256 === evidence.validated_asset_sha256,
    )
  )
    throw new Error("validated asset is not in candidate");
  const checks = new Map<string, Evidence["checks"][number]>();
  for (const check of evidence.checks) {
    exactKeys(
      check as unknown as Record<string, unknown>,
      ["id", "status", "transcript_sha256", "transcript_size"],
      "check",
    );
    if (
      check.status !== "pass" ||
      checks.has(check.id) ||
      check.transcript_size <= 0
    )
      throw new Error(`invalid check ${check.id}`);
    assertHash(check.transcript_sha256, `check ${check.id}`);
    checks.set(check.id, check);
  }
  if (
    checks.size !== requiredChecks.length ||
    requiredChecks.some((id) => !checks.has(id))
  )
    throw new Error("required release checks are incomplete");
  if (
    new Set(evidence.checks.map((check) => check.transcript_sha256)).size !==
    evidence.checks.length
  )
    throw new Error("transcript hashes must be unique");
  for (const asset of candidate.assets) {
    exactKeys(
      asset as unknown as Record<string, unknown>,
      ["name", "size", "sha256"],
      "asset",
    );
    if (basename(asset.name) !== asset.name)
      throw new Error("invalid asset name");
    const data = await readFile(join(payload, asset.name));
    if (data.length !== asset.size || hash(data) !== asset.sha256)
      throw new Error(`candidate asset mismatch: ${asset.name}`);
  }
  if (
    new Set(candidate.assets.map((asset) => asset.name)).size !==
    candidate.assets.length
  )
    throw new Error("duplicate candidate asset");
  const payloadNames = (await readdir(payload)).sort();
  if (
    payloadNames.join("\n") !==
    candidate.assets
      .map((asset) => asset.name)
      .sort()
      .join("\n")
  )
    throw new Error("payload contains unlisted assets");
}

async function main() {
  const [command, ...args] = process.argv.slice(2);
  const option = (name: string) => args[args.indexOf(name) + 1];
  if (command === "candidate") {
    const candidate = await buildCandidate(
      option("--payload"),
      option("--tag"),
      option("--commit"),
      Number(option("--run-id")),
      option("--repository"),
    );
    await writeFile(option("--out"), JSON.stringify(candidate, null, 2) + "\n");
    return;
  }
  if (command === "validate" || command === "verify-release") {
    await validateEvidence(
      option("--candidate"),
      option("--evidence"),
      option("--payload"),
      {
        tag: option("--expected-tag"),
        commit: option("--expected-commit"),
        actor: option("--expected-actor"),
      },
    );
    return;
  }
  throw new Error(
    "usage: release-evidence.ts candidate|validate|verify-release ...",
  );
}

if (import.meta.main)
  main().catch((error) => {
    console.error(error.message);
    process.exit(1);
  });
