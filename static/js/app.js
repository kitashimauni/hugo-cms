import * as API from './api.js';
import * as UI from './ui.js';
import * as Editor from './editor.js';
import {
    createLocalPreviewFrameController,
    LOCAL_PREVIEW_INITIAL_NAVIGATION_MAX_ATTEMPTS,
    localPreviewNavigationRetryDelay,
    shouldAutoShowEmbeddedLocalPreview,
    shouldCloseEmbeddedLocalPreview,
    shouldRetryLocalPreviewNavigation,
    shouldUseLocalPreviewSplitDefault,
} from './local_preview.js';

// Global State
let cmsConfig = null;
let siteRegistry = null;
let publishInProgress = false;
let deploymentEnabled = false;
let deploymentState = null;
let deploymentPollTimer = null;
let deploymentController = null;
let deploymentOperationInProgress = false;
let localPreviewEnabled = false;
let localPreviewState = null;
let localPreviewPollTimer = null;
let localPreviewController = null;
let localPreviewOperationInProgress = false;
let localPreviewFrameController = null;
let localPreviewArticleURL = "";
let localPreviewArticleURLFresh = false;
let localPreviewURLResolutionGeneration = 0;
let localPreviewFrontMatterKey = "";
let localPreviewArticleURLKey = "";
let localPreviewURLResolution = null;

const LOCAL_PREVIEW_POLL_MS = 3000;
const LOCAL_PREVIEW_FRESH_NAVIGATION_MAX_ATTEMPTS = 120;

init();

async function init() {
    initializeLocalPreviewFrame();
    try {
        siteRegistry = await API.fetchSites();
        API.initializeCurrentSite(siteRegistry);
        UI.renderSiteSelector(siteRegistry, API.getCurrentSite(), switchSite);
        await loadSiteData();
    } catch (e) {
        console.error("Initial load failed", e);
        UI.showToast("Failed to load site configuration", "error");
    }

    Editor.initAutoSave();
    window.switchView = switchView;
    window.toggleSplitView = UI.toggleSplitView;
    window.toggleSidebar = UI.toggleSidebar;
    window.toggleHeaderMenu = UI.toggleHeaderMenu;
    window.closeModal = UI.closeModal;

    window.loadFile = loadFile;
    window.buildAndPreview = () => Editor.refreshMarkdownPreview();
    window.saveFile = async () => {
        await Editor.saveFile();
        await refreshFileList();
    };
    window.createNewFile = () => Editor.createNewFile(refreshFileList);
    window.deleteFile = async () => {
        await Editor.deleteFile(refreshFileList);
    };
    window.insertImage = () => {
        const currentPath = Editor.getCurrentPath();
        let collectionName = null;
        const collection = UI.getCollectionForPath(currentPath, cmsConfig);
        if (collection) collectionName = collection.name;

        UI.showMediaLibrary((file) => {
            const markdown = `![${file.name}](${file.path})`;
            Editor.insertText(markdown);
        }, collectionName, currentPath);
    };
    window.insertSnippet = async () => {
        try {
            const snippets = await API.fetchSnippets();
            const showList = () => {
                UI.showSnippetsModal(snippets, (snippet) => {
                    let body = Array.isArray(snippet.body) ? snippet.body.join('\n') : snippet.body;
                    const vars = new Map();
                    const regex = /\$\{(\d+)(?::([^}]*))?\}/g;
                    let match;
                    while ((match = regex.exec(body)) !== null) {
                        const id = match[1];
                        const def = match[2] || "";
                        if (!vars.has(id)) vars.set(id, { id, label: def || `Param ${id}`, default: def });
                    }
                    if (vars.size > 0) {
                        const varList = Array.from(vars.values()).sort((a, b) => a.id - b.id);
                        UI.showSnippetInputModal(varList, (values) => {
                            const finalBody = body.replace(regex, (m, id, def) => values[id] !== undefined ? values[id] : (def || ""));
                            Editor.insertText(finalBody);
                        }, showList);
                    } else {
                        Editor.insertText(body);
                        UI.closeModal();
                    }
                });
            };
            showList();
        } catch (e) {
            UI.showToast("Failed to load snippets: " + e.message, "error");
        }
    };
    window.resetChanges = Editor.resetChanges;
    window.showDiff = Editor.showDiff;

    window.runSync = runSync;
    window.publishFile = publishFile;
    window.updateDeploymentPreview = updateDeploymentPreview;
    window.retryDeploymentPreview = retryDeploymentPreview;
    window.discardDeploymentPreview = discardDeploymentPreview;
    window.markDeploymentPreviewStale = markDeploymentPreviewStale;
    window.openLocalLivePreview = openLocalLivePreview;
    window.toggleEmbeddedLocalPreview = toggleEmbeddedLocalPreview;
    window.showMarkdownFallback = () => switchView('markdown');
    window.stopLocalLivePreview = stopLocalLivePreview;
    window.refreshLocalPreviewArticleURL = refreshLocalPreviewArticleURL;

    console.log("Hugo CMS Initialized");
}

