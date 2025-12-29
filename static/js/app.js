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
    if (viewName === 'preview') {
        if (!currentPath) {
            alert("No file selected.");
            return;
        }
        await buildAndPreview();
    }
    UI.switchView(viewName);
}

async function buildAndPreview() {
    // Show some loading indicator if possible, or just wait
    // We could add a spinner to the preview area
    const frame = document.getElementById('preview-frame');
    // frame.src = "about:blank"; // Optional: clear or show loader

    try {
        const data = await API.runBuild();
        if (data.status === 'ok') {
            UI.setPreviewUrl(currentPath);
        } else {
            alert("Build Error:\n" + data.log);
        }
    } catch(e) {
        alert("Network Error during build");
    }
}

async function loadFile(path) {
    if (autoSaveTimer) clearTimeout(autoSaveTimer);
    
    currentPath = path;
    const display = document.getElementById('filename-display');
    if(display) display.textContent = path;

    await UI.showLoadingEditor(); // This switches to 'edit' view

    try {
        const data = await API.fetchArticle(path);
        currentData = data;
        UI.updateEditorContent(data, path, cmsConfig);
        
        // Initialize change tracking
        lastSavedPayload = JSON.stringify(getPayload());
        
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
