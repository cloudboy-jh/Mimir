const productionRoot = "worker/web/dist";
const demoRoot = "internal/demoassets/static";

async function files(root: string) {
  const matches: string[] = [];
  for await (const path of new Bun.Glob("**/*").scan({ cwd: root, onlyFiles: true })) matches.push(path);
  return matches;
}

const production = await files(productionRoot);
const demo = await files(demoRoot);
const fixtureMarker = "dev_fixture_macbook";
const productionScripts = await Promise.all(
  production.filter((path) => path.endsWith(".js")).map((path) => Bun.file(`${productionRoot}/${path}`).text()),
);
const demoScripts = await Promise.all(
  demo.filter((path) => path.endsWith(".js")).map((path) => Bun.file(`${demoRoot}/${path}`).text()),
);
if (productionScripts.some((content) => content.includes(fixtureMarker))) {
  throw new Error("production dashboard contains fixture data");
}
if (!demoScripts.some((content) => content.includes(fixtureMarker))) {
  throw new Error("demo dashboard is missing fixture data");
}
if (!demoScripts.some((content) => content.includes("Sample data."))) {
  throw new Error("demo dashboard is missing the sample-data notice");
}
const productionIndex = await Bun.file(`${productionRoot}/index.html`).text();
if (!productionIndex.includes('src="/assets/') || !productionIndex.includes('href="/assets/')) {
  throw new Error("production dashboard index does not reference compiled assets");
}

console.log("dashboard build modes verified");