function cancelLocalPreviewURLResolution() {
    const resolution = localPreviewURLResolution;
    localPreviewURLResolution = null;
    resolution?.controller.abort();
}

function resetLocalPreviewArticleURL() {
    cancelLocalPreviewURLResolution();
    localPreviewURLResolutionGeneration += 1;
    localPreviewArticleURL = "";
    localPreviewArticleURLFresh = false;
    localPreviewArticleURLKey = "";
    localPreviewFrontMatterKey = "";
}

async function loadSiteData() {
    stopLocalPreviewMonitoring();
    resetLocalPreviewArticleURL();
    localPreviewState = null;

    cmsConfig = await API.fetchConfig();
    const site = siteRegistry?.sites?.find(s => s.id === API.getCurrentSite());
    if (!cmsConfig._cms) cmsConfig._cms = {};
    cmsConfig._cms.local_preview = site?.preview?.local_preview || { enabled: false, url: '' };
    Editor.setConfig(cmsConfig);
    UI.renderConfigWarnings(cmsConfig);

    localPreviewEnabled = cmsConfig?._cms?.local_preview?.enabled === true && Boolean(localPreviewURL());
    configureLocalPreviewPanel();
    UI.switchView('edit');
    if (localPreviewEnabled) {
        await refreshLocalPreviewStatus();
        scheduleLocalPreviewMonitoring();
    }

    deploymentEnabled = UI.configureDeploymentPreview(cmsConfig);
    deploymentState = null;
    UI.renderDeploymentState(null);
    await refreshFileList();
}

async function switchSite(siteID) {
    const previousSiteID = API.getCurrentSite();
    if (!siteID || siteID === previousSiteID) return;

    try {
        await Editor.flushPendingSave();
    } catch (e) {
        UI.showToast("Site switch cancelled: save failed", "error");
        UI.renderSiteSelector(siteRegistry, previousSiteID, switchSite);
        return;
    }

    stopLocalPreviewMonitoring();
    stopDeploymentPolling();
    closeEmbeddedLocalPreview();
    API.setCurrentSite(siteID);
    Editor.clearEditor();
    try {
        await loadSiteData();
        UI.renderSiteSelector(siteRegistry, siteID, switchSite);
        const site = siteRegistry?.sites?.find(s => s.id === siteID);
        UI.showToast(`Switched to ${site?.name || siteID}`, "success");
    } catch (e) {
        API.setCurrentSite(previousSiteID);
        UI.renderSiteSelector(siteRegistry, previousSiteID, switchSite);
        try {
            await loadSiteData();
        } catch (reloadErr) {
            console.error("Failed to reload previous site", reloadErr);
        }
        UI.showToast("Failed to switch site: " + e.message, "error");
    }
}

async function loadFile(path) {
    stopDeploymentPolling();
    deploymentState = null;
    UI.renderDeploymentState(null);
    await Editor.loadFile(path);
    if (Editor.getCurrentPath() !== path) {
        await refreshLocalPreviewStatus();
        return;
    }

    resetLocalPreviewArticleURL();
    if (localPreviewEnabled) {
        localPreviewFrameController?.resetDismissed();
        updateLocalPreviewAvailability();
        UI.switchView(shouldUseLocalPreviewSplitDefault({
            enabled: localPreviewEnabled,
            narrowViewport: isNarrowViewport(),
        }) ? 'split' : 'edit');
        try {
            await Editor.refreshLocalLivePreview();
            const articleURL = await resolveLocalPreviewArticleURL(Editor.getCurrentLocalPreviewFrontMatterKey());
            if (Editor.getCurrentPath() === path && !articleURL) {
                throw new Error('generatorから記事URLを取得できませんでした');
            }
            if (Editor.getCurrentPath() === path) showEmbeddedLocalPreview();
        } catch (error) {
            showLocalPreviewResolutionError(error);
        }
        await refreshLocalPreviewStatus();
    }
    if (deploymentEnabled) await refreshDeploymentState();
}

