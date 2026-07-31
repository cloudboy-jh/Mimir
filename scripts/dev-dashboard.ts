const live = process.argv.includes("--live");
const commands = live
  ? [["bun", "run", "dev:api"], ["bun", "run", "dev:web"]]
  : [["bun", "run", "dev:web"]];

const children = commands.map((command) => Bun.spawn(command, {
  cwd: import.meta.dir + "/..",
  env: { ...process.env, VITE_MIMIR_DATA_SOURCE: live ? "live" : "fixtures" },
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
