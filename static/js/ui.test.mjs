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
    normalizeLocalPreviewState,
    safeExternalURL,
    switchView,
} = await import("./ui.js");
const {
    createLocalPreviewFrameController,
    LOCAL_PREVIEW_INITIAL_NAVIGATION_MAX_ATTEMPTS,
    localPreviewNavigationRetryDelay,
    shouldAutoShowEmbeddedLocalPreview,
    shouldCloseEmbeddedLocalPreview,
    shouldRetryLocalPreviewNavigation,
    shouldUseLocalPreviewSplitDefault,
} = await import("./local_preview.js");
const {
    createDraftUUID,
    createLocalPreviewSessionID,
    getOrCreateDraftID,
    waitForLocalPreviewUpdates,
} = await import("./editor.js");
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

describe("normalizeLocalPreviewState", () => {
    it("keeps an active session owned by another tab in conflict", () => {
        const state = normalizeLocalPreviewState({
            status: "ready",
            process_state: "ready",
            session_active: true,
            session_owned: false,
            session_stale: false,
        });

        assert.equal(state.status, "conflict");
    });

    it("does not replace the stale recovery state", () => {
        const state = normalizeLocalPreviewState({
            status: "stale",
            session_active: true,
            session_owned: false,
            session_stale: true,
        });

        assert.equal(state.status, "stale");
    });
});

function createPreviewFrameHarness() {
    const makeClassList = (...initial) => {
        const values = new Set(initial);
        return {
            add(value) { values.add(value); },
            remove(value) { values.delete(value); },
            contains(value) { return values.has(value); },
        };
    };
    const wrapper = { classList: makeClassList("hidden") };
    const frame = {
        src: "about:blank",
        getAttribute(name) { return name === "src" ? this.src : null; },
    };
    const button = { textContent: "埋め込み表示" };
    const loading = { classList: makeClassList("hidden") };
    const error = { classList: makeClassList("hidden") };
    const errorMessage = { textContent: "" };
    let timerCallback = null;
    const controller = createLocalPreviewFrameController({
        getURL: () => "https://preview.example.test/",
        wrapper,
        frame,
        button,
        loading,
        error,
        errorMessage,
        setTimeoutFn(callback) {
            timerCallback = callback;
            return "preview-timer";
        },
        clearTimeoutFn() {
            timerCallback = null;
        },
    });
    return { controller, wrapper, frame, button, loading, error, errorMessage, triggerTimeout: () => timerCallback?.() };
}