async function refreshFileList() {
    try {
        const files = await API.fetchArticles();
        if (files) {
            UI.renderFileList(files, cmsConfig);
            return files;
        }
    } catch (e) {
        UI.showToast("Failed to fetch file list", "error");
    }
    return null;
}

async function switchView(viewName) {
    if ((viewName === 'preview' && !localPreviewEnabled) || viewName === 'markdown') {
        try {
            await Editor.refreshMarkdownPreview();
        } catch (_) {
            // The preview surface already shows the request error.
        }
    }
    UI.switchView(viewName);
}

function localPreviewURL() {
    return UI.safeExternalURL(cmsConfig?._cms?.local_preview?.url || "");
}

function currentLocalPreviewURL() {
    return Editor.getCurrentPath() ? localPreviewArticleURL : localPreviewURL();
}

function isNarrowViewport() {
    if (typeof window.matchMedia === 'function') {
        return window.matchMedia('(max-width: 768px)').matches;
    }
    return Number(window.innerWidth || 0) > 0 && window.innerWidth <= 768;
}

function configureLocalPreviewPanel() {
    const panel = document.getElementById('local-preview-panel');
    updateLocalPreviewAvailability();
    if (!localPreviewEnabled) {
        closeEmbeddedLocalPreview();
        return;
    }
    if (!panel) return;
    localPreviewFrameController?.resetDismissed();
    renderLocalPreviewState({ enabled: true, status: 'stopped', process_state: 'stopped', workspace_active: false });
}

function initializeLocalPreviewFrame() {
    const frame = document.getElementById('local-preview-frame');
    if (!frame || localPreviewFrameController) return;
    localPreviewFrameController = createLocalPreviewFrameController({
        getURL: currentLocalPreviewURL,
        wrapper: document.getElementById('local-preview-embed'),
        frame,
        button: document.getElementById('local-preview-embed-btn'),
        loading: document.getElementById('local-preview-frame-loading'),
        error: document.getElementById('local-preview-frame-error'),
        errorMessage: document.getElementById('local-preview-frame-error-message'),
    });
    frame.addEventListener('load', handleLocalPreviewFrameLoad);
    frame.addEventListener('error', () => localPreviewFrameController?.handleError());
    window.addEventListener('message', handleLocalPreviewReadyMessage);
}

function handleLocalPreviewReadyMessage(event) {
    const frame = document.getElementById('local-preview-frame');
    const url = localPreviewURL();
    if (!frame || !url || event.source !== frame.contentWindow) return;
    let origin;
    try {
        origin = new URL(url).origin;
    } catch (_) {
        return;
    }
    if (event.origin !== origin || event.data?.type !== 'homecms-local-preview-ready') return;
    localPreviewFrameController?.handleReady();
}

function handleLocalPreviewFrameLoad() {
    localPreviewFrameController?.handleLoad();
}

function updateLocalPreviewAvailability() {
    const contentArea = document.getElementById('content-area');
    if (!contentArea) return;
    const hasCurrentPath = Boolean(Editor.getCurrentPath());
    contentArea.dataset.localPreviewHasArticle = String(hasCurrentPath);
    contentArea.classList.toggle('local-preview-enabled', localPreviewEnabled);
}

function showEmbeddedLocalPreview(options = {}) {
    if (Editor.getCurrentPath() && !localPreviewArticleURL) return false;
    const shown = localPreviewFrameController?.show(options) === true;
    if (shown) updateLocalPreviewAvailability();
    return shown;
}

function showLocalPreviewFrameError(message) {
    localPreviewFrameController?.showError(message);
}

function showLocalPreviewResolutionError(error) {
    const message = `記事URLを解決できません: ${error?.message || 'generatorからURLを取得できませんでした。'}`;
    const messageEl = document.getElementById('local-preview-message');
    if (messageEl) messageEl.textContent = message;
    showLocalPreviewFrameError(message);
}

function closeEmbeddedLocalPreview(options = {}) {
    localPreviewFrameController?.close(options);
    updateLocalPreviewAvailability();
}

function isLocalPreviewArticleURL(value) {
    const rootURL = localPreviewURL();
    if (!rootURL || !value) return false;
    try {
        return new URL(value).origin === new URL(rootURL).origin;
    } catch (_) {
        return false;
    }
}

function localPreviewURLResolutionKey(frontMatterKey) {
    return [API.getCurrentSite(), Editor.getCurrentPath(), frontMatterKey || ""].join("\u0000");
}

