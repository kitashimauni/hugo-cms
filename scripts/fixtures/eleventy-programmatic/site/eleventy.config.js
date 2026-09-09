import outputAssetPlugin from "./plugins/output-asset.js";

export const config = {
  dir: {
    input: "src",
    output: "public",
  },
};

export default function configure(eleventyConfig) {
  eleventyConfig.addPlugin(outputAssetPlugin);
  eleventyConfig.addCollection("posts", (collectionApi) => (
    collectionApi.getFilteredByGlob("src/posts/**/*.{md,html,njk}")
  ));
  eleventyConfig.addPassthroughCopy("src/images");
}
