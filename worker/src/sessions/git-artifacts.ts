import { readConfig, stringArray } from "../config/config-store";
import { readBoundedText } from "../exchanges/response-codec";
import { redact } from "../exchanges/redaction";
import { rootSessionID } from "./session-queries";
import { normalizeRepositoryUrl } from "./git-repository";

export const MAX_GIT_ARTIFACT_BODY_BYTES = 5 * 1024 * 1024;
const MAX_GIT_ARTIFACTS = 50;
const SHA = /^[0-9a-f]{40}$/;

export type GitArtifact = {
  commit_sha: string;
  parent_commit_sha: string | null;
  committed_at: string | null;
  subject: string | null;
  repository_url: string | null;
  ref: string | null;
  provenance: string;
  patch_r2_key: string;
  patch_sha256: string;
  patch_bytes: number;
  patch_files: number;
  patch_additions: number;
  patch_deletions: number;
  capture_status: "accepted" | "saved" | "failed";
  accepted_at: string;
  saved_at: string | null;
  failed_at: string | null;
  failure_code: string | null;
  created_at: string;
};

type InputArtifact = {
  commit_sha: string;
  parent_commit_sha: string | null;
  committed_at: string | null;
  subject: string | null;
  repository_url: string | null;
  ref: string | null;
  provenance: string;
  patch: string;
};

type PreparedArtifact = InputArtifact & {
  patch: string;
  patchBytes: Uint8Array;
  patchSha256: string;
  patchR2Key: string;
  stats: ReturnType<typeof patchStats>;
};

export type IngestGitArtifactsResult =
  | { kind: "ok"; session_id: string; artifacts: GitArtifact[]; duplicates: number }
  | { kind: "invalid"; error: string }
  | { kind: "not-found" }
  | { kind: "conflict"; commit_sha: string }
  | {
      kind: "partial";
      session_id: string;
      artifacts: GitArtifact[];
      duplicates: number;
      failures: Array<{ commit_sha: string; failure_code: string }>;
    };

export async function readGitArtifactBody(
  request: Request,
): Promise<{ body: unknown } | { error: string }> {
  const declared = Number(request.headers.get("content-length") ?? 0);
  if (Number.isFinite(declared) && declared > MAX_GIT_ARTIFACT_BODY_BYTES)
    return { error: "Git artifact body too large" };
  let text: string;
  try {
    text = await readBoundedText(request.body, MAX_GIT_ARTIFACT_BODY_BYTES);
  } catch {
    return { error: "Git artifact body too large" };
  }
  try {
    return { body: JSON.parse(text) as unknown };
  } catch {
    return { error: "invalid JSON body" };
  }
}

