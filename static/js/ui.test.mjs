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
    clearEditor,
    execAutoSave,
    finishForGitSync,
    flushLocalPreviewBeforeArticleSwitch,
    flushPendingSave,
    getOrCreateDraftID,
    getCurrentPath,
    initAutoSave,
    isGitSyncInProgress,
    loadFile,
    prepareForGitSync,
    refreshLocalLivePreview,
    runGitMutation,
    setConfig,
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
    it("keeps the site runtime state without browser ownership fields", () => {
        const state = normalizeLocalPreviewState({
            status: "ready",
            process_state: "ready",
            workspace_active: true,
        });

        assert.equal(state.status, "ready");
        assert.equal(state.workspace_active, true);
        assert.equal("session_owned" in state, false);
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
    it("closes the embed for missing article state", () => {
        assert.equal(shouldCloseEmbeddedLocalPreview({ status: "ready", hasCurrentPath: false }), true);
        assert.equal(shouldCloseEmbeddedLocalPreview({ status: "ready", hasCurrentPath: true }), false);
    });

    it("auto-shows a ready runtime for an active article", () => {
        assert.equal(shouldAutoShowEmbeddedLocalPreview({ status: "starting", hasCurrentPath: true, dismissed: false }), true);
        assert.equal(shouldAutoShowEmbeddedLocalPreview({ status: "ready", hasCurrentPath: true, dismissed: false }), true);
        assert.equal(shouldAutoShowEmbeddedLocalPreview({ status: "stopped", hasCurrentPath: true, dismissed: false }), false);
        assert.equal(shouldAutoShowEmbeddedLocalPreview({ status: "ready", hasCurrentPath: true, dismissed: true }), false);
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

    it("flushes the latest preview payload before switching articles", async () => {
        let oldUpdateApplied = false;
        let resolveOldUpdate;
        const oldUpdate = new Promise(resolve => {
            resolveOldUpdate = () => {
                oldUpdateApplied = true;
                resolve();
            };
        });
        const pending = new Set([oldUpdate]);
        const events = [];
        const switching = flushLocalPreviewBeforeArticleSwitch(async () => {
            assert.equal(oldUpdateApplied, true);
            events.push("latest payload sent");
            const latestUpdate = Promise.resolve().then(() => events.push("latest payload applied"));
            pending.add(latestUpdate);
        }, pending);

        await Promise.resolve();
        assert.deepEqual(events, []);
        resolveOldUpdate();
        await switching;
        assert.deepEqual(events, ["latest payload sent", "latest payload applied"]);
    });

    it("does not block a switch when an in-flight preview update rejects", async () => {
        let rejectUpdate;
        const pending = new Set([new Promise((_, reject) => { rejectUpdate = reject; })]);
        const switching = flushLocalPreviewBeforeArticleSwitch(async () => "latest payload sent", pending);

        rejectUpdate(new Error("preview update failed"));
        await switching;
        assert.equal(getCurrentPath(), "");
    });

    function createArticleSwitchHarness({ generator = "hugo", previewFailure = null, saveFailure = false, saveResponse = null, articleResponses = new Map(), markdownResponses = new Map(), localPreviewResponses = new Map() } = {}) {
        const previousDocument = globalThis.document;
        const previousFetch = globalThis.fetch;
        const previousRequestAnimationFrame = globalThis.requestAnimationFrame;
        const editor = { disabled: false, value: "", placeholder: "" };
        const frontMatterControl = { disabled: false };
        const fmContainer = {
            style: { display: "" },
            innerHTML: "",
            querySelectorAll() { return [frontMatterControl]; },
        };
        const markdownPreview = { replaceChildren() {}, innerHTML: "" };
        const markdownStatus = {
            textContent: "",
            className: "",
            removeAttribute() {},
            setAttribute() {},
        };
        const filename = { textContent: "" };
        const makeToastElement = () => ({
            id: "",
            className: "",
            textContent: "",
            innerHTML: "",
            style: {},
            parentElement: null,
            appendChild(child) {
                child.parentElement = this;
            },
            remove() {
                this.parentElement = null;
            },
        });
        const body = makeToastElement();
        const elements = new Map([
            ["editor", editor],
            ["fm-container", fmContainer],
            ["filename-display", filename],
            ["markdown-preview", markdownPreview],
            ["markdown-preview-status", markdownStatus],
        ]);
        const calls = [];

        globalThis.document = {
            getElementById(id) { return elements.get(id) || null; },
            querySelectorAll() { return []; },
            createElement: makeToastElement,
            body,
        };
        globalThis.requestAnimationFrame = callback => {
            callback();
            return 1;
        };
        globalThis.fetch = async (url, options = {}) => {
            const requestURL = String(url);
            calls.push({ url: requestURL, options });
            if (requestURL === "/admin/api/csrf-token") {
                return { ok: true, status: 200, json: async () => ({ csrf_token: "csrf" }) };
            }
            if (requestURL.includes("/admin/api/article?") && !options.method) {
                const path = new URL(requestURL, "http://localhost").searchParams.get("path");
                const responseOverride = articleResponses.get(path);
                if (responseOverride) return responseOverride();
                return {
                    ok: true,
                    status: 200,
                    json: async () => ({ path, content: path === "posts/old.md" ? "before" : "after" }),
                };
            }
            if (requestURL.includes("/admin/api/preview/markdown")) {
                const markdownPath = JSON.parse(options.body || "{}").path;
                const responseOverride = markdownResponses.get(markdownPath);
                if (responseOverride) return responseOverride();
                return { ok: true, status: 200, json: async () => ({ html: `<p>${markdownPath}</p>` }) };
            }
            if (requestURL.includes("/admin/api/preview/local") && options.method === "POST") {
                const localPreviewPath = JSON.parse(options.body || "{}").path;
                const responseOverride = localPreviewResponses.get(localPreviewPath);
                if (responseOverride) return responseOverride();
                if (previewFailure === "network") {
                    throw new Error("preview network failed");
                }
                if (previewFailure) {
                    return {
                        ok: false,
                        status: previewFailure,
                        json: async () => ({ message: `preview failed with ${previewFailure}` }),
                    };
                }
                return { ok: true, status: 200, json: async () => ({ status: "ok" }) };
            }
            if (requestURL.endsWith("/admin/api/article") && options.method === "POST") {
                if (saveResponse) return saveResponse();
                if (saveFailure) {
                    return { ok: false, status: 500, json: async () => ({}) };
                }
                return { ok: true, status: 200, json: async () => ({ status: "ok" }) };
            }
            throw new Error(`Unexpected request: ${requestURL}`);
        };

        clearEditor();
        setConfig({ _cms: { local_preview: { enabled: true, generator } } });
        return {
            editor,
            frontMatterControl,
            markdownPreview,
            calls,
            restore() {
                clearEditor();
                setConfig(null);
                globalThis.document = previousDocument;
                globalThis.fetch = previousFetch;
                globalThis.requestAnimationFrame = previousRequestAnimationFrame;
            },
        };
    }

    function deferred() {
        let resolve;
        let reject;
        const promise = new Promise((promiseResolve, promiseReject) => {
            resolve = promiseResolve;
            reject = promiseReject;
        });
        return { promise, resolve, reject };
    }

    for (const generator of ["hugo", "eleventy"]) {
        for (const previewFailure of [503, 500, "network"]) {
            it(`continues ${generator} article switching after Local Preview ${previewFailure} failure`, async () => {
                const harness = createArticleSwitchHarness({ generator, previewFailure });
                try {
                    await loadFile("posts/old.md");
                    harness.calls.length = 0;
                    harness.editor.value = "changed before switch";

                    await loadFile("posts/new.md");

                    assert.equal(getCurrentPath(), "posts/new.md");
                    const saveIndex = harness.calls.findIndex(call => call.url.endsWith("/admin/api/article") && call.options.method === "POST");
                    const previewIndex = harness.calls.findIndex(call => call.url.includes("/admin/api/preview/local"));
                    const newArticleIndex = harness.calls.findIndex(call => call.url.includes("/admin/api/article?") && call.url.includes("posts%2Fnew.md"));
                    assert.ok(saveIndex >= 0, "production save should complete before switching");
                    assert.ok(previewIndex > saveIndex, "Local Preview flush should follow the production save");
                    assert.ok(newArticleIndex > previewIndex, "the new article should load after the failed flush");
                } finally {
                    harness.restore();
                }
            });
        }
    }

    it("does not let an older Markdown Preview response overwrite the newest article", async () => {
        const markdownStarted = deferred();
        const markdownResult = deferred();
        const markdownResponses = new Map([
            ["posts/b.md", async () => {
                markdownStarted.resolve();
                return markdownResult.promise;
            }],
        ]);
        const harness = createArticleSwitchHarness({ markdownResponses });
        try {
            await loadFile("posts/a.md");
            const loadingB = loadFile("posts/b.md");
            await markdownStarted.promise;

            const loadingC = loadFile("posts/c.md");
            await loadingC;
            assert.equal(getCurrentPath(), "posts/c.md");
            assert.equal(harness.editor.value, "after");
            assert.equal(harness.markdownPreview.innerHTML, "<p>posts/c.md</p>");

            markdownResult.resolve({
                ok: true,
                status: 200,
                json: async () => ({ html: "<p>stale B preview</p>" }),
            });
            await loadingB;
            assert.equal(getCurrentPath(), "posts/c.md");
            assert.equal(harness.markdownPreview.innerHTML, "<p>posts/c.md</p>");
        } finally {
            harness.restore();
        }
    });

    it("does not apply an older Local Preview response after switching articles", async () => {
        const localPreviewStarted = deferred();
        const localPreviewResult = deferred();
        let localPreviewCalls = 0;
        const localPreviewResponses = new Map([
            ["posts/a.md", async () => {
                localPreviewCalls += 1;
                if (localPreviewCalls === 1) {
                    localPreviewStarted.resolve();
                    return localPreviewResult.promise;
                }
                return {
                    ok: true,
                    status: 200,
                    json: async () => ({ article_url: "valid A preview" }),
                };
            }],
        ]);
        const harness = createArticleSwitchHarness({ localPreviewResponses });
        const previousRefresh = window.refreshLocalPreviewArticleURL;
        const refreshedURLs = [];
        window.refreshLocalPreviewArticleURL = async result => {
            refreshedURLs.push(result.article_url);
        };
        try {
            await loadFile("posts/a.md");
            const refreshingA = refreshLocalLivePreview();
            await localPreviewStarted.promise;

            const loadingB = loadFile("posts/b.md");
            localPreviewResult.resolve({
                ok: true,
                status: 200,
                json: async () => ({ article_url: "stale A preview" }),
            });
            await loadingB;
            await refreshingA;

            assert.equal(getCurrentPath(), "posts/b.md");
            assert.deepEqual(refreshedURLs, ["valid A preview"]);
        } finally {
            window.refreshLocalPreviewArticleURL = previousRefresh;
            harness.restore();
        }
    });

    it("pauses editor and front matter writes during the entire article switch", async () => {
        const saveStarted = deferred();
        const saveResult = deferred();
        const localPreviewStarted = deferred();
        const localPreviewResult = deferred();
        const localPreviewResponses = new Map([
            ["posts/a.md", async () => {
                localPreviewStarted.resolve();
                return localPreviewResult.promise;
            }],
        ]);
        const harness = createArticleSwitchHarness({
            saveResponse: () => {
                saveStarted.resolve();
                return saveResult.promise;
            },
            localPreviewResponses,
        });
        try {
            await loadFile("posts/a.md");
            harness.editor.value = "draft A";

            const loadingB = loadFile("posts/b.md");
            await saveStarted.promise;
            assert.equal(harness.editor.disabled, true);
            assert.equal(harness.frontMatterControl.disabled, true);

            saveResult.resolve({
                ok: true,
                status: 200,
                json: async () => ({ status: "ok" }),
            });
            await localPreviewStarted.promise;
            assert.equal(harness.editor.disabled, true);
            assert.equal(harness.frontMatterControl.disabled, true);

            localPreviewResult.resolve({
                ok: true,
                status: 200,
                json: async () => ({ status: "ok" }),
            });
            await loadingB;
            assert.equal(getCurrentPath(), "posts/b.md");
            assert.equal(harness.editor.disabled, false);
            assert.equal(harness.frontMatterControl.disabled, false);
        } finally {
            harness.restore();
        }
    });

    it("keeps the article unchanged when production save fails", async () => {
        const harness = createArticleSwitchHarness({ saveFailure: true });
        try {
            await loadFile("posts/old.md");
            harness.calls.length = 0;
            harness.editor.value = "unsaved production change";

            await loadFile("posts/new.md");

            assert.equal(getCurrentPath(), "posts/old.md");
            assert.equal(harness.calls.some(call => call.url.includes("/admin/api/preview/local")), false);
            assert.equal(harness.calls.some(call => call.url.includes("posts%2Fnew.md")), false);
        } finally {
            harness.restore();
        }
    });

    for (const staleResult of ["success", "failure"]) {
        it(`keeps the newest article when an older load finishes ${staleResult} later`, async () => {
            const bFetchStarted = deferred();
            const bFetchResult = deferred();
            const articleResponses = new Map([
                ["posts/b.md", async () => {
                    bFetchStarted.resolve();
                    return bFetchResult.promise;
                }],
            ]);
            const harness = createArticleSwitchHarness({ articleResponses });
            try {
                await loadFile("posts/a.md");
                harness.calls.length = 0;
                harness.editor.value = "draft A";

                const loadingB = loadFile("posts/b.md");
                await bFetchStarted.promise;
                assert.equal(getCurrentPath(), "posts/a.md");

                const bRequest = harness.calls.find(call => call.url.includes("posts%2Fb.md"));
                assert.ok(bRequest?.options.signal, "article fetch should receive an AbortSignal");

                const loadingC = loadFile("posts/c.md");
                await loadingC;
                assert.equal(getCurrentPath(), "posts/c.md");
                assert.equal(harness.editor.value, "after");
                assert.equal(bRequest.options.signal.aborted, true);

                const saves = harness.calls
                    .filter(call => call.url.endsWith("/admin/api/article") && call.options.method === "POST")
                    .map(call => JSON.parse(call.options.body));
                assert.ok(saves.length >= 1, "the active article should be saved before navigation");
                assert.ok(saves.every(payload => payload.path === "posts/a.md"), "navigation must not save under the pending B path");
                assert.ok(saves.some(payload => payload.path === "posts/a.md" && payload.body === "draft A"), JSON.stringify(saves));

                if (staleResult === "success") {
                    bFetchResult.resolve({
                        ok: true,
                        status: 200,
                        json: async () => ({ path: "posts/b.md", content: "stale B" }),
                    });
                } else {
                    bFetchResult.reject(new Error("stale B fetch failed"));
                }
                await loadingB;

                assert.equal(getCurrentPath(), "posts/c.md");
                assert.equal(harness.editor.value, "after");
            } finally {
                harness.restore();
            }
        });
    }

});

describe("Git Sync editor gate", () => {
    it("waits for an AutoSave already in flight before allowing Sync to continue", async () => {
        const previousDocument = globalThis.document;
        const previousFetch = globalThis.fetch;
        const previousRequestAnimationFrame = globalThis.requestAnimationFrame;
        const editor = { disabled: false, value: "", placeholder: "" };
        const fmContainer = {
            style: { display: "" },
            innerHTML: "",
            querySelectorAll() { return []; },
        };
        const preview = { replaceChildren() {}, querySelectorAll() { return []; } };
        const previewStatus = {
            textContent: "",
            className: "",
            removeAttribute() {},
            setAttribute() {},
        };
        const elements = new Map([
            ["editor", editor],
            ["fm-container", fmContainer],
            ["filename-display", { textContent: "" }],
            ["markdown-preview", preview],
            ["markdown-preview-status", previewStatus],
        ]);
        globalThis.document = {
            getElementById(id) { return elements.get(id) || null; },
            querySelectorAll() { return []; },
        };
        globalThis.requestAnimationFrame = callback => {
            callback();
            return 1;
        };

        let resolveSave;
        let saveStarted;
        const saveStartedPromise = new Promise(resolve => { saveStarted = resolve; });
        let saveCompleted = false;
        globalThis.fetch = async (url, options = {}) => {
            if (url === "/admin/api/csrf-token") {
                return { ok: true, status: 200, json: async () => ({ csrf_token: "csrf" }) };
            }
            if (String(url).includes("/admin/api/article?") && !options.method) {
                return { ok: true, status: 200, json: async () => ({ path: "posts/pending.md", content: "before" }) };
            }
            if (String(url).includes("/admin/api/preview/markdown")) {
                return { ok: true, status: 200, json: async () => ({ html: "<p>before</p>" }) };
            }
            if (String(url).endsWith("/admin/api/article") && options.method === "POST") {
                saveStarted();
                return new Promise(resolve => {
                    resolveSave = () => {
                        saveCompleted = true;
                        resolve({ ok: true, status: 200, json: async () => ({ status: "ok" }) });
                    };
                });
            }
            throw new Error(`Unexpected request: ${url}`);
        };

        try {
            await loadFile("posts/pending.md");
            editor.value = "after";
            const saveOperation = execAutoSave();
            await saveStartedPromise;

            const syncPreparation = prepareForGitSync();
            await Promise.resolve();
            assert.equal(isGitSyncInProgress(), true);
            assert.equal(saveCompleted, false);
            await assert.rejects(flushPendingSave, /Git Sync is in progress/);
            let mutationCalled = false;
            await assert.rejects(
                () => runGitMutation(() => { mutationCalled = true; }),
                /Git Sync is in progress/,
            );
            assert.equal(mutationCalled, false);

            resolveSave();
            await saveOperation;
            await syncPreparation;
            assert.equal(saveCompleted, true);
        } finally {
            if (isGitSyncInProgress()) finishForGitSync();
            globalThis.document = previousDocument;
            globalThis.fetch = previousFetch;
            globalThis.requestAnimationFrame = previousRequestAnimationFrame;
        }
    });

    it("pauses editor writes and blocks edits while Sync is running", async () => {
        const previousDocument = globalThis.document;
        const previousMarkDeploymentPreviewStale = window.markDeploymentPreviewStale;
        const listeners = {};
        const editor = {
            disabled: false,
            value: "before sync",
            addEventListener(type, callback) { listeners[type] = callback; },
        };
        const controls = [{ disabled: false }, { disabled: false }];
        const fmContainer = {
            querySelectorAll() { return controls; },
            addEventListener(type, callback) { listeners[type] = callback; },
        };
        let staleMarkCount = 0;
        window.markDeploymentPreviewStale = () => { staleMarkCount += 1; };
        globalThis.document = {
            getElementById(id) {
                if (id === "editor") return editor;
                if (id === "fm-container") return fmContainer;
                return null;
            },
        };

        try {
            initAutoSave();
            await prepareForGitSync();

            assert.equal(isGitSyncInProgress(), true);
            assert.equal(editor.disabled, true);
            assert.deepEqual(controls.map(control => control.disabled), [true, true]);
            editor.value = "changed during sync";
            listeners.input();
            assert.equal(staleMarkCount, 0);
            assert.equal(await execAutoSave(), false);
        } finally {
            finishForGitSync();
            globalThis.document = previousDocument;
            window.markDeploymentPreviewStale = previousMarkDeploymentPreviewStale;
        }

        assert.equal(isGitSyncInProgress(), false);
        assert.equal(editor.disabled, false);
        assert.deepEqual(controls.map(control => control.disabled), [false, false]);
    });
});

describe("preview API contracts", () => {
    it("pins site-scoped read requests to their explicit site id", async () => {
        const calls = [];
        globalThis.fetch = async (url, options = {}) => {
            calls.push({ url, options });
            return { ok: true, status: 200, json: async () => ({ status: "ready" }) };
        };

        API.setCurrentSite("site-a");
        await API.fetchConfig("site-b");
        await API.fetchArticles("site-b");
        await API.fetchLocalPreviewStatus(undefined, "site-b");
        await API.fetchPreviewDeployment("draft/id", undefined, "site-b");

        assert.deepEqual(calls.map(call => call.url), [
            "/admin/api/config?site=site-b",
            "/admin/api/articles?site=site-b",
            "/admin/api/preview/local/status?site=site-b",
            "/admin/api/preview/deployments/draft%2Fid?site=site-b",
        ]);
        calls.forEach(call => assert.equal(call.options.headers["X-CMS-Site"], "site-b"));
    });

    it("pins site-scoped destructive preview requests to their explicit site id", async () => {
        const calls = [];
        globalThis.fetch = async (url, options = {}) => {
            calls.push({ url, options });
            if (url === "/admin/api/csrf-token") {
                return { ok: true, status: 200, json: async () => ({ csrf_token: "csrf" }) };
            }
            return { ok: true, status: 200, json: async () => ({ status: "ok" }) };
        };

        API.setCurrentSite("site-a");
        await API.stopLocalPreviewContent("site-b");
        await API.triggerPreviewDeployment("posts/one.md", "draft/id", "site-b");
        await API.retryPreviewDeployment("draft/id", "site-b");
        await API.discardPreviewDeployment("draft/id", "site-b");
        await API.runPublish("posts/one.md", "draft/id", "site-b");

        const requestCalls = calls.filter(call => call.url !== "/admin/api/csrf-token");
        assert.deepEqual(requestCalls.map(call => call.url), [
            "/admin/api/preview/local/stop?site=site-b",
            "/admin/api/preview/deployments?site=site-b",
            "/admin/api/preview/deployments/draft%2Fid/retry?site=site-b",
            "/admin/api/preview/deployments/draft%2Fid/discard?site=site-b",
            "/admin/api/publish?site=site-b",
        ]);
        requestCalls.forEach(call => assert.equal(call.options.headers["X-CMS-Site"], "site-b"));
    });

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
        await API.updateLocalPreviewContent(article, 7);
        await API.resolveLocalPreviewArticleURL(article.path);
        await API.fetchLocalPreviewStatus();
        await API.stopLocalPreviewContent();
        await API.triggerPreviewDeployment(article.path, "draft/id");
        await API.fetchPreviewDeployment("draft/id");
        await API.retryPreviewDeployment("draft/id");
        await API.discardPreviewDeployment("draft/id");
        await API.runPublish(article.path, "draft/id");

        const requestCalls = calls.filter(call => call.url !== "/admin/api/csrf-token");
        assert.equal(requestCalls[0].url, "/admin/api/preview/markdown?site=docs+site");
        assert.deepEqual(JSON.parse(requestCalls[0].options.body), article);
        assert.equal(requestCalls[1].url, "/admin/api/preview/local?site=docs+site");
        assert.deepEqual(JSON.parse(requestCalls[1].options.body), { ...article, revision: 7 });
        assert.equal(requestCalls[2].url, "/admin/api/preview/local/navigate?site=docs+site");
        assert.deepEqual(JSON.parse(requestCalls[2].options.body), { path: article.path });
        assert.equal(requestCalls[3].url, "/admin/api/preview/local/status?site=docs+site");
        assert.equal(requestCalls[4].url, "/admin/api/preview/local/stop?site=docs+site");
        assert.equal(requestCalls[5].url, "/admin/api/preview/deployments?site=docs+site");
        assert.deepEqual(JSON.parse(requestCalls[5].options.body), { path: article.path, draft_id: "draft/id" });
        assert.equal(requestCalls[6].url, "/admin/api/preview/deployments/draft%2Fid?site=docs+site");
        assert.equal(requestCalls[7].url, "/admin/api/preview/deployments/draft%2Fid/retry?site=docs+site");
        assert.equal(requestCalls[8].url, "/admin/api/preview/deployments/draft%2Fid/discard?site=docs+site");
        assert.deepEqual(JSON.parse(requestCalls[9].options.body), { path: article.path, draft_id: "draft/id" });
        requestCalls.forEach(call => {
            assert.equal(call.options.headers["X-CMS-Site"], "docs site");
        });
    });

});
