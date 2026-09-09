import outputAssetPlugin from "./plugins/output-asset.js";

export const config = {
  dir: {
    input: "src",
    output: "public",
  },
};

export default function configure(eleventyConfig) {
  if (process.env.ELEVENTY_FIXTURE_SLOW_BUILD === "1") {
    eleventyConfig.on("eleventy.before", async () => {
      await new Promise((resolve) => setTimeout(resolve, 1000));
    });
  }
  eleventyConfig.addPlugin(outputAssetPlugin);
  eleventyConfig.addCollection("posts", (collectionApi) => (
    collectionApi.getFilteredByGlob("src/posts/**/*.{md,html,njk}")
  ));
  eleventyConfig.addPassthroughCopy("src/images");
}
