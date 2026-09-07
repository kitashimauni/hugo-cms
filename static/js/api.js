// CSRF Token Management
let csrfToken = null;
let currentSite = window.localStorage.getItem('hugo-cms:site') || "";

export function getCurrentSite() {
    return currentSite;
}

export function setCurrentSite(siteID) {
    currentSite = siteID || "";
    if (currentSite) {
        window.localStorage.setItem('hugo-cms:site', currentSite);
    } else {
        window.localStorage.removeItem('hugo-cms:site');
    }
}

export function initializeCurrentSite(registry) {
    const sites = registry?.sites || [];
    const siteExists = currentSite && sites.some(site => site.id === currentSite);
    if (!siteExists) {
        setCurrentSite(registry?.default_site || sites[0]?.id || "");
    }
}

function withSite(url) {
    if (!currentSite) return url;
    const parsed = new URL(url, window.location.origin);
    parsed.searchParams.set('site', currentSite);
    return parsed.pathname + parsed.search;
}

function siteHeaders() {
    return currentSite ? { 'X-CMS-Site': currentSite } : {};
}

async function ensureCSRFToken(forceRefresh = false) {
    if (csrfToken && !forceRefresh) return csrfToken;
    const res = await fetch('/admin/api/csrf-token');
    if (!res.ok) throw new Error("Failed to fetch CSRF token");
    const data = await res.json();
    csrfToken = data.csrf_token;
    return csrfToken;
}

function getCSRFHeaders() {
    return csrfToken ? { 'X-CSRF-Token': csrfToken } : {};
}

function resetCSRFToken() {
    csrfToken = null;
}

async function fetchWithCSRF(url, options) {
    await ensureCSRFToken();
    const request = () => fetch(url, {
        ...options,
        headers: { ...(options.headers || {}), ...getCSRFHeaders() }
    });
    let res = await request();
    if (res.status === 403) {
        resetCSRFToken();
        await ensureCSRFToken(true);
        res = await request();
    }
    return res;
}

async function responseError(res, fallback) {
    let message = fallback;
    try {
        const data = await res.json();
        message = data?.message || data?.error || fallback;
    } catch (_) {
        // Keep the stable fallback when the server did not return JSON.
    }
    const error = new Error(message);
    error.status = res.status;
    return error;
}

export async function fetchConfig() {
    const res = await fetch(withSite('/admin/api/config'), { headers: siteHeaders() });
    if (!res.ok) throw new Error("Config fetch failed");
    return await res.json();
}

export async function fetchSites() {
    const res = await fetch('/admin/api/sites');
    if (!res.ok) throw new Error("Sites fetch failed");
    return await res.json();
}

export async function fetchArticles() {
    const res = await fetch(withSite('/admin/api/articles'), { headers: siteHeaders() });
    if (res.status === 401) {
        window.location.href = "/admin/login";
        return null;
    }
    return await res.json();
}

export async function fetchArticle(path) {
    const params = new URLSearchParams({ path });
    const res = await fetch(withSite(`/admin/api/article?${params.toString()}`), { headers: siteHeaders() });
    if (!res.ok) throw new Error("Failed to load article");
    return await res.json();
}

export async function saveArticle(payload) {
    await ensureCSRFToken();
    const res = await fetch(withSite('/admin/api/article'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders(),
            ...getCSRFHeaders()
        },
        body: JSON.stringify(payload)
    });
    if (res.status === 403) {
        resetCSRFToken();
        await ensureCSRFToken(true);
        const retryRes = await fetch(withSite('/admin/api/article'), {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                ...siteHeaders(),
                ...getCSRFHeaders()
            },
            body: JSON.stringify(payload)
        });
        if (!retryRes.ok) throw new Error("Save failed");
        return await retryRes.json();
    }
    if (!res.ok) throw new Error("Save failed");
    return await res.json();
}

export async function createArticle(arg1, arg2) {
    await ensureCSRFToken();
    let body;
    if (typeof arg1 === 'object') {
        body = arg1;
    } else {
        body = { path: arg1, content: arg2 };
    }

    const res = await fetch(withSite('/admin/api/create'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders(),
            ...getCSRFHeaders()
        },
        body: JSON.stringify(body)
    });
    if (!res.ok) {
        const data = await res.json();
        throw new Error(data.message || data.error || "Create failed");
    }
    return await res.json();
}

export async function deleteArticle(path) {
    await ensureCSRFToken();
    const res = await fetch(withSite('/admin/api/delete'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders(),
            ...getCSRFHeaders()
        },
        body: JSON.stringify({ path })
    });
    if (!res.ok) {
        const data = await res.json();
        throw new Error(data.message || data.error || "Delete failed");
    }
    return await res.json();
}

export async function getDiff(payload) {
    await ensureCSRFToken();
    const res = await fetch(withSite('/admin/api/diff'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders(),
            ...getCSRFHeaders()
        },
        body: JSON.stringify(payload)
    });
    return await res.json();
}

export async function runSync() {
    await ensureCSRFToken();
    const res = await fetch(withSite('/admin/api/sync'), {
        method: 'POST',
        headers: {
            ...siteHeaders(),
            ...getCSRFHeaders()
        }
    });
    return await res.json();
}

