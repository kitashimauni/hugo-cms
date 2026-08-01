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

// Initialization
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

    // --- Expose functions to Global Scope for HTML onclick handlers ---

    // UI
    window.switchView = switchView;
    window.toggleSplitView = UI.toggleSplitView;
    window.toggleSidebar = UI.toggleSidebar;
    window.toggleHeaderMenu = UI.toggleHeaderMenu;
    window.closeModal = UI.closeModal;

    // Editor
    window.loadFile = loadFile;
    window.buildAndPreview = () => Editor.refreshMarkdownPreview();
    window.saveFile = async () => {
        await Editor.saveFile();
        await refreshFileList();
    };
    window.createNewFile = () => Editor.createNewFile(refreshFileList);
    window.deleteFile = () => Editor.deleteFile(refreshFileList);
    window.insertImage = () => {
        const currentPath = Editor.getCurrentPath();
        let collectionName = null;
        const collection = UI.getCollectionForPath(currentPath, cmsConfig);
        if (collection) {
            collectionName = collection.name;
        }

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
                    // Regex for ${1:default} or ${1}
                    const regex = /\$\{(\d+)(?::([^}]*))?\}/g;
                    let match;
                    while ((match = regex.exec(body)) !== null) {
                        const id = match[1];
                        const def = match[2] || "";
                        if (!vars.has(id)) {
                            vars.set(id, { id, label: def || `Param ${id}`, default: def });
                        }
                    }
    
                    if (vars.size > 0) {
                        const varList = Array.from(vars.values()).sort((a, b) => a.id - b.id);
                        UI.showSnippetInputModal(varList, (values) => {
                            let finalBody = body.replace(regex, (m, id, def) => {
                                return values[id] !== undefined ? values[id] : (def || "");
                            });
                            Editor.insertText(finalBody);
                        }, showList); // Pass showList as onBack
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

    // Actions
    window.runSync = runSync;
    window.publishFile = publishFile;
    window.updateDeploymentPreview = updateDeploymentPreview;
    window.retryDeploymentPreview = retryDeploymentPreview;
    window.discardDeploymentPreview = discardDeploymentPreview;
    window.markDeploymentPreviewStale = markDeploymentPreviewStale;

    console.log("Hugo CMS Initialized");
}

async function loadSiteData() {
    cmsConfig = await API.fetchConfig();
    Editor.setConfig(cmsConfig);
    UI.renderConfigWarnings(cmsConfig);
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

    stopDeploymentPolling();
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
    if (deploymentEnabled && Editor.getCurrentPath() === path) {
        await refreshDeploymentState();
    }
}

async function refreshFileList() {
    try {
        const files = await API.fetchArticles();
        if (files) {
            UI.renderFileList(files, cmsConfig);
        }
    } catch (e) {
        UI.showToast("Failed to fetch file list", "error");
    }
}

async function switchView(viewName) {
    if (viewName === 'preview') {
        try {
            await Editor.refreshMarkdownPreview();
        } catch (e) {
            // The preview surface already shows the request error. Editing and
            // saving remain available even when rendering is temporarily down.
        }
    }
    UI.switchView(viewName);
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
        UI.showToast(e.message, 'error');
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
