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

// commitUrl derives a browsable commit link from the recorded remote. Without
// a remote there is no reliable link, and the caller shows the bare SHA.
export function commitUrl(repositoryUrl: string | null | undefined, sha: string): string | null {
  const repository = normalizeRepositoryUrl(repositoryUrl);
  if (!repository) return null;
  const host = new URL(repository).hostname;
  const segment = /(^|\.)gitlab\./i.test(host) ? "/-/commit/" : /(^|\.)bitbucket\./i.test(host) ? "/commits/" : "/commit/";
  return `${repository}${segment}${sha}`;
}