function localPreviewResolutionAbortError() {
    const error = new Error('Local Preview URL resolution was cancelled');
    error.name = 'AbortError';
    return error;
}

function waitForLocalPreviewRetry(delay, signal) {
    return new Promise((resolve, reject) => {
        if (signal.aborted) {
            reject(localPreviewResolutionAbortError());
            return;
        }
        let timer = setTimeout(() => {
            signal.removeEventListener('abort', cancel);
            resolve();
        }, delay);
        function cancel() {
            clearTimeout(timer);
            timer = null;
            signal.removeEventListener('abort', cancel);
            reject(localPreviewResolutionAbortError());
        }
        signal.addEventListener('abort', cancel, { once: true });
    });
}

function resolveLocalPreviewArticleURLState(frontMatterKey = localPreviewFrontMatterKey, { requireFresh = false } = {}) {
    if (!localPreviewEnabled || !Editor.getCurrentPath()) return Promise.resolve(null);
    const requestPath = Editor.getCurrentPath();
    const requestKey = localPreviewURLResolutionKey(frontMatterKey);
    if (!requireFresh && localPreviewArticleURL && localPreviewArticleURLKey === requestKey) {
        return Promise.resolve({
            url: localPreviewArticleURL,
            fresh: localPreviewArticleURLFresh,
            result: null,
            generation: localPreviewURLResolutionGeneration,
        });
    }
    const resolutionKey = `${requestKey}\u0000${requireFresh ? 'fresh' : 'cached'}`;
    if (localPreviewURLResolution?.key === resolutionKey) return localPreviewURLResolution.promise;

    cancelLocalPreviewURLResolution();
    localPreviewURLResolutionGeneration += 1;
    const requestGeneration = localPreviewURLResolutionGeneration;
    const controller = new AbortController();
    const maxAttempts = requireFresh
        ? LOCAL_PREVIEW_FRESH_NAVIGATION_MAX_ATTEMPTS
        : LOCAL_PREVIEW_INITIAL_NAVIGATION_MAX_ATTEMPTS;

    const resolution = {
        controller,
        key: resolutionKey,
        promise: null,
    };
    resolution.promise = (async () => {
        for (let attempt = 1; attempt <= maxAttempts; attempt++) {
            if (controller.signal.aborted) throw localPreviewResolutionAbortError();
            if (
                Editor.getCurrentPath() !== requestPath ||
                localPreviewURLResolutionGeneration !== requestGeneration
            ) return null;
            try {
                const result = await API.resolveLocalPreviewArticleURL(requestPath, controller.signal);
                const articleURL = UI.safeExternalURL(result?.article_url || "");
                if (!isLocalPreviewArticleURL(articleURL)) throw new Error('generator returned an invalid local preview URL');
                const fresh = result?.fresh !== false && result?.metadata_status !== 'stale' && result?.status !== 'stale';
                if (requireFresh && !fresh) {
                    await waitForLocalPreviewRetry(localPreviewNavigationRetryDelay(attempt), controller.signal);
                    continue;
                }
                if (
                    Editor.getCurrentPath() !== requestPath ||
                    localPreviewURLResolutionGeneration !== requestGeneration
                ) return null;
                localPreviewArticleURL = articleURL;
                localPreviewArticleURLFresh = fresh;
                localPreviewArticleURLKey = requestKey;
                return { url: articleURL, fresh, result, generation: requestGeneration };
            } catch (error) {
                if (error?.name === 'AbortError') throw error;
                const transient = error?.status === undefined || error?.status === 408 || error?.status === 425 || error?.status === 429 || error?.status >= 500;
                if (!(requireFresh && attempt < maxAttempts && transient) && !shouldRetryLocalPreviewNavigation({ error, attempt })) throw error;
                await waitForLocalPreviewRetry(localPreviewNavigationRetryDelay(attempt), controller.signal);
            }
        }
        return null;
    })();
    localPreviewURLResolution = resolution;
    resolution.promise.then(
        () => { if (localPreviewURLResolution === resolution) localPreviewURLResolution = null; },
        () => { if (localPreviewURLResolution === resolution) localPreviewURLResolution = null; },
    );
    return resolution.promise;
}

