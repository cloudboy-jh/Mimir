const commands = [
  ["bun", "run", "dev:api"],
  ["bun", "run", "dev:web"],
] as const;

const children = commands.map((command) => Bun.spawn(command, {
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