describe("embedded Local Preview state transitions", () => {
    it("closes the embed for conflict, stale, or missing article state", () => {
        assert.equal(shouldCloseEmbeddedLocalPreview({ status: "conflict", hasCurrentPath: true }), true);
        assert.equal(shouldCloseEmbeddedLocalPreview({ status: "stale", hasCurrentPath: true }), true);
        assert.equal(shouldCloseEmbeddedLocalPreview({ status: "ready", hasCurrentPath: false }), true);
        assert.equal(shouldCloseEmbeddedLocalPreview({ status: "ready", hasCurrentPath: true }), false);
    });

    it("only auto-shows an owned preview for an active article", () => {
        assert.equal(shouldAutoShowEmbeddedLocalPreview({ status: "starting", sessionOwned: true, hasCurrentPath: true, dismissed: false }), true);
        assert.equal(shouldAutoShowEmbeddedLocalPreview({ status: "conflict", sessionOwned: true, hasCurrentPath: true, dismissed: false }), false);
        assert.equal(shouldAutoShowEmbeddedLocalPreview({ status: "ready", sessionOwned: false, hasCurrentPath: true, dismissed: false }), false);
        assert.equal(shouldAutoShowEmbeddedLocalPreview({ status: "ready", sessionOwned: true, hasCurrentPath: true, dismissed: true }), false);
    });

    it("retries transient URL resolution failures with bounded backoff", () => {
        assert.equal(LOCAL_PREVIEW_INITIAL_NAVIGATION_MAX_ATTEMPTS, 3);
        assert.equal(shouldRetryLocalPreviewNavigation({ error: new TypeError("network"), attempt: 1 }), true);
        assert.equal(shouldRetryLocalPreviewNavigation({ error: { status: 503 }, attempt: 2 }), true);
        assert.equal(shouldRetryLocalPreviewNavigation({ error: { status: 409 }, attempt: 1 }), false);
        assert.equal(shouldRetryLocalPreviewNavigation({ error: { status: 400 }, attempt: 1 }), false);
        assert.equal(shouldRetryLocalPreviewNavigation({ error: { status: 503 }, attempt: 3 }), false);
        assert.equal(localPreviewNavigationRetryDelay(1), 250);
        assert.equal(localPreviewNavigationRetryDelay(2), 750);
    });

    it("uses Split as the desktop default but keeps Edit on narrow viewports", () => {
        assert.equal(shouldUseLocalPreviewSplitDefault({ enabled: true, narrowViewport: false }), true);
        assert.equal(shouldUseLocalPreviewSplitDefault({ enabled: true, narrowViewport: true }), false);
        assert.equal(shouldUseLocalPreviewSplitDefault({ enabled: false, narrowViewport: false }), false);
    });

    it("shows the iframe in loading state and clears it after a ready event", () => {
        const harness = createPreviewFrameHarness();

        assert.equal(harness.controller.show(), true);
        assert.equal(harness.controller.isVisible(), true);
        assert.equal(harness.frame.src, "https://preview.example.test/");
        assert.equal(harness.loading.classList.contains("hidden"), false);
        assert.equal(harness.button.textContent, "埋め込みを閉じる");

        harness.controller.handleReady();
        assert.equal(harness.loading.classList.contains("hidden"), true);
        assert.equal(harness.error.classList.contains("hidden"), true);
    });

    it("shows a best-effort fallback state when the iframe does not respond", () => {
        const harness = createPreviewFrameHarness();

        harness.controller.show();
        harness.triggerTimeout();

        assert.equal(harness.error.classList.contains("hidden"), false);
        assert.match(harness.errorMessage.textContent, /確認できません/);
    });

    it("keeps dismissal separate from manual reopening", () => {
        const harness = createPreviewFrameHarness();

        harness.controller.show();
        harness.controller.close({ dismiss: true });
        assert.equal(harness.controller.isDismissed(), true);
        assert.equal(harness.controller.isVisible(), false);
        assert.equal(harness.frame.src, "about:blank");

        harness.controller.resetDismissed();
        harness.controller.show({ reload: true });
        assert.equal(harness.controller.isDismissed(), false);
        assert.equal(harness.controller.isVisible(), true);
    });
});

function createViewHarness({ localPreviewEnabled = false } = {}) {
    const makeClassList = (...initial) => {
        const values = new Set(initial);
        return {
            add(value) { values.add(value); },
            remove(...items) { items.forEach(value => values.delete(value)); },
            contains(value) { return values.has(value); },
        };
    };
    const makeElement = id => ({
        id,
        classList: makeClassList(),
        dataset: {},
        style: { display: "" },
    });
    const contentArea = makeElement("content-area");
    if (localPreviewEnabled) contentArea.classList.add("local-preview-enabled");
    const elements = new Map([
        ["content-area", contentArea],
        ["edit-view", makeElement("edit-view")],
        ["preview-view", makeElement("preview-view")],
        ["local-preview-view", makeElement("local-preview-view")],
        ["btn-view-edit", makeElement("btn-view-edit")],
        ["btn-view-preview", makeElement("btn-view-preview")],
        ["btn-view-split", makeElement("btn-view-split")],
    ]);
    const previousDocument = globalThis.document;
    globalThis.document = {
        getElementById(id) { return elements.get(id); },
        querySelectorAll(selector) {
            return selector === ".view-toggle"
                ? [elements.get("btn-view-edit"), elements.get("btn-view-preview"), elements.get("btn-view-split")]
                : [];
        },
    };
    return {
        contentArea,
        editView: elements.get("edit-view"),
        previewView: elements.get("preview-view"),
        localPreviewView: elements.get("local-preview-view"),
        restore() { globalThis.document = previousDocument; },
    };
}

