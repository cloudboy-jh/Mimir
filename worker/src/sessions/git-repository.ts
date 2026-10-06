// Recorded remotes only. This never opens a remote source connection.
export function normalizeRepositoryUrl(raw: string | null | undefined): string | null {
  const value = raw?.trim();
  if (!value) return null;
  const scp = /^[A-Za-z0-9._-]+@([^:/]+):(?!\/)(.+)$/.exec(value);
  const candidate = scp ? `https://${scp[1]}/${scp[2]}` : value.replace(/^ssh:\/\/(?:[^@/]+@)?/i, "https://").replace(/^git:\/\//i, "https://").replace(/^http:\/\//i, "https://");
  let url: URL;
  try { url = new URL(candidate); } catch { return null; }
  if (url.protocol !== "https:" || !url.hostname.includes(".")) return null;
  let path = url.pathname.replace(/\/+$/, "").replace(/\.git$/i, "");
  if (!path || path === "/") return null;
  if (url.hostname === "github.com" || url.hostname === "bitbucket.org") path = path.toLowerCase();
  return `https://${url.hostname}${path}`;
}