function reconcileFreshLocalPreviewArticleURL(frontMatterKey, cachedURL) {
    const requestPath = Editor.getCurrentPath();
    if (!requestPath || !cachedURL) return;
    void resolveLocalPreviewArticleURLState(frontMatterKey, { requireFresh: true }).then((resolution) => {
        if (!resolution || Editor.getCurrentPath() !== requestPath || resolution.generation !== localPreviewURLResolutionGeneration) return;
        if (resolution.url !== cachedURL && !localPreviewFrameController?.isDismissed()) {
            showEmbeddedLocalPreview({ reload: true });
        }
    }).catch((error) => {
        if (error?.name !== 'AbortError') console.warn('[LocalPreview] fresh URL reconciliation failed', error);
    });
}

async function resolveLocalPreviewArticleURL(frontMatterKey = localPreviewFrontMatterKey) {
    const resolution = await resolveLocalPreviewArticleURLState(frontMatterKey);
    if (resolution?.url && !resolution.fresh) {
        reconcileFreshLocalPreviewArticleURL(frontMatterKey, resolution.url);
    }
    return resolution?.url || null;
}

async function refreshLocalPreviewArticleURL(updateResult, frontMatterKey = "") {
    const requestKey = localPreviewURLResolutionKey(frontMatterKey);
    if (localPreviewArticleURL && localPreviewArticleURLKey === requestKey && localPreviewArticleURLFresh) {
        return localPreviewArticleURL;
    }
    localPreviewFrontMatterKey = frontMatterKey;
    if (localPreviewArticleURL && localPreviewArticleURLKey === requestKey && !localPreviewArticleURLFresh) {
        reconcileFreshLocalPreviewArticleURL(frontMatterKey, localPreviewArticleURL);
        return localPreviewArticleURL;
    }
    localPreviewArticleURL = "";
    localPreviewArticleURLFresh = false;
    localPreviewArticleURLKey = "";
    if (!localPreviewEnabled || !Editor.getCurrentPath()) return null;
    try {
        const articleURL = await resolveLocalPreviewArticleURL(frontMatterKey);
        if (articleURL && Editor.getCurrentPath() && !localPreviewFrameController?.isDismissed()) {
            showEmbeddedLocalPreview();
        }
        return articleURL;
    } catch (error) {
        showLocalPreviewResolutionError(error);
        return null;
    }
}

function localPreviewStatusClass(status) {
    if (status === 'ready') return 'ready';
    if (status === 'starting') return 'queued';
    if (status === 'failed') return 'failed';
    return 'idle';
}

function localPreviewStatusLabel(status) {
    return ({
        stopped: '停止',
        starting: '起動中',
        ready: 'Ready',
        failed: '失敗',
        stopping: '停止中',
        disabled: '無効',
    })[status] || status || '停止';
}

function renderLocalPreviewState(state) {
    localPreviewState = UI.normalizeLocalPreviewState(state);
    state = localPreviewState;
    const statusEl = document.getElementById('local-preview-status');
    const messageEl = document.getElementById('local-preview-message');
    const policyEl = document.getElementById('local-preview-policy');
    const loadingEl = document.getElementById('local-preview-loading');
    const stopBtn = document.getElementById('local-preview-stop-btn');
    if (!statusEl || !messageEl) return;

    const status = state?.status || 'stopped';
    statusEl.textContent = localPreviewStatusLabel(status);
    statusEl.className = `deployment-status ${localPreviewStatusClass(status)}`;

    let message = '記事を選択すると未保存内容をshadow workspaceへ同期します。';
    if (status === 'ready') message = 'Live Preview is ready. 編集内容はLiveReloadで反映されます。';
    else if (status === 'starting') message = 'Live Preview generatorを起動しています…';
    else if (status === 'failed') message = state?.process_error || 'Live Preview generatorの起動に失敗しました。';
    else if (state?.workspace_active) message = '未保存内容は同期済みです。Previewを開くとgeneratorを起動します。';
    messageEl.textContent = message;

    if (policyEl) {
        if (state?.always_on === true) {
            let policy = '常駐設定中';
            if (state.supervisor_state === 'retrying') policy += '（自動復旧を待機中）';
            if (state.next_refresh_at) {
                const nextRefresh = new Date(state.next_refresh_at);
                const formatted = Number.isNaN(nextRefresh.getTime())
                    ? state.next_refresh_at
                    : nextRefresh.toLocaleString('ja-JP', { timeZone: state.refresh_timezone || 'UTC' });
                policy += ` · 次回refresh: ${formatted}`;
            }
            policyEl.textContent = policy;
        } else {
            policyEl.textContent = '';
        }
    }

    if (loadingEl) loadingEl.classList.toggle('hidden', status !== 'starting');

    if (stopBtn) {
        const processRunning = state?.process_state && state.process_state !== 'stopped';
        stopBtn.classList.toggle('hidden', !processRunning);
    }

    const hasCurrentPath = Boolean(Editor.getCurrentPath());
    if (shouldCloseEmbeddedLocalPreview({ status, hasCurrentPath })) {
        closeEmbeddedLocalPreview();
    } else if (shouldAutoShowEmbeddedLocalPreview({
        status,
        hasCurrentPath,
        dismissed: localPreviewFrameController?.isDismissed(),
    })) {
        showEmbeddedLocalPreview();
    } else if (status === 'failed' && state?.process_error) {
        showLocalPreviewFrameError(state.process_error);
    }
}

