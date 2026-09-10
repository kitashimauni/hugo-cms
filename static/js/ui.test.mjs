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
    execAutoSave,
    finishForGitSync,
    flushLocalPreviewBeforeArticleSwitch,
    getOrCreateDraftID,
    initAutoSave,
    isGitSyncInProgress,
    loadFile,
    prepareForGitSync,
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
