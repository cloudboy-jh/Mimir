const productionRoot = "worker/web/dist";
const demoRoot = "internal/demoassets/static";

async function files(root: string) {
  const matches: string[] = [];
  for await (const path of new Bun.Glob("**/*").scan({ cwd: root, onlyFiles: true })) matches.push(path);
  return matches;
}

const production = await files(productionRoot);
const demo = await files(demoRoot);
if (production.some((path) => path.includes("dev-fixtures-"))) {
  throw new Error("production dashboard contains the fixture transport chunk");
}
if (!demo.some((path) => path.includes("dev-fixtures-"))) {
  throw new Error("demo dashboard is missing the fixture transport chunk");
}
const demoScripts = demo.filter((path) => path.endsWith(".js") && !path.includes("dev-fixtures-"));
const noticePresent = await Promise.all(demoScripts.map((path) => Bun.file(`${demoRoot}/${path}`).text())).then((contents) => contents.some((content) => content.includes("Sample data.")));
if (!noticePresent) throw new Error("demo dashboard is missing the sample-data notice");
const productionIndex = await Bun.file(`${productionRoot}/index.html`).text();
if (!productionIndex.includes('src="/assets/') || !productionIndex.includes('href="/assets/')) {
  throw new Error("production dashboard index does not reference compiled assets");
}

console.log("dashboard build modes verified");
