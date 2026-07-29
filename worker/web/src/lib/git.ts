import type { OutcomeEvidence } from "@/lib/api";

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
  const path = url.pathname.replace(/\.git$/i, "").replace(/\/+$/, "");
  if (!path || path === "/") return null;
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

// commitUrl prefers an explicitly recorded commit URL and otherwise derives one
// from the repository remote. Without a remote there is no reliable link, and
// the caller must show the bare SHA instead of guessing a host.
export function commitUrl(evidence: OutcomeEvidence | null): string | null {
  if (!evidence) return null;
  const explicit = evidence.commit_url?.trim();
  if (explicit && /^https:\/\/[^\s]+$/i.test(explicit)) return explicit;
  const repository = repositoryUrl(evidence);
  if (!repository || !evidence.commit) return null;
  return `${repository}${commitSegment(new URL(repository).hostname)}${evidence.commit}`;
}

export function shortCommit(sha: string | undefined): string {
  return sha ? sha.slice(0, 7) : "";
}
