import * as API from './api.js';
import * as UI from './ui.js';
import * as Editor from './editor.js';

// Global State
let cmsConfig = null;

// Initialization
init();

async function init() {
    try {
        cmsConfig = await API.fetchConfig();
        Editor.setConfig(cmsConfig);
    } catch (e) {
        console.error("Config fetch failed", e);
        UI.showToast("Failed to load configuration", "error");
    }

    await refreshFileList();
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
        await Editor.execAutoSave();
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
        if (currentPath && cmsConfig && cmsConfig.collections) {
            for (const col of cmsConfig.collections) {
                const colFolder = col.folder.replace(/^content\//, '');
                if (currentPath.startsWith(colFolder + "/") || currentPath === colFolder) {
                    collectionName = col.name;
                    break;
                }
            }
        }

        UI.showMediaLibrary((file) => {
            const markdown = `![${file.name}](${file.path})`;
            Editor.insertText(markdown);
        }, collectionName, currentPath);
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
            if (currentPath) UI.setPreviewUrl(currentPath);
        } catch (e) {
            UI.showToast("Restart Failed", "error");
        }
    };

    console.log("Hugo CMS Initialized");
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
        await Editor.execAutoSave();
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
    const isSingle = !!path;
    const msg = isSingle
        ? "このファイルの変更をGitHubにPushして公開しますか？"
        : "全ての変更をGitHubにPushして公開しますか？";

    if (!confirm(msg)) return;

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
        const data = await API.runPublish(path);
        if (data.status === 'ok') {
            UI.showToast("Published Successfully! 🚀", "success");
            // Refresh file list to update dirty flags
            await refreshFileList();
        } else {
            UI.showToast("Publish Error: " + data.log, "error");
        }
    } catch (e) {
        UI.showToast("Network Error", "error");
    } finally {
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