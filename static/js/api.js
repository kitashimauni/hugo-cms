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

// Reset CSRF token on 403 errors (token expired/invalid)
function resetCSRFToken() {
    csrfToken = null;
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
    // Handle CSRF token expiration - retry once with fresh token
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

export async function runBuild() {
    await ensureCSRFToken();
    const res = await fetch(withSite('/admin/api/build'), {
        method: 'POST',
        headers: {
            ...siteHeaders(),
            ...getCSRFHeaders()
        }
    });
    return await res.json();
}

export async function restartHugo() {
    await ensureCSRFToken();
    const res = await fetch(withSite('/admin/api/build/restart'), {
        method: 'POST',
        headers: {
            ...siteHeaders(),
            ...getCSRFHeaders()
        }
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

export async function runPublish(path = null) {
    await ensureCSRFToken();
    const options = {
        method: 'POST',
        headers: {
            ...siteHeaders(),
            ...getCSRFHeaders()
        }
    };
    if (path) {
        options.headers = {
            'Content-Type': 'application/json',
            ...siteHeaders(),
            ...getCSRFHeaders()
        };
        options.body = JSON.stringify({ path });
    }
    const res = await fetch(withSite('/admin/api/publish'), options);
    return await res.json();
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
