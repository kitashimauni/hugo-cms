import * as API from './api.js';
import * as UI from './ui.js';
import * as Editor from './editor.js';

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
let localPreviewSessionID = "";
let localPreviewPollTimer = null;
let localPreviewHeartbeatTimer = null;
let localPreviewController = null;
let localPreviewOperationInProgress = false;

const LOCAL_PREVIEW_POLL_MS = 3000;
const LOCAL_PREVIEW_HEARTBEAT_MS = 30000;

init();

async function init() {
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
        await refreshLocalPreviewStatus();
        if (!localPreviewState?.session_active) localPreviewSessionID = "";
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
    window.stopLocalLivePreview = stopLocalLivePreview;
    window.reclaimLocalLivePreview = reclaimLocalLivePreview;

    console.log("Hugo CMS Initialized");
}

async function loadSiteData() {
    stopLocalPreviewMonitoring();
    localPreviewSessionID = "";
    localPreviewState = null;

    cmsConfig = await API.fetchConfig();
    const site = siteRegistry?.sites?.find(s => s.id === API.getCurrentSite());
    if (!cmsConfig._cms) cmsConfig._cms = {};
    cmsConfig._cms.local_preview = site?.preview?.local_preview || { enabled: false, url: '' };
    Editor.setConfig(cmsConfig);
    UI.renderConfigWarnings(cmsConfig);

    localPreviewEnabled = cmsConfig?._cms?.local_preview?.enabled === true && Boolean(localPreviewURL());
    configureLocalPreviewPanel();
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

    try {
        await Editor.releaseLocalLivePreview();
        localPreviewSessionID = "";
    } catch (e) {
        UI.showToast("Site switch cancelled: Local Live Preview cleanup failed", "error");
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
    const previousLocalPreviewSessionID = localPreviewSessionID;
    await Editor.loadFile(path);
    if (Editor.getCurrentPath() !== path) {
        // Editor keeps its owner ID when release failed; keep the matching
        // heartbeat handle so the live session does not expire while the user
        // retries the switch.
        localPreviewSessionID = previousLocalPreviewSessionID;
        await refreshLocalPreviewStatus();
        return;
    }

    localPreviewSessionID = "";
    if (localPreviewEnabled) {
        try {
            await ensureLocalPreviewSession();
        } catch (_) {
            // Editor already reports 409 conflicts; status panel provides the
            // persistent recovery action when the old session becomes stale.
        }
        await refreshLocalPreviewStatus();
    }
    if (deploymentEnabled) await refreshDeploymentState();
}

async function refreshFileList() {
    try {
        const files = await API.fetchArticles();
        if (files) UI.renderFileList(files, cmsConfig);
    } catch (e) {
        UI.showToast("Failed to fetch file list", "error");
    }
}

async function switchView(viewName) {
    if (viewName === 'preview') {
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

function configureLocalPreviewPanel() {
    const panel = document.getElementById('local-preview-panel');
    if (!panel) return;
    panel.classList.toggle('hidden', !localPreviewEnabled);
    if (!localPreviewEnabled) {
        closeEmbeddedLocalPreview();
        return;
    }
    renderLocalPreviewState({ enabled: true, status: 'stopped', process_state: 'stopped', session_active: false });
}

function localPreviewStatusClass(status) {
    if (status === 'ready') return 'ready';
    if (status === 'starting') return 'queued';
    if (status === 'failed' || status === 'stale' || status === 'conflict') return 'failed';
    return 'idle';
}

function localPreviewStatusLabel(status) {
    return ({
        stopped: '停止',
        starting: '起動中',
        ready: 'Ready',
        failed: '失敗',
        stopping: '停止中',
        stale: '期限切れ',
        conflict: '別タブ使用中',
        disabled: '無効',
    })[status] || status || '停止';
}

function renderLocalPreviewState(state) {
    localPreviewState = state || null;
    const statusEl = document.getElementById('local-preview-status');
    const messageEl = document.getElementById('local-preview-message');
    const reclaimBtn = document.getElementById('local-preview-reclaim-btn');
    const stopBtn = document.getElementById('local-preview-stop-btn');
    if (!statusEl || !messageEl) return;

    const status = state?.status || 'stopped';
    statusEl.textContent = localPreviewStatusLabel(status);
    statusEl.className = `deployment-status ${localPreviewStatusClass(status)}`;

    let message = '記事を選択すると未保存内容をshadow workspaceへ同期します。';
    if (status === 'ready') message = 'Hugo Live Preview is ready. 編集内容はLiveReloadで反映されます。';
    else if (status === 'starting') message = 'Hugoを起動しています…';
    else if (status === 'failed') message = state?.process_error || 'Hugo Live Previewの起動に失敗しました。';
    else if (status === 'stale') message = '以前のタブのpreview sessionが期限切れです。安全に回収して再開できます。';
    else if (status === 'conflict') message = 'このsiteのLocal Live Previewは別のタブで使用中です。';
    else if (state?.session_active && state?.session_owned) message = '未保存内容は同期済みです。Previewを開くとHugoを起動します。';
    else if (state?.session_active) message = 'このsiteには別の編集sessionがあります。';
    messageEl.textContent = message;

    if (reclaimBtn) reclaimBtn.classList.toggle('hidden', !state?.session_stale);
    if (stopBtn) {
        const processRunning = state?.process_state && state.process_state !== 'stopped';
        stopBtn.classList.toggle('hidden', !(state?.session_owned || (!state?.session_active && processRunning)));
    }
}

async function ensureLocalPreviewSession() {
    if (!localPreviewEnabled || !Editor.getCurrentPath()) return null;
    try {
        const result = await Editor.refreshLocalLivePreview();
        if (result?.session_id) localPreviewSessionID = result.session_id;
        return result;
    } catch (e) {
        if (e?.status === 409) {
            renderLocalPreviewState({
                ...(localPreviewState || {}),
                enabled: true,
                status: 'conflict',
                session_active: true,
                session_owned: false,
            });
        }
        throw e;
    }
}

function stopLocalPreviewMonitoring() {
    if (localPreviewPollTimer) clearTimeout(localPreviewPollTimer);
    if (localPreviewHeartbeatTimer) clearTimeout(localPreviewHeartbeatTimer);
    localPreviewPollTimer = null;
    localPreviewHeartbeatTimer = null;
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
    if (!localPreviewHeartbeatTimer) {
        localPreviewHeartbeatTimer = setTimeout(async () => {
            localPreviewHeartbeatTimer = null;
            if (localPreviewSessionID) {
                try {
                    await API.heartbeatLocalPreviewContent(localPreviewSessionID);
                } catch (e) {
                    if (e?.status !== 409) console.error('[LocalPreview] heartbeat failed', e);
                }
            }
            scheduleLocalPreviewMonitoring();
        }, LOCAL_PREVIEW_HEARTBEAT_MS);
    }
}

async function refreshLocalPreviewStatus() {
    if (!localPreviewEnabled) return null;
    if (localPreviewController) localPreviewController.abort();
    const controller = new AbortController();
    localPreviewController = controller;
    try {
        const state = await API.fetchLocalPreviewStatus(localPreviewSessionID, controller.signal);
        renderLocalPreviewState(state);
        return state;
    } catch (e) {
        if (e?.name !== 'AbortError') console.error('[LocalPreview] status failed', e);
        return null;
    } finally {
        if (localPreviewController === controller) localPreviewController = null;
    }
}

async function openLocalLivePreview() {
    const url = localPreviewURL();
    if (!localPreviewEnabled || !url) {
        UI.showToast('Local Live Preview is not configured', 'warning');
        return;
    }
    try {
        if (Editor.getCurrentPath()) await ensureLocalPreviewSession();
        window.open(url, '_blank', 'noopener');
        setTimeout(() => refreshLocalPreviewStatus(), 500);
    } catch (e) {
        UI.showToast('Local Live Previewを開けません: ' + e.message, 'error');
    }
}

async function toggleEmbeddedLocalPreview() {
    const wrapper = document.getElementById('local-preview-embed');
    const frame = document.getElementById('local-preview-frame');
    const btn = document.getElementById('local-preview-embed-btn');
    if (!wrapper || !frame || !btn) return;
    if (!wrapper.classList.contains('hidden')) {
        closeEmbeddedLocalPreview();
        return;
    }

    const url = localPreviewURL();
    if (!url) return UI.showToast('Local Live Preview URL is unavailable', 'warning');
    try {
        if (Editor.getCurrentPath()) await ensureLocalPreviewSession();
        frame.src = url;
        wrapper.classList.remove('hidden');
        btn.textContent = '埋め込みを閉じる';
        setTimeout(() => refreshLocalPreviewStatus(), 500);
    } catch (e) {
        UI.showToast('埋め込みpreviewを開始できません: ' + e.message, 'error');
    }
}

function closeEmbeddedLocalPreview() {
    const wrapper = document.getElementById('local-preview-embed');
    const frame = document.getElementById('local-preview-frame');
    const btn = document.getElementById('local-preview-embed-btn');
    if (wrapper) wrapper.classList.add('hidden');
    if (frame) frame.src = 'about:blank';
    if (btn) btn.textContent = '埋め込み表示';
}

async function stopLocalLivePreview() {
    if (!localPreviewEnabled || localPreviewOperationInProgress) return;
    localPreviewOperationInProgress = true;
    try {
        const released = await Editor.releaseLocalLivePreview();
        localPreviewSessionID = "";
        if (!released) await API.stopLocalPreviewContent("");
        closeEmbeddedLocalPreview();
        UI.showToast('Local Live Previewを停止しました', 'success');
    } catch (e) {
        UI.showToast('Local Live Previewを停止できません: ' + e.message, 'error');
    } finally {
        localPreviewOperationInProgress = false;
        await refreshLocalPreviewStatus();
    }
}

async function reclaimLocalLivePreview() {
    if (!localPreviewEnabled || localPreviewOperationInProgress) return;
    if (!confirm('期限切れのLocal Live Preview sessionを回収して再開しますか？')) return;
    localPreviewOperationInProgress = true;
    try {
        await API.reclaimStaleLocalPreview();
        localPreviewSessionID = "";
        if (Editor.getCurrentPath()) await ensureLocalPreviewSession();
        UI.showToast('Local Live Preview sessionを回収しました', 'success');
    } catch (e) {
        UI.showToast('Local Live Previewを回収できません: ' + e.message, 'error');
    } finally {
        localPreviewOperationInProgress = false;
        await refreshLocalPreviewStatus();
    }
}

async function runSync() {
    if (!confirm("GitHubから最新の状態を取得しますか？\n（ローカルの未保存の変更は注意してください）")) return;

    const btn = document.querySelector('button[onclick="runSync()"]');
    const originalText = btn ? btn.textContent : "Sync";
    if (btn) btn.textContent = "Syncing...";

    try {
        const data = await API.runSync();
        if (data.status === 'ok') {
            UI.showToast("Sync Complete", "success");
            await refreshFileList();
        } else {
            UI.showToast("Sync Error: " + data.log, "error");
        }
    } catch (e) {
        UI.showToast("Network Error", "error");
    } finally {
        if (btn) btn.textContent = originalText;
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
        const data = await API.runPublish(path, draftID);
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
        const state = await API.triggerPreviewDeployment(path, Editor.getDraftID());
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
        const state = await API.retryPreviewDeployment(Editor.getDraftID());
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
        await API.discardPreviewDeployment(Editor.getDraftID());
        Editor.resetDraftID();
        applyDeploymentState(null);
        UI.showToast("デプロイプレビューを破棄しました", "success");
    } catch (e) {
        UI.showToast(e.message, "error");
    } finally {
        deploymentOperationInProgress = false;
    }
}
