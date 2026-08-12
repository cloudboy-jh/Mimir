import { describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { buildCandidate, validateEvidence } from "./release-evidence";

const sha = (value: string) => createHash("sha256").update(value).digest("hex");
const ids = [
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
];

async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "mimir-evidence-"));
  const payload = join(root, "payload");
  await Bun.write(join(payload, "checksums.txt"), "checksums");
  await Bun.write(join(payload, "install.sh"), "shell");
  await Bun.write(join(payload, "install.ps1"), "powershell");
  await Bun.write(join(payload, "mimir_1.2.3_windows_amd64.zip"), "binary");
  const candidate = await buildCandidate(payload, "v1.2.3", "a".repeat(40), 42);
  const candidatePath = join(root, "candidate.json");
  await writeFile(candidatePath, JSON.stringify(candidate) + "\n");
  const candidateHash = createHash("sha256")
    .update(await readFile(candidatePath))
    .digest("hex");
  const evidence = {
    schema_version: 1,
    repository: candidate.repository,
    tag: candidate.tag,
    commit: candidate.commit,
    candidate_run_id: 42,
    candidate_manifest_sha256: candidateHash,
    validated_asset_sha256: candidate.assets.at(-1)!.sha256,
    performed_by: "operator",
    performed_at: new Date().toISOString(),
    checks: ids.map((id, index) => ({
      id,
      status: "pass",
      transcript_sha256: sha(`${id}-${index}`),
      transcript_size: index + 1,
    })),
  };
  const evidencePath = join(root, "evidence.json");
  await writeFile(evidencePath, JSON.stringify(evidence));
  return { payload, candidatePath, evidencePath, evidence };
}

describe("release evidence", () => {
  test("accepts complete matching evidence", async () => {
    const value = await fixture();
    await expect(
      validateEvidence(value.candidatePath, value.evidencePath, value.payload, {
        tag: "v1.2.3",
        commit: "a".repeat(40),
        actor: "operator",
      }),
    ).resolves.toBeUndefined();
  });
  test("rejects missing checks", async () => {
    const value = await fixture();
    value.evidence.checks.pop();
    await writeFile(value.evidencePath, JSON.stringify(value.evidence));
    await expect(
      validateEvidence(value.candidatePath, value.evidencePath, value.payload),
    ).rejects.toThrow("incomplete");
  });
  test("rejects changed assets", async () => {
    const value = await fixture();
    await writeFile(join(value.payload, "install.sh"), "changed");
    await expect(
      validateEvidence(value.candidatePath, value.evidencePath, value.payload),
    ).rejects.toThrow("asset mismatch");
  });
  test("rejects unknown evidence fields", async () => {
    const value = await fixture();
    const changed = { ...value.evidence, token: "forbidden" };
    await writeFile(value.evidencePath, JSON.stringify(changed));
    await expect(
      validateEvidence(value.candidatePath, value.evidencePath, value.payload),
    ).rejects.toThrow("unknown or missing");
  });
});
