import * as API from './api.js';
import * as UI from './ui.js';

let currentPath = "";
let currentData = null; 
let cmsConfig = null;
let autoSaveTimer = null;
let lastSavedPayload = "";

// 初期化
init();

async function init() {
    try {
        cmsConfig = await API.fetchConfig();
    } catch(e) {
        console.error("Config fetch failed", e);
    }
    await refreshFileList();
    
    // UI Event Listeners (Global Functions)
    window.switchView = switchView;
    window.toggleSplitView = UI.toggleSplitView;
    window.toggleSidebar = UI.toggleSidebar;
    window.toggleHeaderMenu = UI.toggleHeaderMenu;
    window.closeModal = UI.closeModal;
    
    // Core Functions
    window.loadFile = loadFile;
    window.saveFile = saveFile;
    window.createNewFile = createNewFile;
    window.deleteFile = deleteFile;
    window.runSync = runSync;
    window.runPublish = runPublish;
    window.resetChanges = resetChanges;
    window.showDiff = showDiff;
    window.buildAndPreview = buildAndPreview;

    // Auto Save Listeners
    const editor = document.getElementById('editor');
    const fmContainer = document.getElementById('fm-container');

    if (editor) editor.addEventListener('input', triggerAutoSave);
    if (fmContainer) {
        fmContainer.addEventListener('input', triggerAutoSave);
        fmContainer.addEventListener('change', triggerAutoSave);
    }
}

function triggerAutoSave() {
    if (!currentPath) return;
    if (autoSaveTimer) clearTimeout(autoSaveTimer);
    
    // Debounce 3 seconds
    autoSaveTimer = setTimeout(execAutoSave, 3000);
}

function updateSaveStatus(msg, type) {
    const el = document.getElementById('save-status');
    if (!el) return;
    el.textContent = msg;
    if (type === 'saving') el.style.color = '#e2c08d';
    else if (type === 'saved') {
        el.style.color = '#81b181';
        setTimeout(() => { if(el.textContent === msg) el.textContent = ''; }, 2000);
    }
    else if (type === 'error') el.style.color = '#d67a7a';
    else el.style.color = '#888';
}

function reloadPreviewIfNeeded() {
    // Always refresh preview in background so it's ready when switched
    if (currentPath) UI.setPreviewUrl(currentPath);
}

async function execAutoSave() {
    if (!currentPath) return;
    
    const payloadObj = getPayload();
    const payloadStr = JSON.stringify(payloadObj);
    
    if (payloadStr === lastSavedPayload) {
        return; // No changes
    }

    updateSaveStatus("Auto Saving...", "saving");

    try {
        await API.saveArticle(payloadObj);
        lastSavedPayload = payloadStr;
        console.log("[AutoSave] Saved:", currentPath);
        updateSaveStatus("Saved", "saved");
        reloadPreviewIfNeeded();
    } catch(e) {
        console.error("[AutoSave] Failed:", e);
        updateSaveStatus("Save Failed", "error");
    }
}

async function refreshFileList() {
    const files = await API.fetchArticles();
    if (files) {
        UI.renderFileList(files, cmsConfig);
    }
}

async function switchView(viewName) {
    // Trigger save to ensure preview is up to date
    if (viewName === 'preview') {
        await execAutoSave();
    }
    UI.switchView(viewName);
}

async function buildAndPreview() {
    // Deprecated: logic moved to autoSave/background load
    await execAutoSave();
}

async function loadFile(path) {
    if (autoSaveTimer) clearTimeout(autoSaveTimer);
    
    currentPath = path;
    const display = document.getElementById('filename-display');
    if(display) display.textContent = path;

    await UI.showLoadingEditor(); // This updates editor text but doesn't force switch view

    try {
        const data = await API.fetchArticle(path);
        currentData = data;
        UI.updateEditorContent(data, path, cmsConfig);
        
        // Initialize change tracking
        lastSavedPayload = JSON.stringify(getPayload());

        // Always load preview in background
        UI.setPreviewUrl(path);
        
    } catch(e) {
        UI.showEditorError(e);
    }
}

