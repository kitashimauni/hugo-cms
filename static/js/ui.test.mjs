import assert from "node:assert/strict";
import { describe, it } from "node:test";

globalThis.window = {
    localStorage: {
        getItem() { return ""; },
        setItem() {},
        removeItem() {},
    },
    location: {
        origin: "http://localhost:8080",
    },
};

const {
    addCacheBuster,
    previewUrlFromFrontMatter,
    previewUrlFromPath,
} = await import("./ui.js");

describe("previewUrlFromPath", () => {
    it("converts regular content files to trailing-slash preview paths", () => {
        assert.equal(previewUrlFromPath("posts/hello.md"), "/posts/hello/");
    });

    it("collapses leaf bundle index files to their bundle URL", () => {
        assert.equal(previewUrlFromPath("posts/hello/index.md"), "/posts/hello/");
    });

    it("maps root index files to the site root", () => {
        assert.equal(previewUrlFromPath("index.md"), "/");
        assert.equal(previewUrlFromPath("_index.md"), "/");
    });
});

describe("previewUrlFromFrontMatter", () => {
    const config = { preview: { url_field: "permalink" } };

    it("uses a root-relative permalink field when configured", () => {
        assert.equal(
            previewUrlFromFrontMatter(config, { permalink: "/blog/hello/" }),
            "/blog/hello/",
        );
    });

    it("normalizes relative permalink values to root-relative paths", () => {
        assert.equal(
            previewUrlFromFrontMatter(config, { permalink: "blog/hello/" }),
            "/blog/hello/",
        );
    });

    it("keeps the path, query, and hash from absolute http URLs", () => {
        assert.equal(
            previewUrlFromFrontMatter(config, { permalink: "https://example.com/blog/hello/?draft=1#top" }),
            "/blog/hello/?draft=1#top",
        );
    });

    it("falls back when the configured field is missing or empty", () => {
        assert.equal(previewUrlFromFrontMatter(config, {}), "");
        assert.equal(previewUrlFromFrontMatter(config, { permalink: "" }), "");
    });

    it("rejects non-http schemes and protocol-relative URLs", () => {
        assert.equal(previewUrlFromFrontMatter(config, { permalink: "javascript:alert(1)" }), "");
        assert.equal(previewUrlFromFrontMatter(config, { permalink: "//example.com/blog/" }), "");
    });
});

describe("addCacheBuster", () => {
    it("adds the cache buster before a hash fragment", () => {
        assert.equal(addCacheBuster("/blog/hello/#top", 123), "/blog/hello/?t=123#top");
    });

    it("uses an ampersand when the URL already has a query string", () => {
        assert.equal(addCacheBuster("/blog/hello/?draft=1#top", 123), "/blog/hello/?draft=1&t=123#top");
    });
});
