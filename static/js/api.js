export async function fetchConfig() {
    const res = await fetch('/api/config');
    if (!res.ok) throw new Error("Config fetch failed");
    return await res.json();
}

export async function fetchArticles() {
    const res = await fetch('/api/articles');
    if (res.status === 401) {
        window.location.href = "/login";
        return null;
    }
    return await res.json();
}

export async function fetchArticle(path) {
    const res = await fetch(`/api/article?path=${path}`);
    if (!res.ok) throw new Error("Failed to load article");
    return await res.json();
}

export async function saveArticle(payload) {
    const res = await fetch('/api/article', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(payload)
    });
    if (!res.ok) throw new Error("Save failed");
    return await res.json();
}

export async function createArticle(arg1, arg2) {
    let body;
    if (typeof arg1 === 'object') {
        body = arg1;
    } else {
        body = { path: arg1, content: arg2 };
    }

    const res = await fetch('/api/create', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(body)
    });
    if (!res.ok) {
        const data = await res.json();
        throw new Error(data.error || "Create failed");
    }
    return await res.json();
}

export async function deleteArticle(path) {
    const res = await fetch('/api/delete', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({ path })
    });
    if (!res.ok) {
        const data = await res.json();
        throw new Error(data.error || "Delete failed");
    }
    return await res.json();
}

export async function getDiff(payload) {
    const res = await fetch('/api/diff', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(payload)
    });
    return await res.json();
}

export async function runBuild() {
    const res = await fetch('/api/build', { method: 'POST' });
    return await res.json();
}

export async function runSync() {
    const res = await fetch('/api/sync', { method: 'POST' });
    return await res.json();
}

export async function runPublish(path = null) {
    const options = { method: 'POST' };
    if (path) {
        options.headers = { 'Content-Type': 'application/json' };
        options.body = JSON.stringify({ path });
    }
    const res = await fetch('/api/publish', options);
    return await res.json();
}

export async function fetchMedia(mode, path) {
    let url = `/api/media?mode=${mode}`;
    if (path) url += `&path=${encodeURIComponent(path)}`;
    const res = await fetch(url);
    if (!res.ok) throw new Error("Failed to fetch media");
    return await res.json();
}

export async function uploadMedia(file, mode, path) {
    const formData = new FormData();
    formData.append('file', file);
    if (mode) formData.append('mode', mode);
    if (path) formData.append('path', path);
    const res = await fetch('/api/media', {
        method: 'POST',
        body: formData
    });
    if (!res.ok) throw new Error("Upload failed");
    return await res.json();
}

export async function deleteMedia(repoPath) {
    const res = await fetch('/api/media/delete', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({ repo_path: repoPath })
    });
    if (!res.ok) throw new Error("Delete failed");
    return await res.json();
}