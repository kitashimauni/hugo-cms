import * as API from './api.js';
import * as UI from './ui.js';

let currentPath = "";
let currentData = null;
let cmsConfig = null;
let autoSaveTimer = null;
let lastSavedPayload = "";

export function getCurrentPath() {
    return currentPath;
}

export function setConfig(cfg) {
    cmsConfig = cfg;
}

export function initAutoSave() {
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
        setTimeout(() => { if (el.textContent === msg) el.textContent = ''; }, 2000);
    }
    else if (type === 'error') el.style.color = '#d67a7a';
    else el.style.color = '#888';
}

function reloadPreviewIfNeeded() {
    if (currentPath) UI.setPreviewUrl(currentPath);
}

export async function execAutoSave() {
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
    } catch (e) {
        console.error("[AutoSave] Failed:", e);
        updateSaveStatus("Save Failed", "error");
    }
}

export async function loadFile(path) {
    if (autoSaveTimer) clearTimeout(autoSaveTimer);

    currentPath = path;
    const display = document.getElementById('filename-display');
    if (display) display.textContent = path;

    await UI.showLoadingEditor();

    try {
        const data = await API.fetchArticle(path);
        currentData = data;
        UI.updateEditorContent(data, path, cmsConfig);

        lastSavedPayload = JSON.stringify(getPayload());
        UI.setPreviewUrl(path);

    } catch (e) {
        UI.showEditorError(e);
        UI.showToast("Failed to load file: " + e.message, "error");
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

export async function saveFile() {
    if (!currentPath) return UI.showToast("No file selected", "warning");

    if (autoSaveTimer) clearTimeout(autoSaveTimer);

    updateSaveStatus("Saving...", "saving");

    try {
        const payload = getPayload();
        await API.saveArticle(payload);
        lastSavedPayload = JSON.stringify(payload);
        updateSaveStatus("Saved", "saved");
        UI.showToast("File saved successfully", "success");
        reloadPreviewIfNeeded();
    } catch (e) {
        UI.showToast("Error saving: " + e.message, "error");
        updateSaveStatus("Error", "error");
    }
}

export async function deleteFile(refreshListCb) {
    if (!currentPath) return UI.showToast("No file selected", "warning");

    if (!confirm("Are you sure you want to delete this article?\nThis action cannot be undone.")) return;

    try {
        await API.deleteArticle(currentPath);
        UI.showToast("Article deleted", "success");

        currentPath = "";
        currentData = null;
        document.getElementById('filename-display').textContent = "Select a file...";
        document.getElementById('editor').value = "";
        document.getElementById('fm-container').style.display = 'none';

        if (refreshListCb) await refreshListCb();
    } catch (e) {
        UI.showToast("Delete failed: " + e.message, "error");
    }
}

export async function createNewFile(refreshListCb) {
    if (!cmsConfig) {
        UI.showToast("Config not loaded", "error");
        return;
    }

    UI.showCreationModal(cmsConfig, async (colName, fields) => {
        try {
            const res = await API.createArticle({
                collection: colName,
                fields: fields
            });

            if (res.status === 'created') {
                if (refreshListCb) await refreshListCb();
                if (res.path) {
                    await loadFile(res.path);
                    UI.showToast("File created successfully", "success");
                }
            }
        } catch (e) {
            UI.showToast("Create failed: " + e.message, "error");
        }
    });
}

export async function resetChanges() {
    if (!currentPath) return;
    if (!confirm("Are you sure you want to discard all changes?")) return;
    await loadFile(currentPath);
    UI.showToast("Changes discarded", "info");
}

export async function showDiff() {
    if (!currentPath) return;
    const payload = getPayload();
    try {
        const data = await API.getDiff(payload);
        let html = data.diff.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
        html = html.split('\n').map(line => {
            if (line.startsWith('+')) return `<span class="diff-added">${line}</span>`;
            if (line.startsWith('-')) return `<span class="diff-removed">${line}</span>`;
            return line;
        }).join('\n');

        UI.showDiffModal(html);
    } catch (e) {
        UI.showToast("Failed to get diff: " + e.message, "error");
    }
}
