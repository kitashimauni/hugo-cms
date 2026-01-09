// CSRF Token Management
let csrfToken = null;

async function ensureCSRFToken() {
    if (csrfToken) return csrfToken;
    const res = await fetch('/admin/api/csrf-token');
    if (!res.ok) throw new Error("Failed to fetch CSRF token");
    const data = await res.json();
    csrfToken = data.csrf_token;
    return csrfToken;
}

function getCSRFHeaders() {
    return csrfToken ? { 'X-CSRF-Token': csrfToken } : {};
}

export async function fetchConfig() {
    const res = await fetch('/admin/api/config');
    if (!res.ok) throw new Error("Config fetch failed");
    return await res.json();
}

export async function fetchArticles() {
    const res = await fetch('/admin/api/articles');
    if (res.status === 401) {
        window.location.href = "/admin/login";
        return null;
    }
    return await res.json();
}

export async function fetchArticle(path) {
    const res = await fetch(`/admin/api/article?path=${path}`);
    if (!res.ok) throw new Error("Failed to load article");
    return await res.json();
}

export async function saveArticle(payload) {
    await ensureCSRFToken();
    const res = await fetch('/admin/api/article', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...getCSRFHeaders()
        },
        body: JSON.stringify(payload)
    });
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

    const res = await fetch('/admin/api/create', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
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
    const res = await fetch('/admin/api/delete', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
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
    const res = await fetch('/admin/api/diff', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...getCSRFHeaders()
        },
        body: JSON.stringify(payload)
    });
    return await res.json();
}

export async function runBuild() {
    await ensureCSRFToken();
    const res = await fetch('/admin/api/build', { 
        method: 'POST',
        headers: getCSRFHeaders()
    });
    return await res.json();
}

export async function restartHugo() {
    await ensureCSRFToken();
    const res = await fetch('/admin/api/build/restart', { 
        method: 'POST',
        headers: getCSRFHeaders()
    });
    return await res.json();
}

export async function runSync() {
    await ensureCSRFToken();
    const res = await fetch('/admin/api/sync', { 
        method: 'POST',
        headers: getCSRFHeaders()
    });
    return await res.json();
}

export async function runPublish(path = null) {
    await ensureCSRFToken();
    const options = { 
        method: 'POST',
        headers: getCSRFHeaders()
    };
    if (path) {
        options.headers = { 
            'Content-Type': 'application/json',
            ...getCSRFHeaders()
        };
        options.body = JSON.stringify({ path });
    }
    const res = await fetch('/admin/api/publish', options);
    return await res.json();
}

export async function fetchMedia(mode, path) {
    let url = `/admin/api/media?mode=${mode}`;
    if (path) url += `&path=${encodeURIComponent(path)}`;
    const res = await fetch(url);
    if (!res.ok) throw new Error("Failed to fetch media");
    return await res.json();
}

export async function uploadMedia(file, mode, path) {
    await ensureCSRFToken();
    const formData = new FormData();
    formData.append('file', file);
    if (mode) formData.append('mode', mode);
    if (path) formData.append('path', path);
    formData.append('csrf_token', csrfToken); // CSRF token in form data for multipart
    const res = await fetch('/admin/api/media', {
        method: 'POST',
        headers: getCSRFHeaders(),
        body: formData
    });
    if (!res.ok) throw new Error("Upload failed");
    return await res.json();
}

export async function deleteMedia(repoPath) {
    await ensureCSRFToken();
    const res = await fetch('/admin/api/media/delete', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
            ...getCSRFHeaders()
        },
        body: JSON.stringify({ repo_path: repoPath })
    });
    if (!res.ok) throw new Error("Delete failed");
    return await res.json();
}
