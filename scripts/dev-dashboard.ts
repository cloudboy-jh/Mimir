// Runs the local Worker (wrangler dev on 8787, local D1) and the Vite dashboard,
// which proxies /dashboard/api and /dashboard/auth to it.
const children = [["bun", "run", "dev:api"], ["bun", "run", "dev:web"]].map((command) => Bun.spawn(command, {
  cwd: import.meta.dir + "/..",
  stdin: "inherit",
  stdout: "inherit",
  stderr: "inherit",
}));

function stop() {
  for (const child of children) child.kill();
}

process.on("SIGINT", stop);
process.on("SIGTERM", stop);

const exitCode = await Promise.race(children.map((child) => child.exited));
stop();
process.exit(exitCode);