function stopLocalPreviewMonitoring() {
    if (localPreviewPollTimer) clearTimeout(localPreviewPollTimer);
    localPreviewPollTimer = null;
    if (localPreviewController) localPreviewController.abort();
    localPreviewController = null;
}

function scheduleLocalPreviewMonitoring() {
    if (!localPreviewEnabled) return;
    if (!localPreviewPollTimer) {
        localPreviewPollTimer = setTimeout(async () => {
            localPreviewPollTimer = null;
            await refreshLocalPreviewStatus();
            scheduleLocalPreviewMonitoring();
        }, LOCAL_PREVIEW_POLL_MS);
    }
}

async function refreshLocalPreviewStatus() {
    if (!localPreviewEnabled) return null;
    if (localPreviewController) localPreviewController.abort();
    const controller = new AbortController();
    localPreviewController = controller;
    try {
        const state = await API.fetchLocalPreviewStatus(controller.signal);
        renderLocalPreviewState(state);
        return localPreviewState;
    } catch (e) {
        if (e?.name !== 'AbortError') console.error('[LocalPreview] status failed', e);
        return null;
    } finally {
        if (localPreviewController === controller) localPreviewController = null;
    }
}

async function openLocalLivePreview() {
    if (!localPreviewEnabled || !localPreviewURL()) {
        UI.showToast('Local Live Preview is not configured', 'warning');
        return;
    }
    try {
        if (Editor.getCurrentPath()) {
            await Editor.refreshLocalLivePreview();
            const articleURL = await resolveLocalPreviewArticleURL(Editor.getCurrentLocalPreviewFrontMatterKey());
            if (!articleURL) throw new Error('generatorから記事URLを取得できませんでした');
        }
        const url = currentLocalPreviewURL();
        if (!url) throw new Error('記事URLを解決できませんでした');
        window.open(url, '_blank', 'noopener');
        setTimeout(() => refreshLocalPreviewStatus(), 500);
    } catch (e) {
        showLocalPreviewResolutionError(e);
        UI.showToast('Local Live Previewを開けません: ' + e.message, 'error');
    }
}

async function toggleEmbeddedLocalPreview() {
    const wrapper = document.getElementById('local-preview-embed');
    const frame = document.getElementById('local-preview-frame');
    const btn = document.getElementById('local-preview-embed-btn');
    if (!wrapper || !frame || !btn) return;
    if (!wrapper.classList.contains('hidden')) {
        closeEmbeddedLocalPreview({ dismiss: true });
        return;
    }

    if (!localPreviewURL()) return UI.showToast('Local Live Preview URL is unavailable', 'warning');
    try {
        localPreviewFrameController?.resetDismissed();
        if (Editor.getCurrentPath()) {
            await Editor.refreshLocalLivePreview();
            const articleURL = await resolveLocalPreviewArticleURL(Editor.getCurrentLocalPreviewFrontMatterKey());
            if (!articleURL) throw new Error('generatorから記事URLを取得できませんでした');
        }
        showEmbeddedLocalPreview({ reload: true });
        setTimeout(() => refreshLocalPreviewStatus(), 500);
    } catch (e) {
        showLocalPreviewResolutionError(e);
        UI.showToast('埋め込みpreviewを開始できません: ' + e.message, 'error');
    }
}

async function stopLocalLivePreview() {
    if (!localPreviewEnabled || localPreviewOperationInProgress) return;
    localPreviewOperationInProgress = true;
    try {
        await Editor.prepareLocalLivePreviewStop();
        await API.stopLocalPreviewContent();
        resetLocalPreviewArticleURL();
        closeEmbeddedLocalPreview();
        UI.showToast('Local Live Previewを停止しました', 'success');
    } catch (e) {
        UI.showToast('Local Live Previewを停止できません: ' + e.message, 'error');
    } finally {
        localPreviewOperationInProgress = false;
        await refreshLocalPreviewStatus();
    }
}

