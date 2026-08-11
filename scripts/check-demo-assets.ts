const root = "internal/demoassets/static";
const build = Bun.spawn(["bun", "run", "build:demo"], { stdout: "inherit", stderr: "inherit" });
if (await build.exited !== 0) process.exit(build.exitCode ?? 1);

const diff = Bun.spawn(["git", "diff", "--exit-code", "--", root], { stdout: "inherit", stderr: "inherit" });
if (await diff.exited !== 0) process.exit(diff.exitCode ?? 1);

const untracked = Bun.spawn(["git", "ls-files", "--others", "--exclude-standard", "--", root], { stdout: "pipe", stderr: "inherit" });
const changed = await new Response(untracked.stdout).text();
if (await untracked.exited !== 0) process.exit(untracked.exitCode ?? 1);
if (changed.trim()) {
  console.error(changed.trimEnd());
  console.error("demo assets are stale; run bun run build:demo and commit the result");
  process.exit(1);
}

console.log("demo assets are current");
