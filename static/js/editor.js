import * as API from './api.js';
import * as UI from './ui.js';

let currentPath = "";
let currentData = null;
let cmsConfig = null;
let autoSaveTimer = null;
let lastSavedPayload = "";
let lastQueuedPayload = "";
let saveQueue = Promise.resolve();

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
    clearAutoSaveTimer();

    // Debounce 3 seconds
    autoSaveTimer = setTimeout(() => {
        autoSaveTimer = null;
        execAutoSave().catch(() => {
            // The editor status already reports the failure. Avoid an
            // unhandled rejection from the timer callback.
        });
    }, 3000);
}

function clearAutoSaveTimer() {
    if (autoSaveTimer) {
        clearTimeout(autoSaveTimer);
        autoSaveTimer = null;
    }
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
    return queueCurrentSave("Auto Saving...");
}

async function queueCurrentSave(statusMessage) {
    while (currentPath) {
        // Another payload may become the saved value while we wait. Read the
        // editor again afterwards so preview/publish always uses what is
        // currently visible, including a revert to an older payload.
        if (lastQueuedPayload !== "") {
            await saveQueue;
            continue;
        }

        const payloadObj = getPayload();
        const payloadStr = JSON.stringify(payloadObj);

        if (payloadStr === lastSavedPayload) {
            return false;
        }

        lastQueuedPayload = payloadStr;
        const operation = saveQueue.catch(() => {
            // A newer payload should still be allowed to save after an earlier
            // request failed.
        }).then(async () => {
            updateSaveStatus(statusMessage, "saving");
            try {
                await API.saveArticle(payloadObj);
                lastSavedPayload = payloadStr;
                console.log("[AutoSave] Saved:", payloadObj.path);
                updateSaveStatus("Saved", "saved");
                reloadPreviewIfNeeded();
                return true;
            } catch (e) {
                console.error("[AutoSave] Failed:", e);
                updateSaveStatus("Save Failed", "error");
                throw e;
            } finally {
                if (lastQueuedPayload === payloadStr) {
                    lastQueuedPayload = "";
                }
            }
        });

        saveQueue = operation;
        return operation;
    }
    return false;
}

export async function flushPendingSave() {
    clearAutoSaveTimer();
    await queueCurrentSave("Saving before publish...");
    await saveQueue;
}

export async function loadFile(path) {
    clearAutoSaveTimer();
    await saveQueue.catch(() => {
        // Loading another file remains possible after a failed save.
    });

    currentPath = path;
    const display = document.getElementById('filename-display');
    if (display) display.textContent = path;

    await UI.showLoadingEditor();

    try {
        const data = await API.fetchArticle(path);
        currentData = data;
        UI.updateEditorContent(data, path, cmsConfig);

        lastSavedPayload = JSON.stringify(getPayload());
        lastQueuedPayload = "";
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

    clearAutoSaveTimer();

    try {
        await queueCurrentSave("Saving...");
        UI.showToast("File saved successfully", "success");
    } catch (e) {
        UI.showToast("Error saving: " + e.message, "error");
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

export function insertText(text) {
    const editor = document.getElementById('editor');
    if (!editor) return;

    const start = editor.selectionStart;
    const end = editor.selectionEnd;
    const val = editor.value;

    editor.value = val.substring(0, start) + text + val.substring(end);
    editor.selectionStart = editor.selectionEnd = start + text.length;
    editor.focus();
    
    // Trigger input event for auto-save
    editor.dispatchEvent(new Event('input'));
}

export async function showDiff() {
    if (!currentPath) return;
    const payload = getPayload();
    try {
        const data = await API.getDiff(payload);
        UI.showDiffModal(data.diff);
    } catch (e) {
        UI.showToast("Failed to get diff: " + e.message, "error");
    }
}
