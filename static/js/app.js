import * as API from './api.js';
import * as UI from './ui.js';

let currentPath = "";
let currentData = null; 
let cmsConfig = null;

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
    window.switchTab = UI.switchTab;
    window.closeModal = UI.closeModal;
    
    // Core Functions
    window.loadFile = loadFile;
    window.saveFile = saveFile;
    window.createNewFile = createNewFile;
    window.deleteFile = deleteFile;
    window.runSync = runSync;
    window.runBuild = runBuild;
    window.runPublish = runPublish;
    window.resetChanges = resetChanges;
    window.showDiff = showDiff;
}

async function refreshFileList() {
    const files = await API.fetchArticles();
    if (files) {
        UI.renderFileList(files, cmsConfig);
    }
}

async function loadFile(path) {
    currentPath = path;
    document.getElementById('filename-display').textContent = path;

    await UI.showLoadingEditor();

    try {
        const data = await API.fetchArticle(path);
        currentData = data;
        UI.updateEditorContent(data, path, cmsConfig);
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
    
    const btn = document.querySelector('button[onclick="saveFile()"]');
    const originalText = btn.textContent;
    btn.textContent = "Saving...";

    try {
        const payload = getPayload();
        await API.saveArticle(payload);
    } catch(e) {
        alert("Error saving: " + e);
    } finally {
        btn.textContent = originalText;
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
        UI.switchTab('files'); // Switch back to file list
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
            
            // res.path is the relative path of the created file
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

async function runBuild() {
    const btn = document.querySelector('button[onclick="runBuild()"]');
    btn.textContent = "Building...";
    btn.disabled = true;

    try {
        const data = await API.runBuild();
        if (data.status === 'ok') {
            UI.setPreviewUrl(currentPath);
            UI.switchTab('preview', document.querySelectorAll('nav button')[2]);
        } else {
            alert("Build Error:\n" + data.log);
        }
    } catch(e) {
        alert("Network Error");
    } finally {
        btn.textContent = "Build";
        btn.disabled = false;
    }
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
        btn.textContent = "Publish";
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