export async function ingestGitArtifacts(
  db: D1Database,
  bucket: R2Bucket,
  requestedSessionID: string,
  body: unknown,
): Promise<IngestGitArtifactsResult> {
  const parsed = parseGitArtifacts(body);
  if ("error" in parsed) return { kind: "invalid", error: parsed.error };
  if (!(await db.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(requestedSessionID).first()))
    return { kind: "not-found" };

  const sessionID = await rootSessionID(db, requestedSessionID);
  const patterns = stringArray((await readConfig(db))["redact.patterns"]);
  const prepared: PreparedArtifact[] = [];
  for (const input of parsed.artifacts) {
    const patch = redact(input.patch, patterns);
    if (typeof patch !== "string")
      return { kind: "invalid", error: "patch must be a string" };
    const patchSha256 = await sha256(patch);
    prepared.push({
      ...input,
      patch,
      patchBytes: new TextEncoder().encode(patch),
      patchSha256,
      patchR2Key: `sessions/${sessionID}/git/${input.commit_sha}/${patchSha256}.patch`,
      stats: patchStats(patch),
    });
  }

  const artifacts: GitArtifact[] = [];
  const failures: Array<{ commit_sha: string; failure_code: string }> = [];
  let duplicates = 0;

  for (const input of prepared) {
    const existing = await loadGitArtifact(db, sessionID, input.commit_sha);
    if (existing) {
      if (existing.capture_status === "saved" && isRepairKey(existing.patch_r2_key, input.patchR2Key) &&
          await loadRepairAudit(bucket, sessionID, existing))
        input.patchR2Key = existing.patch_r2_key;
      if (!sameArtifact(existing, input))
        return { kind: "conflict", commit_sha: input.commit_sha };
      if (existing.capture_status === "saved") {
        artifacts.push(existing);
        duplicates++;
        continue;
      }
    }

    const acceptedAt = new Date().toISOString();
    await db
      .prepare(
        "INSERT OR IGNORE INTO session_git_artifacts(session_id, commit_sha, parent_commit_sha, committed_at, subject, repository_url, repository_key, ref, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'accepted', ?, ?)",
      )
      .bind(
        sessionID,
        input.commit_sha,
        input.parent_commit_sha,
        input.committed_at,
        input.subject,
        input.repository_url,
        normalizeRepositoryUrl(input.repository_url) ?? `session:${sessionID}`,
        input.ref,
        input.provenance,
        input.patchR2Key,
        input.patchSha256,
        input.patchBytes.byteLength,
        input.stats.files,
        input.stats.additions,
        input.stats.deletions,
        acceptedAt,
        acceptedAt,
      )
      .run();
    let stored = await loadGitArtifact(db, sessionID, input.commit_sha);
    if (!stored || !sameArtifact(stored, input))
      return { kind: "conflict", commit_sha: input.commit_sha };

    if (stored.capture_status === "failed") {
      await db
        .prepare(
          "UPDATE session_git_artifacts SET capture_status = 'accepted', failed_at = NULL, failure_code = NULL WHERE session_id = ? AND commit_sha = ? AND patch_sha256 = ? AND capture_status = 'failed'",
        )
        .bind(sessionID, input.commit_sha, input.patchSha256)
        .run();
    }

    try {
      await bucket.put(input.patchR2Key, input.patchBytes, {
        httpMetadata: { contentType: "text/plain; charset=utf-8" },
        customMetadata: {
          session_id: sessionID,
          commit_sha: input.commit_sha,
          sha256: input.patchSha256,
        },
      });
    } catch {
      const failedAt = new Date().toISOString();
      try {
        await db
          .prepare(
            "UPDATE session_git_artifacts SET capture_status = 'failed', saved_at = NULL, failed_at = ?, failure_code = 'r2_put_failed' WHERE session_id = ? AND commit_sha = ? AND patch_sha256 = ? AND capture_status = 'accepted'",
          )
          .bind(failedAt, sessionID, input.commit_sha, input.patchSha256)
          .run();
        stored = await loadGitArtifact(db, sessionID, input.commit_sha);
      } catch {
        // The accepted row remains retryable if recording the R2 failure is interrupted.
      }
      if (stored) artifacts.push(stored);
      failures.push({
        commit_sha: input.commit_sha,
        failure_code: "r2_put_failed",
      });
      continue;
    }

    try {
      const savedAt = new Date().toISOString();
      await db
        .prepare(
          "UPDATE session_git_artifacts SET capture_status = 'saved', saved_at = ?, failed_at = NULL, failure_code = NULL WHERE session_id = ? AND commit_sha = ? AND patch_sha256 = ? AND capture_status = 'accepted'",
        )
        .bind(savedAt, sessionID, input.commit_sha, input.patchSha256)
        .run();
      stored = await loadGitArtifact(db, sessionID, input.commit_sha);
    } catch {
      failures.push({
        commit_sha: input.commit_sha,
        failure_code: "d1_finalize_failed",
      });
      if (stored) artifacts.push(stored);
      continue;
    }
    if (!stored || stored.capture_status !== "saved") {
      failures.push({
        commit_sha: input.commit_sha,
        failure_code: "d1_finalize_failed",
      });
      if (stored) artifacts.push(stored);
      continue;
    }
    artifacts.push(stored);
  }
  if (failures.length)
    return {
      kind: "partial",
      session_id: sessionID,
      artifacts,
      duplicates,
      failures,
    };
  return { kind: "ok", session_id: sessionID, artifacts, duplicates };
}

export async function loadSessionGitArtifacts(db: D1Database, id: string) {
  const sessionID = await rootSessionID(db, id);
  const rows = await db
    .prepare(
      `SELECT ${GIT_ARTIFACT_COLUMNS} FROM session_git_artifacts WHERE session_id = ? ORDER BY COALESCE(committed_at, created_at), created_at, commit_sha`,
    )
    .bind(sessionID)
    .all<GitArtifact>();
  return rows.results;
}