function getPayload() {
    const payload = { path: currentPath };
    const fm = UI.collectFrontMatter();
    if (fm) {
        payload.frontmatter = fm;
        payload.body = document.getElementById('editor').value;
        payload.format = currentData.format || 'yaml';
    } else {
        payload.content = document.getElementById('editor').value;
    }
    return payload;
}

async function saveFile() {
    if(!currentPath) return alert("No file selected");
    
    if (autoSaveTimer) clearTimeout(autoSaveTimer);

    updateSaveStatus("Saving...", "saving");

    try {
        const payload = getPayload();
        await API.saveArticle(payload);
        lastSavedPayload = JSON.stringify(payload);
        updateSaveStatus("Saved", "saved");
        reloadPreviewIfNeeded();
    } catch(e) {
        alert("Error saving: " + e);
        updateSaveStatus("Error", "error");
    }
}

async function deleteFile() {
    if(!currentPath) return alert("No file selected");
    
    if(!confirm("Are you sure you want to delete this article?\nThis action cannot be undone.")) return;

    try {
        await API.deleteArticle(currentPath);
        alert("Article deleted.");
        
        // Reset UI
        currentPath = "";
        currentData = null;
        document.getElementById('filename-display').textContent = "Select a file...";
        document.getElementById('editor').value = "";
        document.getElementById('editor').placeholder = "Select a file to edit...";
        document.getElementById('fm-container').style.display = 'none';
        
        await refreshFileList();
        // Stay in edit view but empty
    } catch(e) {
        alert("Delete failed: " + e.message);
    }
}

async function createNewFile() {
    if (!cmsConfig) {
        alert("Config not loaded");
        return;
    }

    UI.showCreationModal(cmsConfig, async (colName, fields) => {
        try {
            const res = await API.createArticle({
                collection: colName,
                fields: fields
            });
            
            if (res.status === 'created') {
                await refreshFileList();
                if (res.path) {
                    await loadFile(res.path);
                } else {
                    alert("Created, but path not returned.");
                }
            }
        } catch(e) {
            alert("Create failed: " + e.message);
        }
    });
}

async function runSync() {
    if(!confirm("GitHubから最新の状態を取得しますか？\n（ローカルの未保存の変更は注意してください）")) return; 
    
    const btn = document.querySelector('button[onclick="runSync()"]');
    const originalText = btn.textContent;
    btn.textContent = "Syncing...";
    
    try {
        const data = await API.runSync();
        if(data.status === 'ok') {
            alert("Sync Complete!\n" + data.log);
            await refreshFileList();
        } else {
            alert("Sync Error:\n" + data.log);
        }
    } catch(e) {
        alert("Network Error");
    } finally {
        btn.textContent = originalText;
    }
}

async function runPublish() {
    if(!confirm("この記事の変更をGitHubにPushして公開しますか？")) return;

    const btn = document.querySelector('button[onclick="runPublish()"]');
    const originalText = btn.textContent;
    btn.textContent = "Pushing...";
    btn.disabled = true;

    try {
        const data = await API.runPublish();
        if(data.status === 'ok') {
            alert("Published Successfully! 🚀\nCloudflare Pages will deploy shortly.");
        } else {
            alert("Publish Error:\n" + data.log);
        }
    } catch(e) {
        alert("Network Error");
    } finally {
        btn.textContent = originalText;
        btn.disabled = false;
    }
}

async function resetChanges() {
    if(!currentPath) return;
    if(!confirm("Are you sure you want to discard all changes?")) return;
    await loadFile(currentPath);
}

async function showDiff() {
    if(!currentPath) return;
    const payload = getPayload();
    
    const data = await API.getDiff(payload);
    
    let html = data.diff.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    html = html.split('\n').map(line => {
        if(line.startsWith('+')) return `<span class="diff-added">${line}</span>`;
        if(line.startsWith('-')) return `<span class="diff-removed">${line}</span>`;
        return line;
    }).join('\n');
    
    UI.showDiffModal(html);
}