async function runSync() {
    if (!confirm("GitHubから最新の状態を取得しますか？\n（ローカルの未保存の変更は注意してください）")) return;

    const btn = document.querySelector('button[onclick="runSync()"]');
    const originalText = btn ? btn.textContent : "Sync";
    const originalDisabled = btn ? btn.disabled : false;
    if (btn) {
        btn.textContent = "Syncing...";
        btn.disabled = true;
    }

    const currentPath = Editor.getCurrentPath();
    let previewPrepared = false;
    let syncResponseReceived = false;
    let syncCompleted = false;
    try {
        // Pause and drain every editor write source while the server pulls
        // production. This prevents a delayed AutoSave or preview update from
        // recreating the pre-sync workspace afterwards.
        await Editor.prepareForGitSync();
        previewPrepared = true;

        const data = await API.runSync();
        syncResponseReceived = true;
        if (data.status === 'ok') {
            syncCompleted = true;
            resetLocalPreviewArticleURL();
            closeEmbeddedLocalPreview();
            if (data.local_preview_reset === false) {
                UI.showToast("Sync Complete, but Local Live Preview reset failed", "warning");
            } else {
                UI.showToast("Sync Complete", "success");
            }
            const files = await refreshFileList();
            if (currentPath && Array.isArray(files)) {
                // The user may have edited the article while Sync was
                // running. Evaluate the state after the response, not before
                // the gate was installed.
                const editorHasUnsavedChanges = Editor.hasUnsavedChanges();
                const currentFileStillExists = files.some(file => file.path === currentPath);
                if (!currentFileStillExists) {
                    if (editorHasUnsavedChanges) {
                        UI.showToast("Current article was removed remotely; unsaved editor content was kept", "warning");
                    } else {
                        Editor.clearEditor();
                    }
                } else if (!editorHasUnsavedChanges) {
                    // A clean editor may still contain the pre-sync remote
                    // payload. Reload it without starting Local Preview so the
                    // next explicit Preview uses the current production tree.
                    await Editor.loadFile(currentPath, { allowDuringGitSync: true });
                }
            }
            await refreshLocalPreviewStatus();
        } else {
            UI.showToast("Sync Error: " + data.log, "error");
        }
    } catch (e) {
        UI.showToast("Network Error", "error");
        if (previewPrepared && !syncResponseReceived) {
            // The server may have completed the pull/reset even if the
            // response was lost. Do not send the old editor payload back into
            // a possibly-reset workspace; force the next Preview action to
            // perform the authoritative resync instead.
            resetLocalPreviewArticleURL();
            closeEmbeddedLocalPreview();
        }
    } finally {
        if (previewPrepared || Editor.isGitSyncInProgress()) {
            Editor.finishForGitSync();
        }
        if (previewPrepared && syncResponseReceived && !syncCompleted) {
            // If Git sync failed before the server reset, restore the current
            // editor payload so an unsuccessful sync does not silently stop
            // the existing Local Preview update path.
            try {
                await Editor.refreshLocalLivePreview();
            } catch (previewError) {
                console.error('[LocalPreview] failed to restore after Git sync', previewError);
            }
        }
        if (btn) {
            btn.textContent = originalText;
            btn.disabled = originalDisabled;
        }
    }
}

async function runPublish(path, draftID) {
    if (publishInProgress) {
        UI.showToast("Publish is already running", "warning");
        return;
    }
    if (!path || !draftID || deploymentState?.status !== 'ready') {
        UI.showToast("Readyになったデプロイプレビューを確認してから公開してください", "warning");
        return;
    }
    if (!confirm("確認済みのデプロイ内容からPRを作成しますか？")) return;
    publishInProgress = true;

    const btn = document.getElementById('publish-preview-btn');
    let originalText = "";
    if (btn) {
        originalText = btn.innerHTML;
        btn.textContent = "PRを作成中…";
        btn.disabled = true;
    }

    try {
        await Editor.flushPendingSave();
        const data = await Editor.runGitMutation(() => API.runPublish(path, draftID));
        if (data.status === 'ok') {
            UI.showToast("PRを作成しました", "success");
            const url = UI.safeExternalURL(data.url);
            if (url) window.open(url, '_blank', 'noopener');
            await refreshFileList();
        } else {
            UI.showToast("Publish Error: " + data.log, "error");
        }
    } catch (e) {
        UI.showToast("Publish cancelled: " + e.message, "error");
    } finally {
        publishInProgress = false;
        if (btn) {
            btn.innerHTML = originalText;
            btn.disabled = false;
        }
    }
}

