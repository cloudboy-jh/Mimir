import { describe, expect, it } from "vitest";
import { commitUrl, externalEvidenceUrl, gitArtifactCommitUrl, gitArtifactProvenance, normalizeRepositoryUrl, outcomeEvidenceArtifact, outcomeUrlMatchesArtifact } from "../src/lib/git";
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

  it("distinguishes unverified local sources from independently named capture sources", () => {
    expect(gitArtifactProvenance("git").unverified).toBe(true);
    expect(gitArtifactProvenance("local-import").unverified).toBe(true);
    expect(gitArtifactProvenance("signed-release-service").unverified).toBe(false);
  });

  it("matches outcome evidence only to an unambiguous commit in the recorded repository", () => {
    expect(outcomeEvidenceArtifact({ commit: artifact.commit_sha.slice(0, 7) }, [artifact])).toBe(artifact);
    expect(outcomeEvidenceArtifact({ commit: artifact.commit_sha, repository_url: "https://github.com/owner/other" }, [artifact])).toBeNull();
    const collision = { ...artifact, commit_sha: `${"a".repeat(7)}${"b".repeat(33)}` };
    expect(outcomeEvidenceArtifact({ commit: "a".repeat(7) }, [artifact, collision])).toBeNull();
    expect(outcomeEvidenceArtifact({ commit: "not-a-commit" }, [artifact])).toBeNull();
  });

  it("rejects executable outcome links and strips credentials from external evidence", () => {
    expect(externalEvidenceUrl("javascript:alert(1)")).toBeNull();
    expect(externalEvidenceUrl("data:text/html,<script>alert(1)</script>")).toBeNull();
    expect(externalEvidenceUrl("https://user:secret@example.com/pr/1")).toBe("https://example.com/pr/1");
    expect(commitUrl({ commit_url: "https://user:secret@github.com/owner/repo/commit/abc" })).toBe("https://github.com/owner/repo/commit/abc");
  });

  it("canonicalizes transport, credentials and host case without conflating case-sensitive GitLab paths", () => {
    expect(normalizeRepositoryUrl("ssh://git@GITHUB.COM:22/Owner/Repo.git/")).toBe("https://github.com/owner/repo");
    expect(normalizeRepositoryUrl("https://user:secret@github.com/owner/repo.git/?token=secret#fragment")).toBe("https://github.com/owner/repo");
    expect(normalizeRepositoryUrl("https://gitlab.example/Team/Repo.git")).toBe("https://gitlab.example/Team/Repo");
    expect(normalizeRepositoryUrl("/local/checkout")).toBeNull();
    expect(normalizeRepositoryUrl("javascript:alert(1)")).toBeNull();
  });

  it("recognizes outcome URLs that duplicate an artifact commit link", () => {
    expect(outcomeUrlMatchesArtifact(`https://github.com/owner/repo/commit/${artifact.commit_sha}/`, [artifact])).toBe(true);
    expect(outcomeUrlMatchesArtifact("https://github.com/owner/repo/pull/42", [artifact])).toBe(false);
  });
});
