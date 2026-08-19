import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";

const root = resolve(import.meta.dirname, "..");
const source = resolve(root, "assets/images/mimir-system-map-source.svg");
const output = resolve(root, "assets/images/mimir-system-map.png");
const candidates = [
  process.env.CHROME_PATH,
  process.platform === "win32" && `${process.env.ProgramFiles ?? "C:/Program Files"}/Google/Chrome/Application/chrome.exe`,
  process.platform === "win32" && `${process.env["ProgramFiles(x86)"] ?? "C:/Program Files (x86)"}/Microsoft/Edge/Application/msedge.exe`,
  process.platform === "darwin" && "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  "google-chrome",
  "chromium",
  "chromium-browser",
].filter(Boolean);

const browser = candidates.find((candidate) => candidate.includes("/") || candidate.includes("\\") ? existsSync(candidate) : spawnSync(candidate, ["--version"]).status === 0);
if (!browser) throw new Error("Chrome, Edge, or Chromium is required to render the system map");

const result = spawnSync(browser, [
  "--headless=new",
  "--disable-gpu",
  "--hide-scrollbars",
  "--force-device-scale-factor=1",
  "--window-size=1400,860",
  `--screenshot=${output}`,
  pathToFileURL(source).href,
], { stdio: "inherit" });
if (result.status !== 0 || !existsSync(output)) process.exit(result.status ?? 1);
