const root = "internal/demoassets/static";
const build = Bun.spawn(["bun", "run", "build:demo"], { stdout: "inherit", stderr: "inherit" });
if (await build.exited !== 0) process.exit(build.exitCode ?? 1);

const status = Bun.spawn(["git", "status", "--short", "--untracked-files=all", "--", root], { stdout: "pipe", stderr: "inherit" });
const changed = await new Response(status.stdout).text();
if (await status.exited !== 0) process.exit(status.exitCode ?? 1);
if (changed.trim()) {
  console.error("demo assets changed after rebuilding:");
  console.error(changed.trimEnd());
  console.error("demo assets are stale; run bun run build:assets and commit the result");
  process.exit(1);
}

console.log("demo assets are current");