async function publishFile() {
    const currentPath = Editor.getCurrentPath();
    if (!currentPath) {
        UI.showToast("No file selected", "warning");
        return;
    }
    await runPublish(currentPath, Editor.getDraftID());
}

function stopDeploymentPolling() {
    if (deploymentPollTimer) {
        clearTimeout(deploymentPollTimer);
        deploymentPollTimer = null;
    }
    if (deploymentController) {
        deploymentController.abort();
        deploymentController = null;
    }
}

function applyDeploymentState(state) {
    deploymentState = UI.normalizeDeploymentState(state);
    UI.renderDeploymentState(deploymentState);
    if (deploymentState?.status === 'queued' || deploymentState?.status === 'building') {
        deploymentPollTimer = setTimeout(() => refreshDeploymentState(), 3000);
    }
}

function markDeploymentPreviewStale() {
    if (!deploymentState || deploymentState.status === 'stale') return;
    stopDeploymentPolling();
    applyDeploymentState({
        ...deploymentState,
        status: 'stale',
        url: '',
        message: '編集内容が変わりました。デプロイプレビューを更新してください。',
    });
}

async function refreshDeploymentState() {
    stopDeploymentPolling();
    if (!deploymentEnabled || !Editor.getCurrentPath()) {
        applyDeploymentState(null);
        return;
    }
    const path = Editor.getCurrentPath();
    const draftID = Editor.getDraftID();
    const controller = new AbortController();
    deploymentController = controller;
    try {
        const state = await API.fetchPreviewDeployment(draftID, controller.signal);
        if (path !== Editor.getCurrentPath() || draftID !== Editor.getDraftID()) return;
        applyDeploymentState(state);
    } catch (e) {
        if (e?.name !== 'AbortError') {
            UI.showToast(e.message, 'error');
            if (deploymentState?.status === 'queued' || deploymentState?.status === 'building') {
                deploymentPollTimer = setTimeout(() => refreshDeploymentState(), 5000);
            }
        }
    } finally {
        if (deploymentController === controller) deploymentController = null;
    }
}

async function updateDeploymentPreview() {
    const path = Editor.getCurrentPath();
    if (!deploymentEnabled || !path) {
        UI.showToast("デプロイ対象の記事を選択してください", "warning");
        return;
    }
    if (deploymentOperationInProgress) {
        UI.showToast("デプロイ操作を実行中です", "warning");
        return;
    }
    deploymentOperationInProgress = true;
    try {
        stopDeploymentPolling();
        await Editor.flushPendingSave();
        applyDeploymentState({ status: 'queued', message: 'デプロイを開始しています…' });
        const state = await Editor.runGitMutation(() => API.triggerPreviewDeployment(path, Editor.getDraftID()));
        if (path === Editor.getCurrentPath()) applyDeploymentState(state);
    } catch (e) {
        applyDeploymentState({ status: 'failed', message: e.message, retryable: false });
        UI.showToast(e.message, 'error');
    } finally {
        deploymentOperationInProgress = false;
    }
}

async function retryDeploymentPreview() {
    if (!deploymentEnabled || !Editor.getCurrentPath()) return;
    if (deploymentOperationInProgress) return;
    deploymentOperationInProgress = true;
    try {
        stopDeploymentPolling();
        applyDeploymentState({ ...deploymentState, status: 'queued', message: '再試行しています…' });
        const state = await Editor.runGitMutation(() => API.retryPreviewDeployment(Editor.getDraftID()));
        applyDeploymentState(state);
    } catch (e) {
        applyDeploymentState({ ...deploymentState, status: 'failed', message: e.message });
        UI.showToast(e.message, "error");
    } finally {
        deploymentOperationInProgress = false;
    }
}

async function discardDeploymentPreview() {
    if (!deploymentEnabled || !Editor.getCurrentPath()) return;
    if (deploymentOperationInProgress) return;
    if (!confirm("このデプロイプレビューと下書きbranchを破棄しますか？")) return;
    deploymentOperationInProgress = true;
    try {
        stopDeploymentPolling();
        await Editor.runGitMutation(() => API.discardPreviewDeployment(Editor.getDraftID()));
        Editor.resetDraftID();
        applyDeploymentState(null);
        UI.showToast("デプロイプレビューを破棄しました", "success");
    } catch (e) {
        UI.showToast(e.message, "error");
    } finally {
        deploymentOperationInProgress = false;
    }
}