type RepairAudit = {
  version: 1;
  operation: "git-artifact-repair";
  session_id: string;
  installation_id: string | null;
  token_hash: string;
  old: GitArtifact;
  new: GitArtifact;
};

export async function repairGitArtifact(
  db: D1Database,
  bucket: R2Bucket,
  sessionID: string,
  commitSHA: string,
  body: unknown,
  actor: { installationID: string | null; tokenHash: string },
) {
  if (!SHA.test(commitSHA)) return { kind: "invalid", error: "invalid commit SHA" } as const;
  if (!body || typeof body !== "object" || Array.isArray(body))
    return { kind: "invalid", error: "body must be an object" } as const;
  const record = body as Record<string, unknown>;
  if (Object.keys(record).some((key) => key !== "expected_digest" && key !== "artifact") ||
      typeof record.expected_digest !== "string" || !/^[0-9a-f]{64}$/.test(record.expected_digest))
    return { kind: "invalid", error: "body requires expected_digest (lowercase SHA-256) and artifact only" } as const;
  const parsed = parseGitArtifacts({ version: 1, commits: [record.artifact] });
  if ("error" in parsed) return { kind: "invalid", error: parsed.error } as const;
  const input = parsed.artifacts[0];
  if (input.commit_sha !== commitSHA)
    return { kind: "invalid", error: "artifact commit_sha must match path SHA" } as const;
  const session = await db.prepare("SELECT parent_session_id, installation_id FROM sessions WHERE id = ?")
    .bind(sessionID).first<{ parent_session_id: string | null; installation_id: string | null }>();
  if (!session) return { kind: "not-found" } as const;
  if (session.parent_session_id !== null)
    return { kind: "invalid", error: "repair requires the exact root session id" } as const;
  if (session.installation_id !== actor.installationID)
    return { kind: "forbidden" } as const;
  const old = await loadGitArtifact(db, sessionID, commitSHA);
  if (!old) return { kind: "not-found" } as const;
  const patterns = stringArray((await readConfig(db))["redact.patterns"]);
  const patch = redact(input.patch, patterns);
  if (typeof patch !== "string")
    return { kind: "invalid", error: "patch must be a string" } as const;
  const patchSha256 = await sha256(patch);
  const prefix = `sessions/${sessionID}/git/${commitSHA}/${patchSha256}`;
  const prepared: PreparedArtifact = {
    ...input, patch, patchSha256,
    patchBytes: new TextEncoder().encode(patch),
    patchR2Key: `${prefix}.patch`, stats: patchStats(patch),
  };
  const conflict = { kind: "conflict", commit_sha: commitSHA } as const;
  const success = (artifact: GitArtifact, auditKey: string, duplicate: boolean) => ({
    kind: "ok" as const, session_id: sessionID, artifacts: [artifact], duplicates: duplicate ? 1 : 0,
    repaired: { commit_sha: commitSHA, previous_digest: record.expected_digest as string,
      patch_sha256: patchSha256, audit_r2_key: auditKey, duplicate },
  });
  try {
    if (old.capture_status === "saved" && isRepairKey(old.patch_r2_key, prepared.patchR2Key) &&
        sameArtifact(old, { ...prepared, patchR2Key: old.patch_r2_key })) {
      const auditKey = `${old.patch_r2_key}.audit.json`;
      const audit = await loadRepairAudit(bucket, sessionID, old);
      if (audit?.old.patch_sha256 === record.expected_digest &&
          await bucket.head(old.patch_r2_key))
        return success(old, auditKey, true);
    }
    if (old.patch_sha256 !== record.expected_digest) return conflict;
    prepared.patchR2Key = `${prefix}/repair-${crypto.randomUUID()}.patch`;
    const now = new Date().toISOString();
    const { patch: _patch, ...inputMetadata } = input;
    const metadata: GitArtifact = {
      ...old, ...inputMetadata, patch_r2_key: prepared.patchR2Key, patch_sha256: patchSha256,
      patch_bytes: prepared.patchBytes.byteLength, patch_files: prepared.stats.files,
      patch_additions: prepared.stats.additions, patch_deletions: prepared.stats.deletions,
      capture_status: "saved", saved_at: now, failed_at: null, failure_code: null,
    };
    // Patch bodies never enter the audit or D1 metadata.
    const auditKey = `${prepared.patchR2Key}.audit.json`;
    const audit: RepairAudit = { version: 1, operation: "git-artifact-repair", session_id: sessionID,
      installation_id: actor.installationID, token_hash: actor.tokenHash, old, new: metadata };
    if (!await bucket.put(prepared.patchR2Key, prepared.patchBytes, {
      onlyIf: { etagDoesNotMatch: "*" }, sha256: patchSha256,
      httpMetadata: { contentType: "text/plain; charset=utf-8" },
      customMetadata: { session_id: sessionID, commit_sha: commitSHA, sha256: patchSha256 },
    })) throw new Error("patch write failed");
    // Immutable intent precedes the CAS. It proves completion only when D1 matches audit.new.
    if (!await bucket.put(auditKey, JSON.stringify(audit), {
      onlyIf: { etagDoesNotMatch: "*" }, httpMetadata: { contentType: "application/json" },
    })) throw new Error("audit write failed");
    const columns = artifactColumns();
    const updated = await db.prepare(
      `UPDATE session_git_artifacts SET repository_key = ?, ${columns.map((column) => `${column} = ?`).join(", ")} WHERE session_id = ? AND ${columns.map((column) => `${column} IS ?`).join(" AND ")} AND EXISTS (SELECT 1 FROM sessions WHERE id = ? AND parent_session_id IS NULL AND installation_id IS ?)`,
    ).bind(normalizeRepositoryUrl(metadata.repository_url) ?? `session:${sessionID}`,
      ...columns.map((column) => metadata[column]), sessionID,
      ...columns.map((column) => old[column]), sessionID, actor.installationID).run();
    if (updated.meta.changes !== 1) return conflict;
    return success(metadata, auditKey, false);
  } catch {
    return { kind: "failed", error: "Git artifact repair persistence failed", commit_sha: commitSHA } as const;
  }
}

