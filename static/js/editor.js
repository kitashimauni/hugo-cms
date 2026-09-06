import * as API from './api.js';
import * as UI from './ui.js';

let currentPath = "";
let currentData = null;
let cmsConfig = null;
let autoSaveTimer = null;
let lastSavedPayload = "";
let lastQueuedPayload = "";
let saveQueue = Promise.resolve();
let deletingPath = "";
let previewTimer = null;
let previewController = null;
let previewRevision = 0;
let localPreviewTimer = null;
let localPreviewRevision = 0;
let localPreviewSessionID = "";
let localPreviewSessionPath = "";
let localPreviewConflictNotified = false;
const localPreviewInflight = new Set();

const PREVIEW_DEBOUNCE_MS = 180;
const LOCAL_PREVIEW_DEBOUNCE_MS = 250;

export function getCurrentPath() {
    return currentPath;
}

function draftStorageKey(siteID, path) {
    return `hugo-cms:draft:${siteID}:${path}`;
}

function localPreviewStorageKey(siteID, path) {
    return `hugo-cms:local-preview:${siteID}:${path}`;
}

export function createDraftUUID(cryptoProvider = window.crypto) {
    if (typeof cryptoProvider?.randomUUID === 'function') {
        return cryptoProvider.randomUUID();
    }
    if (typeof cryptoProvider?.getRandomValues !== 'function') {
        throw new Error('Secure random number generation is unavailable');
    }

    const bytes = new Uint8Array(16);
    cryptoProvider.getRandomValues(bytes);
    bytes[6] = (bytes[6] & 0x0f) | 0x40; // UUID version 4
    bytes[8] = (bytes[8] & 0x3f) | 0x80; // RFC 4122 variant

    const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, '0'));
    return `${hex.slice(0, 4).join('')}-${hex.slice(4, 6).join('')}-${hex.slice(6, 8).join('')}-${hex.slice(8, 10).join('')}-${hex.slice(10).join('')}`;
}

export function getOrCreateDraftID(siteID, path, storage = window.sessionStorage, createUUID = createDraftUUID) {
    if (!siteID || !path) return "";
    const key = draftStorageKey(siteID, path);
    let draftID = storage.getItem(key);
    if (!draftID) {
        draftID = createUUID();
        storage.setItem(key, draftID);
    }
    return draftID;
}

function getOrCreateLocalPreviewSessionID(siteID, path, storage = window.sessionStorage, createUUID = createDraftUUID) {
    if (!siteID || !path) return "";
    const key = localPreviewStorageKey(siteID, path);
    let sessionID = storage.getItem(key);
    if (!sessionID) {
        sessionID = createUUID();
        storage.setItem(key, sessionID);
    }
    return sessionID;
}

export function getDraftID() {
    return getOrCreateDraftID(API.getCurrentSite(), currentPath);
}

export function resetDraftID() {
    if (currentPath) window.sessionStorage.removeItem(draftStorageKey(API.getCurrentSite(), currentPath));
}

export function setConfig(cfg) {
    cmsConfig = cfg;
}

function localPreviewEnabled() {
    return cmsConfig?._cms?.local_preview?.enabled === true;
}

export function clearEditor() {
    clearAutoSaveTimer();
    cancelMarkdownPreview();
    cancelLocalPreviewTimer();
    resetLocalPreviewClientState();
    currentPath = "";
    currentData = null;
    lastSavedPayload = "";
    lastQueuedPayload = "";
    deletingPath = "";

    const display = document.getElementById('filename-display');
    if (display) display.textContent = "Select a file...";

    const editor = document.getElementById('editor');
    if (editor) {
        editor.value = "";
        editor.placeholder = "Select a file to edit...";
        editor.disabled = false;
    }

    const fmContainer = document.getElementById('fm-container');
    if (fmContainer) {
        fmContainer.innerHTML = "";
        fmContainer.style.display = 'none';
    }

    UI.clearMarkdownPreview();
}

