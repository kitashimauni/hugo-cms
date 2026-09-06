import assert from "node:assert/strict";
import { describe, it } from "node:test";

const localValues = new Map();
const sessionValues = new Map();
const storage = values => ({
    getItem(key) { return values.get(key) || null; },
    setItem(key, value) { values.set(key, value); },
    removeItem(key) { values.delete(key); },
});

globalThis.window = {
    localStorage: storage(localValues),
    sessionStorage: storage(sessionValues),
    crypto: { randomUUID: () => "browser-generated-uuid" },
    location: {
        origin: "http://localhost:8080",
    },
};

const {
    normalizeDeploymentState,
    safeExternalURL,
} = await import("./ui.js");
const { createDraftUUID, createLocalPreviewSessionID, getOrCreateDraftID } = await import("./editor.js");
const API = await import("./api.js");

describe("safeExternalURL", () => {
    it("accepts only absolute HTTP(S) links", () => {
        assert.equal(safeExternalURL("https://preview.example.test/build/1"), "https://preview.example.test/build/1");
        assert.equal(safeExternalURL("javascript:alert(1)"), "");
        assert.equal(safeExternalURL("/admin"), "");
        assert.equal(safeExternalURL("//evil.example.test"), "");
    });
});

describe("normalizeDeploymentState", () => {
    it("normalizes provider field aliases and safe links", () => {
        assert.deepEqual(normalizeDeploymentState({
            state: "READY",
            commit: "0123456789abcdef",
            deployment_url: "https://preview.example.test/commit",
            log_url: "javascript:alert(1)",
        }), {
            state: "READY",
            commit: "0123456789abcdef",
            deployment_url: "https://preview.example.test/commit",
            log_url: "",
            status: "ready",
            commit_sha: "0123456789abcdef",
            url: "https://preview.example.test/commit",
        });
    });

    it("does not treat an unknown state as ready", () => {
        assert.equal(normalizeDeploymentState({ status: "unexpected" }).status, "queued");
        assert.equal(normalizeDeploymentState({ status: "stale", url: "https://old.example.test" }).status, "stale");
        assert.equal(normalizeDeploymentState(null), null);
    });
});

describe("draft IDs", () => {
    it("uses getRandomValues for a UUID v4 when randomUUID is unavailable", () => {
        let called = false;
        const cryptoWithoutRandomUUID = {
            getRandomValues(bytes) {
                called = true;
                bytes.forEach((_, index) => { bytes[index] = index; });
                return bytes;
            },
        };

        const draftID = createDraftUUID(cryptoWithoutRandomUUID);

        assert.equal(called, true);
        assert.equal(draftID, "00010203-0405-4607-8809-0a0b0c0d0e0f");
        assert.match(draftID, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    });

    it("fails closed when no cryptographic random source is available", () => {
        assert.throws(
            () => createDraftUUID({}),
            /Secure random number generation is unavailable/,
        );
    });

    it("persists one UUID per site and article for the browser session", () => {
        const values = new Map();
        const memoryStorage = storage(values);
        let sequence = 0;
        const createUUID = () => `uuid-${++sequence}`;

        assert.equal(getOrCreateDraftID("docs", "posts/one.md", memoryStorage, createUUID), "uuid-1");
        assert.equal(getOrCreateDraftID("docs", "posts/one.md", memoryStorage, createUUID), "uuid-1");
        assert.equal(getOrCreateDraftID("docs", "posts/two.md", memoryStorage, createUUID), "uuid-2");
        assert.equal(getOrCreateDraftID("blog", "posts/one.md", memoryStorage, createUUID), "uuid-3");
    });

    it("creates Local Preview ownership IDs without browser storage persistence", () => {
        let sequence = 0;
        const createUUID = () => `local-${++sequence}`;

        assert.equal(createLocalPreviewSessionID(createUUID), "local-1");
        assert.equal(createLocalPreviewSessionID(createUUID), "local-2");
        assert.equal(sessionValues.size, 0);
    });
});

describe("preview API contracts", () => {
    it("scopes Markdown, local, and deployment operations to the selected site", async () => {
        const calls = [];
        globalThis.fetch = async (url, options = {}) => {
            calls.push({ url, options });
            if (url === "/admin/api/csrf-token") {
                return { ok: true, status: 200, json: async () => ({ csrf_token: "csrf" }) };
            }
            return { ok: true, status: 200, json: async () => ({ status: "queued", html: "<p>safe</p>" }) };
        };

        API.setCurrentSite("docs site");
        const article = { path: "posts/one.md", body: "# Draft", frontmatter: { title: "Draft" } };
        await API.renderMarkdownPreview(article);
        await API.updateLocalPreviewContent(article, "local-session", 7);
        await API.releaseLocalPreviewContent("local-session");
        await API.triggerPreviewDeployment(article.path, "draft/id");
        await API.fetchPreviewDeployment("draft/id");
        await API.retryPreviewDeployment("draft/id");
        await API.discardPreviewDeployment("draft/id");
        await API.runPublish(article.path, "draft/id");

        assert.equal(calls[1].url, "/admin/api/preview/markdown?site=docs+site");
        assert.deepEqual(JSON.parse(calls[1].options.body), article);
        assert.equal(calls[2].url, "/admin/api/preview/local?site=docs+site");
        assert.deepEqual(JSON.parse(calls[2].options.body), { ...article, draft_id: "local-session", revision: 7 });
        assert.equal(calls[3].url, "/admin/api/preview/local/release?site=docs+site");
        assert.deepEqual(JSON.parse(calls[3].options.body), { draft_id: "local-session" });
        assert.equal(calls[4].url, "/admin/api/preview/deployments?site=docs+site");
        assert.deepEqual(JSON.parse(calls[4].options.body), { path: article.path, draft_id: "draft/id" });
        assert.equal(calls[5].url, "/admin/api/preview/deployments/draft%2Fid?site=docs+site");
        assert.equal(calls[6].url, "/admin/api/preview/deployments/draft%2Fid/retry?site=docs+site");
        assert.equal(calls[7].url, "/admin/api/preview/deployments/draft%2Fid/discard?site=docs+site");
        assert.deepEqual(JSON.parse(calls[8].options.body), { path: article.path, draft_id: "draft/id" });
        calls.slice(1).forEach(call => {
            assert.equal(call.options.headers["X-CMS-Site"], "docs site");
        });
    });

    it("preserves conflict status from local preview API errors", async () => {
        globalThis.fetch = async (url) => {
            if (url === "/admin/api/csrf-token") {
                return { ok: true, status: 200, json: async () => ({ csrf_token: "csrf" }) };
            }
            return {
                ok: false,
                status: 409,
                json: async () => ({ message: "another local preview session is already active for this site" }),
            };
        };

        await assert.rejects(
            () => API.updateLocalPreviewContent({ path: "one.md", content: "draft" }, "local-session", 1),
            error => error.status === 409 && /another local preview session/.test(error.message),
        );
    });
});