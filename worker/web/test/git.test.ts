import { describe, expect, it } from "vitest";
import { commitUrl, normalizeRepositoryUrl } from "../src/lib/git";

const sha = "a".repeat(40);

describe("commit links", () => {
  it("derives a browsable commit link from the recorded repository", () => {
    expect(commitUrl("git@github.com:owner/repo.git", sha)).toBe(`https://github.com/owner/repo/commit/${sha}`);
    expect(commitUrl("https://gitlab.example/Team/Repo.git", sha)).toBe(`https://gitlab.example/Team/Repo/-/commit/${sha}`);
    expect(commitUrl("https://bitbucket.org/Team/Repo", sha)).toBe(`https://bitbucket.org/team/repo/commits/${sha}`);
  });

  it("returns no link without a browsable remote", () => {
    expect(commitUrl(null, sha)).toBeNull();
    expect(commitUrl("/local/checkout", sha)).toBeNull();
    expect(commitUrl("javascript:alert(1)", sha)).toBeNull();
  });

  it("canonicalizes transport, credentials and host case without conflating case-sensitive GitLab paths", () => {
    expect(normalizeRepositoryUrl("ssh://git@GITHUB.COM:22/Owner/Repo.git/")).toBe("https://github.com/owner/repo");
    expect(normalizeRepositoryUrl("https://user:secret@github.com/owner/repo.git/?token=secret#fragment")).toBe("https://github.com/owner/repo");
    expect(normalizeRepositoryUrl("https://gitlab.example/Team/Repo.git")).toBe("https://gitlab.example/Team/Repo");
    expect(normalizeRepositoryUrl("/local/checkout")).toBeNull();
    expect(normalizeRepositoryUrl("javascript:alert(1)")).toBeNull();
  });
});
