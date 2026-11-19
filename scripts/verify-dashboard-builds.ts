const productionIndex = await Bun.file("worker/web/dist/index.html").text();
if (!productionIndex.includes('src="/assets/') || !productionIndex.includes('href="/assets/')) {
  throw new Error("production dashboard index does not reference compiled assets");
}

console.log("dashboard build verified");