export async function runPublish(path, draftID) {
    const res = await fetchWithCSRF(withSite('/admin/api/publish'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders()
        },
        body: JSON.stringify({ path, draft_id: draftID })
    });
    if (!res.ok) throw new Error("Publish failed");
    return await res.json();
}

export async function renderMarkdownPreview(payload, signal) {
    const res = await fetchWithCSRF(withSite('/admin/api/preview/markdown'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders()
        },
        body: JSON.stringify(payload),
        signal
    });
    if (!res.ok) throw new Error("Markdown preview failed");
    return await res.json();
}

export async function updateLocalPreviewContent(payload, draftID, revision, signal) {
    const res = await fetchWithCSRF(withSite('/admin/api/preview/local'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders()
        },
        body: JSON.stringify({ ...payload, draft_id: draftID, revision }),
        signal
    });
    if (!res.ok) throw await responseError(res, "Local Live Preview update failed");
    return await res.json();
}

export async function releaseLocalPreviewContent(draftID) {
    const res = await fetchWithCSRF(withSite('/admin/api/preview/local/release'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders()
        },
        body: JSON.stringify({ draft_id: draftID })
    });
    if (!res.ok) throw await responseError(res, "Local Live Preview release failed");
    return await res.json();
}

export async function fetchLocalPreviewStatus(draftID = "", signal) {
    let url = '/admin/api/preview/local/status';
    if (draftID) url += `?draft_id=${encodeURIComponent(draftID)}`;
    const res = await fetch(withSite(url), {
        headers: siteHeaders(),
        signal
    });
    if (!res.ok) throw await responseError(res, "Local Live Preview status failed");
    return await res.json();
}

export async function heartbeatLocalPreviewContent(draftID) {
    const res = await fetchWithCSRF(withSite('/admin/api/preview/local/heartbeat'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders()
        },
        body: JSON.stringify({ draft_id: draftID })
    });
    if (!res.ok) throw await responseError(res, "Local Live Preview heartbeat failed");
    return await res.json();
}

export async function stopLocalPreviewContent(draftID = "") {
    const res = await fetchWithCSRF(withSite('/admin/api/preview/local/stop'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders()
        },
        body: JSON.stringify({ draft_id: draftID })
    });
    if (!res.ok) throw await responseError(res, "Local Live Preview stop failed");
    return await res.json();
}

export async function reclaimStaleLocalPreview() {
    const res = await fetchWithCSRF(withSite('/admin/api/preview/local/reclaim'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders()
        },
        body: JSON.stringify({})
    });
    if (!res.ok) throw await responseError(res, "Local Live Preview recovery failed");
    return await res.json();
}

export async function triggerPreviewDeployment(path, draftID) {
    const res = await fetchWithCSRF(withSite('/admin/api/preview/deployments'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders()
        },
        body: JSON.stringify({ path, draft_id: draftID })
    });
    if (!res.ok) throw new Error("Failed to update deployment preview");
    return await res.json();
}

export async function fetchPreviewDeployment(draftID, signal) {
    const id = encodeURIComponent(draftID);
    const res = await fetch(withSite(`/admin/api/preview/deployments/${id}`), {
        headers: siteHeaders(),
        signal
    });
    if (res.status === 404) return null;
    if (!res.ok) throw new Error("Failed to fetch deployment preview");
    return await res.json();
}

async function postPreviewDeploymentAction(draftID, action) {
    const id = encodeURIComponent(draftID);
    const res = await fetchWithCSRF(withSite(`/admin/api/preview/deployments/${id}/${action}`), {
        method: 'POST',
        headers: {
            ...siteHeaders()
        }
    });
    if (!res.ok) throw new Error(`Failed to ${action} deployment preview`);
    return await res.json();
}

export function retryPreviewDeployment(draftID) {
    return postPreviewDeploymentAction(draftID, 'retry');
}

export function discardPreviewDeployment(draftID) {
    return postPreviewDeploymentAction(draftID, 'discard');
}

export async function fetchMedia(mode, path) {
    let url = `/admin/api/media?mode=${mode}`;
    if (path) url += `&path=${encodeURIComponent(path)}`;
    const res = await fetch(withSite(url), { headers: siteHeaders() });
    if (!res.ok) throw new Error("Failed to fetch media");
    return await res.json();
}

export async function uploadMedia(file, mode, path) {
    await ensureCSRFToken();
    const formData = new FormData();
    formData.append('file', file);
    if (mode) formData.append('mode', mode);
    if (path) formData.append('path', path);
    const res = await fetch(withSite('/admin/api/media'), {
        method: 'POST',
        headers: {
            ...siteHeaders(),
            ...getCSRFHeaders()
        },
        body: formData
    });
    if (!res.ok) throw new Error("Upload failed");
    return await res.json();
}

export async function deleteMedia(repoPath) {
    await ensureCSRFToken();
    const res = await fetch(withSite('/admin/api/media/delete'), {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...siteHeaders(),
            ...getCSRFHeaders()
        },
        body: JSON.stringify({ repo_path: repoPath })
    });
    if (!res.ok) throw new Error("Delete failed");
    return await res.json();
}

export async function fetchSnippets() {
    const res = await fetch(withSite('/admin/api/snippets'), { headers: siteHeaders() });
    if (!res.ok) throw new Error("Failed to fetch snippets");
    return await res.json();
}