function artifactColumns() {
  return GIT_ARTIFACT_COLUMNS.split(", ") as Array<keyof GitArtifact>;
}

async function loadRepairAudit(bucket: R2Bucket, sessionID: string, artifact: GitArtifact) {
  const object = await bucket.get(`${artifact.patch_r2_key}.audit.json`);
  const audit = object ? await object.json<RepairAudit>() : null;
  return audit?.version === 1 && audit.operation === "git-artifact-repair" &&
    audit.session_id === sessionID && audit.old.commit_sha === artifact.commit_sha &&
    artifactColumns().every((column) => audit.new[column] === artifact[column])
    ? audit : null;
}

function isRepairKey(key: string, canonicalKey: string) {
  const prefix = canonicalKey.slice(0, -6);
  return key.startsWith(`${prefix}/repair-`) &&
    /^repair-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.patch$/.test(key.slice(prefix.length + 1));
}

export async function loadGitArtifactPatch(
  db: D1Database,
  bucket: R2Bucket,
  requestedSessionID: string,
  commitSHA: string,
) {
  if (!SHA.test(commitSHA)) return { kind: "invalid" } as const;
  if (!(await db.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(requestedSessionID).first()))
    return { kind: "session-not-found" } as const;
  const sessionID = await rootSessionID(db, requestedSessionID);
  const artifact = await loadGitArtifact(db, sessionID, commitSHA);
  if (!artifact) return { kind: "artifact-not-found" } as const;
  if (artifact.capture_status !== "saved")
    return { kind: "artifact-unavailable" } as const;
  if (!artifact.patch_r2_key.startsWith(`sessions/${sessionID}/git/${commitSHA}/`))
    return { kind: "artifact-not-found" } as const;
  const object = await bucket.get(artifact.patch_r2_key);
  return object
    ? ({ kind: "stream", body: object.body, artifact } as const)
    : ({ kind: "patch-not-found" } as const);
}

