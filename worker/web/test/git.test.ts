import { describe, expect, it } from "vitest";
import { gitArtifactCommitUrl, gitArtifactProvenance, outcomeCommitMatchesArtifact, outcomeUrlMatchesArtifact } from "../src/lib/git";
import type { GitArtifact } from "../src/lib/api";

const artifact: GitArtifact = {
  commit_sha: "a".repeat(40),
  parent_commit_sha: null,
  committed_at: null,
  subject: null,
  repository_url: "git@github.com:owner/repo.git",
  ref: "main",
  provenance: "git",
  patch_r2_key: "patches/a.patch",
  patch_sha256: "b".repeat(64),
  patch_bytes: 120,
  patch_files: 1,
  patch_additions: 2,
  patch_deletions: 1,
  capture_status: "saved",
  accepted_at: "2026-08-20T10:00:00Z",
  saved_at: "2026-08-20T10:00:01Z",
  failed_at: null,
  failure_code: null,
  created_at: "2026-08-20T10:00:00Z",
};

describe("Git artifact presentation", () => {
  it("derives a browsable commit link from the recorded repository", () => {
    expect(gitArtifactCommitUrl(artifact)).toBe(`https://github.com/owner/repo/commit/${artifact.commit_sha}`);
  });

  it("labels local Git provenance as unverified evidence", () => {
    expect(gitArtifactProvenance("git")).toEqual({ label: "Local checkout, unverified", unverified: true });
  });

  it("preserves a non-local provenance label", () => {
    expect(gitArtifactProvenance("signed-release-service")).toEqual({ label: "signed-release-service", unverified: false });
  });

  it("recognizes full and abbreviated outcome commits already represented by an artifact", () => {
    expect(outcomeCommitMatchesArtifact(artifact.commit_sha, [artifact])).toBe(true);
    expect(outcomeCommitMatchesArtifact(artifact.commit_sha.slice(0, 7), [artifact])).toBe(true);
    expect(outcomeCommitMatchesArtifact("c".repeat(40), [artifact])).toBe(false);
  });

  it("recognizes outcome URLs that duplicate an artifact commit link", () => {
    expect(outcomeUrlMatchesArtifact(`https://github.com/owner/repo/commit/${artifact.commit_sha}/`, [artifact])).toBe(true);
    expect(outcomeUrlMatchesArtifact("https://github.com/owner/repo/pull/42", [artifact])).toBe(false);
  });
});
