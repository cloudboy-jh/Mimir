import { fileURLToPath } from "node:url";

const workerRoot = fileURLToPath(new URL("..", import.meta.url));
const webRoot = `${workerRoot}/web`;
const outputRoot = `${webRoot}/dist`;
const build = Bun.spawn(["bunx", "vite", "build"], {
  cwd: webRoot,
  stdout: "inherit",
  stderr: "inherit",
});
if (await build.exited !== 0) process.exit(build.exitCode ?? 1);

const generated = new Bun.Glob("**/*");
for await (const path of generated.scan({ cwd: outputRoot, onlyFiles: true })) {
  if (![".html", ".js", ".css", ".svg"].some((extension) => path.endsWith(extension))) continue;
  const file = Bun.file(`${outputRoot}/${path}`);
  const normalized = (await file.text())
    .replace(/\r/g, "")
    .replace(/[\t ]+\n/g, "\n")
    .replace(/\n+$/g, "\n");
  await Bun.write(file, normalized);
}