function parseGitArtifacts(body: unknown): { artifacts: InputArtifact[] } | { error: string } {
  if (!body || typeof body !== "object" || Array.isArray(body))
    return { error: "body must be an object" };
  const record = body as Record<string, unknown>;
  if (Object.keys(record).some((key) => key !== "version" && key !== "commits"))
    return { error: "body must contain only version and commits" };
  if (record.version !== 1) return { error: "version must be 1" };
  const values = Array.isArray(record.commits) ? record.commits : null;
  if (!values || values.length === 0 || values.length > MAX_GIT_ARTIFACTS)
    return { error: "commits must contain between 1 and 50 artifacts" };
  const artifacts: InputArtifact[] = [];
  for (const value of values) {
    if (!value || typeof value !== "object" || Array.isArray(value))
      return { error: "each Git artifact must be an object" };
    const item = value as Record<string, unknown>;
    const allowed = new Set([
      "commit_sha",
      "parent_commit_sha",
      "committed_at",
      "subject",
      "patch",
      "repository_url",
      "ref",
      "provenance",
    ]);
    if (Object.keys(item).some((key) => !allowed.has(key)))
      return { error: "Git artifact contains an unknown field" };
    const commit = item.commit_sha;
    const parent = item.parent_commit_sha ?? null;
    const committedAt = item.committed_at ?? null;
    const subject = item.subject ?? null;
    const repositoryURL = item.repository_url ?? null;
    const ref = item.ref ?? null;
    const provenance = item.provenance ?? "git";
    if (typeof commit !== "string" || !SHA.test(commit))
      return { error: "commit_sha must be a full lowercase commit SHA" };
    if (parent !== null && (typeof parent !== "string" || !SHA.test(parent)))
      return { error: "parent_commit_sha must be a full lowercase commit SHA or null" };
    if (committedAt !== null && (typeof committedAt !== "string" || !validTimestamp(committedAt)))
      return { error: "committed_at must be an ISO timestamp or null" };
    if (subject !== null && (typeof subject !== "string" || subject.length > 500 || /[\p{Cc}]/u.test(subject)))
      return { error: "subject must be at most 500 characters without control characters" };
    if (repositoryURL !== null && !boundedText(repositoryURL, 2_048))
      return { error: "repository_url must be at most 2048 characters without control characters" };
    if (ref !== null && !boundedText(ref, 500))
      return { error: "ref must be at most 500 characters without control characters" };
    if (!boundedText(provenance, 100) || provenance.length === 0)
      return { error: "provenance must be a non-empty string of at most 100 characters" };
    if (typeof item.patch !== "string" || !item.patch)
      return { error: "patch must be a non-empty string" };
    artifacts.push({
      commit_sha: commit,
      parent_commit_sha: parent as string | null,
      committed_at: committedAt as string | null,
      subject: subject as string | null,
      repository_url: repositoryURL as string | null,
      ref: ref as string | null,
      provenance,
      patch: item.patch,
    });
  }
  return { artifacts };
}

function validTimestamp(value: string) {
  return !Number.isNaN(Date.parse(value)) && new Date(value).toISOString() === value;
}

function boundedText(value: unknown, maxLength: number): value is string {
  return (
    typeof value === "string" &&
    value.length <= maxLength &&
    !/[\p{Cc}]/u.test(value)
  );
}

const GIT_ARTIFACT_COLUMNS =
  "commit_sha, parent_commit_sha, committed_at, subject, repository_url, ref, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, saved_at, failed_at, failure_code, created_at";

async function loadGitArtifact(db: D1Database, sessionID: string, commitSHA: string) {
  return db
    .prepare(
      `SELECT ${GIT_ARTIFACT_COLUMNS} FROM session_git_artifacts WHERE session_id = ? AND commit_sha = ?`,
    )
    .bind(sessionID, commitSHA)
    .first<GitArtifact>();
}

function sameArtifact(stored: GitArtifact, input: PreparedArtifact) {
  return (
    stored.patch_sha256 === input.patchSha256 &&
    stored.patch_r2_key === input.patchR2Key &&
    stored.parent_commit_sha === input.parent_commit_sha &&
    stored.committed_at === input.committed_at &&
    stored.subject === input.subject &&
    stored.repository_url === input.repository_url &&
    stored.ref === input.ref &&
    stored.provenance === input.provenance
  );
}

function patchStats(patch: string) {
  let files = 0;
  let additions = 0;
  let deletions = 0;
  for (const line of patch.split("\n")) {
    if (line.startsWith("diff --git ")) files++;
    else if (line.startsWith("+") && !line.startsWith("+++")) additions++;
    else if (line.startsWith("-") && !line.startsWith("---")) deletions++;
  }
  return { files, additions, deletions };
}

async function sha256(value: string) {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(value),
  );
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}
