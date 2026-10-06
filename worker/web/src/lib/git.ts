import type { GitArtifact, OutcomeEvidence } from "@/lib/api";

// normalizeRepositoryUrl turns any recorded remote form (SCP, ssh, git, http)
// into a browsable https URL, dropping credentials, ports, and the .git suffix.
// It never contacts a Git host; it only reshapes a value Mimir already stored.
export function normalizeRepositoryUrl(raw: string | null | undefined): string | null {
  const value = raw?.trim();
  if (!value) return null;
  const scp = /^[A-Za-z0-9._-]+@([^:/]+):(?!\/)(.+)$/.exec(value);
  const candidate = scp
    ? `https://${scp[1]}/${scp[2]}`
    : value.replace(/^ssh:\/\/(?:[^@/]+@)?/i, "https://").replace(/^git:\/\//i, "https://").replace(/^http:\/\//i, "https://");
  let url: URL;
  try {
    url = new URL(candidate);
  } catch {
    return null;
  }
  if (url.protocol !== "https:" || !url.hostname.includes(".")) return null;
  let path = url.pathname.replace(/\/+$/, "").replace(/\.git$/i, "");
  if (!path || path === "/") return null;
  if (url.hostname === "github.com" || url.hostname === "bitbucket.org") path = path.toLowerCase();
  return `https://${url.hostname}${path}`;
}

function commitSegment(hostname: string): string {
  if (/(^|\.)gitlab\./i.test(hostname)) return "/-/commit/";
  if (/(^|\.)bitbucket\./i.test(hostname)) return "/commits/";
  return "/commit/";
}

export function repositoryUrl(evidence: OutcomeEvidence | null): string | null {
  return normalizeRepositoryUrl(evidence?.repository_url);
}

export function externalEvidenceUrl(raw: string | null | undefined): string | null {
  if (!raw?.trim()) return null;
  try {
    const url = new URL(raw.trim());
    if (url.protocol !== "https:" && url.protocol !== "http:") return null;
    url.username = ""; url.password = "";
    return url.href;
  } catch { return null; }
}

// commitUrl prefers an explicitly recorded commit URL and otherwise derives one
// from the repository remote. Without a remote there is no reliable link, and
// the caller must show the bare SHA instead of guessing a host.
export function commitUrl(evidence: OutcomeEvidence | null): string | null {
  if (!evidence) return null;
  const explicit = externalEvidenceUrl(evidence.commit_url);
  if (explicit) return explicit;
  const repository = repositoryUrl(evidence);
  if (!repository || !evidence.commit) return null;
  return `${repository}${commitSegment(new URL(repository).hostname)}${evidence.commit}`;
}

export function shortCommit(sha: string | undefined): string {
  return sha ? sha.slice(0, 7) : "";
}

export function gitArtifactCommitUrl(artifact: GitArtifact): string | null {
  return commitUrl({ commit: artifact.commit_sha, repository_url: artifact.repository_url ?? undefined });
}

export function gitArtifactProvenance(provenance: string) {
  const normalized = provenance.trim().toLowerCase();
  if (normalized === "git" || normalized.includes("local") || normalized.includes("import")) {
    return { label: "Local checkout, unverified", unverified: true };
  }
  return { label: provenance || "Unknown source", unverified: false };
}

export function outcomeEvidenceArtifact(evidence: OutcomeEvidence, artifacts: GitArtifact[]): GitArtifact | null {
  const sha = evidence.commit?.trim().toLowerCase();
  if (!sha || sha.length < 7 || !/^[0-9a-f]+$/.test(sha)) return null;
  const repo = normalizeRepositoryUrl(evidence.repository_url);
  const candidates = artifacts.filter((artifact) => {
    const artifactRepo = normalizeRepositoryUrl(artifact.repository_url);
    return artifact.commit_sha.startsWith(sha) && !(repo && artifactRepo && repo !== artifactRepo);
  });
  return candidates.length === 1 ? candidates[0]! : null;
}

export function outcomeUrlMatchesArtifact(url: string | undefined, artifacts: GitArtifact[]): boolean {
  const normalized = url?.trim().replace(/\/+$/, "");
  if (!normalized) return false;
  return artifacts.some((artifact) => gitArtifactCommitUrl(artifact)?.replace(/\/+$/, "") === normalized);
}