export function initAutoSave() {
    const editor = document.getElementById('editor');
    const fmContainer = document.getElementById('fm-container');

    if (editor) editor.addEventListener('input', handleEditorChange);
    if (fmContainer) {
        fmContainer.addEventListener('input', handleEditorChange);
        fmContainer.addEventListener('change', handleEditorChange);
    }
}

function handleEditorChange() {
    triggerAutoSave();
    scheduleMarkdownPreview();
    scheduleLocalLivePreview();
    if (window.markDeploymentPreviewStale) window.markDeploymentPreviewStale();
}

function triggerAutoSave() {
    if (!currentPath) return;
    if (currentPath === deletingPath) return;
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

function cancelMarkdownPreview() {
    if (previewTimer) {
        clearTimeout(previewTimer);
        previewTimer = null;
    }
    if (previewController) {
        previewController.abort();
        previewController = null;
    }
    previewRevision += 1;
}

function scheduleMarkdownPreview() {
    if (!currentPath || currentPath === deletingPath) return;
    if (previewTimer) clearTimeout(previewTimer);
    previewTimer = setTimeout(() => {
        previewTimer = null;
        refreshMarkdownPreview().catch(() => undefined);
    }, PREVIEW_DEBOUNCE_MS);
}

export async function refreshMarkdownPreview() {
    if (!currentPath || currentPath === deletingPath) {
        UI.clearMarkdownPreview();
        return;
    }

    if (previewTimer) {
        clearTimeout(previewTimer);
        previewTimer = null;
    }
    if (previewController) previewController.abort();

    const requestPath = currentPath;
    const revision = ++previewRevision;
    previewController = new AbortController();
    const payload = {
        path: requestPath,
        body: document.getElementById('editor')?.value || "",
        frontmatter: UI.collectFrontMatter()
    };
    UI.showMarkdownPreviewLoading();

    try {
        const data = await API.renderMarkdownPreview(payload, previewController.signal);
        if (revision !== previewRevision || requestPath !== currentPath) return;
        UI.renderMarkdownPreview(typeof data?.html === 'string' ? data.html : "");
    } catch (e) {
        if (e?.name === 'AbortError' || revision !== previewRevision) return;
        UI.showMarkdownPreviewError(e);
        throw e;
    } finally {
        if (revision === previewRevision) previewController = null;
    }
}

function cancelLocalPreviewTimer() {
    if (localPreviewTimer) {
        clearTimeout(localPreviewTimer);
        localPreviewTimer = null;
    }
}

function resetLocalPreviewClientState() {
    localPreviewRevision = 0;
    localPreviewSessionID = "";
    localPreviewSessionPath = "";
    localPreviewConflictNotified = false;
}

function scheduleLocalLivePreview() {
    if (!localPreviewEnabled() || !currentPath || currentPath === deletingPath) return;
    if (localPreviewTimer) clearTimeout(localPreviewTimer);
    localPreviewTimer = setTimeout(() => {
        localPreviewTimer = null;
        refreshLocalLivePreview().catch(() => undefined);
    }, LOCAL_PREVIEW_DEBOUNCE_MS);
}

export async function refreshLocalLivePreview() {
    if (!localPreviewEnabled() || !currentPath || currentPath === deletingPath) return null;
    cancelLocalPreviewTimer();

    const requestPath = currentPath;
    const siteID = API.getCurrentSite();
    const sessionID = localPreviewSessionID || getOrCreateLocalPreviewSessionID(siteID, requestPath);
    if (!sessionID) return null;

    if (localPreviewSessionPath && localPreviewSessionPath !== requestPath) {
        throw new Error("Local Live Preview session path changed without release");
    }
    localPreviewSessionID = sessionID;
    localPreviewSessionPath = requestPath;
    const revision = ++localPreviewRevision;
    const payload = getPayload();

    const request = API.updateLocalPreviewContent(payload, sessionID, revision);
    localPreviewInflight.add(request);
    try {
        return await request;
    } catch (e) {
        if (e?.status === 409) {
            if (!localPreviewConflictNotified) {
                localPreviewConflictNotified = true;
                UI.showToast("Local Live Preview is active in another tab for this site", "warning");
            }
        } else {
            console.error("[LocalPreview] Update failed:", e);
        }
        throw e;
    } finally {
        localPreviewInflight.delete(request);
    }
}

export async function releaseLocalLivePreview() {
    cancelLocalPreviewTimer();
    const sessionID = localPreviewSessionID;
    if (!sessionID) {
        resetLocalPreviewClientState();
        return false;
    }

    // Do not race release against an update that the server may still be
    // applying even when the UI has moved on to another article/site.
    await Promise.allSettled(Array.from(localPreviewInflight));
    try {
        const result = await API.releaseLocalPreviewContent(sessionID);
        return result?.released === true;
    } catch (e) {
        // A different tab may own the site's active session. Our stale tab must
        // not release that workspace, but it can safely forget its local state.
        if (e?.status !== 409) throw e;
        return false;
    } finally {
        resetLocalPreviewClientState();
    }
}

export async function execAutoSave() {
    return queueCurrentSave("Auto Saving...");
}

async function queueCurrentSave(statusMessage) {
    while (currentPath && currentPath !== deletingPath) {
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
        const operation = saveQueue.then(async () => {
            updateSaveStatus(statusMessage, "saving");
            try {
                await API.saveArticle(payloadObj);
                lastSavedPayload = payloadStr;
                console.log("[AutoSave] Saved:", payloadObj.path);
                updateSaveStatus("Saved", "saved");
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

        // Return the rejecting operation to its caller, but keep the queue tail
        // fulfilled so an old failure cannot block a later no-op, retry,
        // preview, or publish.
        saveQueue = operation.catch(() => undefined);
        return operation;
    }
    return false;
}

export async function flushPendingSave() {
    clearAutoSaveTimer();
    if (currentPath && currentPath === deletingPath) {
        throw new Error("Article deletion is in progress");
    }
    await queueCurrentSave("Saving before publish...");
}

export async function loadFile(path) {
    clearAutoSaveTimer();
    cancelMarkdownPreview();
    cancelLocalPreviewTimer();
    await saveQueue.catch(() => {
        // Loading another file remains possible after a failed save.
    });

    if (currentPath && currentPath !== path) {
        try {
            await releaseLocalLivePreview();
        } catch (e) {
            UI.showToast("Failed to release Local Live Preview: " + e.message, "error");
            return;
        }
    }

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
        await refreshMarkdownPreview();
        refreshLocalLivePreview().catch(() => undefined);

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
    if (currentPath === deletingPath) {
        return UI.showToast("Article deletion is in progress", "warning");
    }

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
    if (currentPath === deletingPath) {
        return UI.showToast("Article deletion is already in progress", "warning");
    }

    if (!confirm("Are you sure you want to delete this article?\nThis action cannot be undone.")) return;

    const pathToDelete = currentPath;
    let deleted = false;
    clearAutoSaveTimer();
    cancelLocalPreviewTimer();
    deletingPath = pathToDelete;

    try {
        // Let a save that already reached the server finish, then prevent all
        // queued saves for this path from starting before DELETE.
        await saveQueue;
        await API.deleteArticle(pathToDelete);
        await releaseLocalLivePreview();
        deleted = true;
        UI.showToast("Article deleted", "success");

        if (currentPath === pathToDelete) {
            cancelMarkdownPreview();
            currentPath = "";
            currentData = null;
            lastSavedPayload = "";
            lastQueuedPayload = "";
            document.getElementById('filename-display').textContent = "Select a file...";
            document.getElementById('editor').value = "";
            document.getElementById('fm-container').style.display = 'none';
            UI.clearMarkdownPreview();
        }

        if (refreshListCb) await refreshListCb();
    } catch (e) {
        UI.showToast("Delete failed: " + e.message, "error");
    } finally {
        if (deletingPath === pathToDelete) {
            deletingPath = "";
        }
        if (!deleted && currentPath === pathToDelete) {
            handleEditorChange();
        }
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
