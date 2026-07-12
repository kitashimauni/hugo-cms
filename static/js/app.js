import * as API from './api.js';
import * as UI from './ui.js';
import * as Editor from './editor.js';

// Global State
let cmsConfig = null;
let siteRegistry = null;
let publishInProgress = false;

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
    window.loadFile = Editor.loadFile;
    window.buildAndPreview = async () => {
        try {
            await Editor.flushPendingSave();
        } catch (e) {
            UI.showToast("Preview update cancelled: save failed", "error");
        }
    };
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
    window.runPublish = runPublish;
    window.publishFile = publishFile;
    window.restartPreview = async () => {
        if (!confirm("Restart Hugo Server? (This helps if preview is stuck)")) return;
        UI.showToast("Restarting server...", "info");
        try {
            await API.restartHugo();
            UI.showToast("Server Restarted", "success");
            // Reload iframe
            const currentPath = Editor.getCurrentPath();
            if (currentPath) UI.setPreviewUrl(currentPath, cmsConfig, UI.collectFrontMatter());
        } catch (e) {
            UI.showToast("Restart Failed", "error");
        }
    };

    console.log("Hugo CMS Initialized");
}

async function loadSiteData() {
    cmsConfig = await API.fetchConfig();
    Editor.setConfig(cmsConfig);
    UI.renderConfigWarnings(cmsConfig);
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
    // Trigger save to ensure preview is up to date
    if (viewName === 'preview') {
        try {
            await Editor.flushPendingSave();
        } catch (e) {
            UI.showToast("Preview cancelled: save failed", "error");
            return;
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

async function runPublish(path = null) {
    if (publishInProgress) {
        UI.showToast("Publish is already running", "warning");
        return;
    }

    const isSingle = !!path;
    const msg = isSingle
        ? "このファイルの変更をGitHubにPushして公開しますか？"
        : "全ての変更をGitHubにPushして公開しますか？";

    if (!confirm(msg)) return;
    publishInProgress = true;

    // UI Feedback
    let btnSelector = 'button[onclick="runPublish()"]';
    if (isSingle) {
        btnSelector = 'button[onclick="publishFile()"], button[onclick="publishFile(); toggleHeaderMenu()"]';
    }

    const btn = document.querySelector(btnSelector);
    let originalText = "";
    if (btn) {
        originalText = btn.innerHTML;
        btn.innerHTML = isSingle ? "🚀..." : "Pushing...";
        btn.disabled = true;
    }

    try {
        await Editor.flushPendingSave();
        const data = await API.runPublish(path);
        if (data.status === 'ok') {
            UI.showToast("Published Successfully! 🚀", "success");
            // Refresh file list to update dirty flags
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
    await runPublish(currentPath);
}