describe("view surface integration", () => {
    it("uses Local Live Preview for Preview and Split when enabled", () => {
        const harness = createViewHarness({ localPreviewEnabled: true });
        try {
            switchView("preview");
            assert.equal(harness.localPreviewView.style.display, "flex");
            assert.equal(harness.previewView.style.display, "none");
            assert.equal(harness.editView.style.display, "none");

            switchView("split");
            assert.equal(harness.contentArea.classList.contains("split-mode"), true);
            assert.equal(harness.editView.style.display, "flex");
            assert.equal(harness.localPreviewView.style.display, "flex");
            assert.equal(harness.previewView.style.display, "none");

            switchView("edit");
            assert.equal(harness.contentArea.classList.contains("split-mode"), false);
            assert.equal(harness.editView.style.display, "flex");
            assert.equal(harness.localPreviewView.style.display, "none");
        } finally {
            harness.restore();
        }
    });

    it("keeps the Markdown surface as Preview when Local Live Preview is disabled", () => {
        const harness = createViewHarness();
        try {
            switchView("preview");
            assert.equal(harness.previewView.style.display, "block");
            assert.equal(harness.localPreviewView.style.display, "none");
        } finally {
            harness.restore();
        }
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

describe("Local Preview destructive operations", () => {
    it("waits for in-flight updates before deleting an article", async () => {
        let updateApplied = false;
        let resolveUpdate;
        const update = new Promise(resolve => {
            resolveUpdate = () => {
                updateApplied = true;
                resolve();
            };
        });

        const waiting = waitForLocalPreviewUpdates(new Set([update]));
        await Promise.resolve();
        assert.equal(updateApplied, false);

        resolveUpdate();
        await waiting;
        assert.equal(updateApplied, true);
    });
});

describe("preview API contracts", () => {
    it("scopes Markdown, local lifecycle, and deployment operations to the selected site", async () => {
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
        await API.resolveLocalPreviewArticleURL("local-session", article.path);
        await API.releaseLocalPreviewContent("local-session");
        await API.fetchLocalPreviewStatus("local-session");
        await API.heartbeatLocalPreviewContent("local-session");
        await API.stopLocalPreviewContent("local-session");
        await API.reclaimStaleLocalPreview();
        await API.triggerPreviewDeployment(article.path, "draft/id");
        await API.fetchPreviewDeployment("draft/id");
        await API.retryPreviewDeployment("draft/id");
        await API.discardPreviewDeployment("draft/id");
        await API.runPublish(article.path, "draft/id");

        assert.equal(calls[1].url, "/admin/api/preview/markdown?site=docs+site");
        assert.deepEqual(JSON.parse(calls[1].options.body), article);
        assert.equal(calls[2].url, "/admin/api/preview/local?site=docs+site");
        assert.deepEqual(JSON.parse(calls[2].options.body), { ...article, draft_id: "local-session", revision: 7 });
        assert.equal(calls[3].url, "/admin/api/preview/local/navigate?site=docs+site");
        assert.deepEqual(JSON.parse(calls[3].options.body), { draft_id: "local-session", path: article.path });
        assert.equal(calls[4].url, "/admin/api/preview/local/release?site=docs+site");
        assert.deepEqual(JSON.parse(calls[4].options.body), { draft_id: "local-session" });
        assert.equal(calls[5].url, "/admin/api/preview/local/status?draft_id=local-session&site=docs+site");
        assert.equal(calls[6].url, "/admin/api/preview/local/heartbeat?site=docs+site");
        assert.deepEqual(JSON.parse(calls[6].options.body), { draft_id: "local-session" });
        assert.equal(calls[7].url, "/admin/api/preview/local/stop?site=docs+site");
        assert.deepEqual(JSON.parse(calls[7].options.body), { draft_id: "local-session" });
        assert.equal(calls[8].url, "/admin/api/preview/local/reclaim?site=docs+site");
        assert.equal(calls[9].url, "/admin/api/preview/deployments?site=docs+site");
        assert.deepEqual(JSON.parse(calls[9].options.body), { path: article.path, draft_id: "draft/id" });
        assert.equal(calls[10].url, "/admin/api/preview/deployments/draft%2Fid?site=docs+site");
        assert.equal(calls[11].url, "/admin/api/preview/deployments/draft%2Fid/retry?site=docs+site");
        assert.equal(calls[12].url, "/admin/api/preview/deployments/draft%2Fid/discard?site=docs+site");
        assert.deepEqual(JSON.parse(calls[13].options.body), { path: article.path, draft_id: "draft/id" });
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
