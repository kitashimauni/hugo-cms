let currentPath = "";
let currentData = null;
let cmsConfig = null;
let activeLoadController = null;
let currentLoadToken = 0;

const tabIndexFallback = { files: 0, edit: 1, preview: 2 };

function getTabButton(tabName) {
    const btn = document.querySelector(`nav button[data-tab="${tabName}"]`);
    if (btn) return btn;
    const index = tabIndexFallback[tabName] ?? 0;
    return document.querySelectorAll('nav button')[index] || null;
}

function waitForNextPaint() {
    return new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
}

function showEditorLoadingState(path) {
    currentPath = path;
    document.getElementById('filename-display').textContent = path;
    const fmContainer = document.getElementById('fm-container');
    const editor = document.getElementById('editor');

    switchTab('edit', getTabButton('edit'));
    fmContainer.style.display = 'none';
    fmContainer.innerHTML = '';
    editor.value = "Loading...";
    editor.disabled = true;
}

// 初期化
init();

async function init() {
    await fetchConfig();
    await fetchFiles();
}

async function fetchConfig() {
    try {
        const res = await fetch('/api/config');
        if (res.ok) {
            cmsConfig = await res.json();
        } else {
            console.warn("Config not found or invalid");
        }
    } catch (e) {
        console.error("Failed to fetch config", e);
    }
}

function switchTab(tabName, btn) {
    document.querySelectorAll('.tab-content').forEach(el => el.classList.remove('active'));
    document.getElementById('tab-' + tabName).classList.add('active');
    if (btn) {
        document.querySelectorAll('nav button').forEach(el => el.classList.remove('active'));
        btn.classList.add('active');
    }
}

async function fetchFiles() {
    const res = await fetch('/api/articles');
    if (res.status === 401) {
        window.location.href = "/login";
        return;
    }
    const files = await res.json();
    const list = document.getElementById('file-list');
    list.innerHTML = "";

    const grouped = {};
    const others = [];

    if (cmsConfig && cmsConfig.collections) {
        cmsConfig.collections.forEach(col => {
            grouped[col.name] = { label: col.label, files: [] };
        });
    }

    files.forEach(f => {
        let matched = false;
        if (cmsConfig && cmsConfig.collections) {
            for (const col of cmsConfig.collections) {
                const colFolder = col.folder.replace(/^content\//, '');
                if (f.path.startsWith(colFolder + "/") || f.path === colFolder) {
                    grouped[col.name].files.push(f);
                    matched = true;
                    break;
                }
            }
        }
        if (!matched) {
            others.push(f);
        }
    });

    if (cmsConfig && cmsConfig.collections) {
        cmsConfig.collections.forEach(col => {
            const group = grouped[col.name];
            if (group.files.length > 0) {
                renderCollectionGroup(list, group.label, group.files);
            }
        });
    }

    if (others.length > 0) {
        renderCollectionGroup(list, "Others", others);
    }
}

function renderCollectionGroup(container, label, files) {
    const details = document.createElement('details');
    details.open = true;
    details.style.marginBottom = '10px';

    const summary = document.createElement('summary');
    summary.textContent = label;
    summary.style.cursor = 'pointer';
    summary.style.padding = '10px';
    summary.style.background = '#333';
    summary.style.color = '#fff';
    summary.style.fontWeight = 'bold';
    summary.style.borderBottom = '1px solid #444';

    details.appendChild(summary);

    files.forEach(f => {
        const div = document.createElement('div');
        div.className = 'file-item';
        div.style.paddingLeft = '20px';

        const titleDiv = document.createElement('div');
        titleDiv.style.fontWeight = 'bold';

        let titleText = f.title || f.path;
        if (f.is_dirty) {
            titleText = "✎ " + titleText;
            titleDiv.style.color = "#e2c08d"; // Modified color
        }
        titleDiv.textContent = titleText;

        const pathDiv = document.createElement('div');
        pathDiv.style.fontSize = '12px';
        pathDiv.style.color = '#888';
        pathDiv.textContent = f.path;

        div.appendChild(titleDiv);
        div.appendChild(pathDiv);

        div.onclick = () => loadFile(f.path);
        details.appendChild(div);
    });

    container.appendChild(details);
}

async function loadFile(path) {
    const loadToken = ++currentLoadToken;

    if (activeLoadController) {
        activeLoadController.abort();
    }
    const controller = new AbortController();
    activeLoadController = controller;

    showEditorLoadingState(path);
    const fmContainer = document.getElementById('fm-container');
    const editor = document.getElementById('editor');

    await waitForNextPaint();

    const fetchPromise = fetch(`/api/article?path=${encodeURIComponent(path)}`, { signal: controller.signal });

    try {
        const res = await fetchPromise;
        if (!res.ok) {
            throw new Error(`Failed to load article (${res.status})`);
        }
        const data = await res.json();

        if (loadToken !== currentLoadToken) {
            return; // Ignore stale response after a newer request
        }

        currentData = data;
        editor.disabled = false;
        switchTab('edit', getTabButton('edit'));

        if (data.frontmatter) {
            renderFrontMatterForm(data.frontmatter, path);
            fmContainer.style.display = 'block';
            editor.value = data.body ?? '';
        } else {
            fmContainer.style.display = 'none';
            fmContainer.innerHTML = '';
            editor.value = data.content ?? '';
        }
    } catch (e) {
        if (e.name === 'AbortError') {
            return;
        }
        if (loadToken !== currentLoadToken) {
            return;
        }
        editor.value = "Error loading file: " + (e.message || e);
        editor.disabled = false;
    } finally {
        if (loadToken === currentLoadToken && activeLoadController === controller) {
            activeLoadController = null;
        }
    }
}

function getCollectionForPath(path) {
    if (!cmsConfig || !cmsConfig.collections) return null;
    for (const col of cmsConfig.collections) {
        const colFolder = col.folder.replace(/^content\//, '');
        if (path.startsWith(colFolder + "/")) {
            return col;
        }
    }
    return null;
}

function renderFrontMatterForm(fm, path) {
    const container = document.getElementById('fm-container');
    container.innerHTML = '<div style="color:#aaa; font-weight:bold; margin-bottom:10px;">Front Matter</div>';

    const fragment = document.createDocumentFragment();
    const collection = getCollectionForPath(path);
    const definedFields = collection ? collection.fields : [];
    const processedKeys = new Set();

    definedFields.forEach(field => {
        if (field.name === 'body') return;

        const val = fm[field.name];
        renderField(fragment, field, val);
        processedKeys.add(field.name);
    });

    for (const [key, value] of Object.entries(fm)) {
        if (!processedKeys.has(key)) {
            let widget = 'string';
            if (typeof value === 'boolean') widget = 'boolean';
            else if (Array.isArray(value)) widget = 'list';

            renderField(fragment, { name: key, label: key + " (Extra)", widget: widget }, value);
        }
    }
    container.appendChild(fragment);
}

function renderField(container, field, value) {
    const div = document.createElement('div');
    div.className = 'fm-field';

    const label = document.createElement('label');
    label.className = 'fm-label';
    label.textContent = field.label || field.name;
    div.appendChild(label);

    if (field.widget === 'datetime') {
        const wrapper = document.createElement('div');
        wrapper.style.display = 'flex';
        wrapper.style.gap = '5px';

        const input = createInputForWidget(field, value);
        input.style.flex = '1';

        const nowBtn = document.createElement('button');
        nowBtn.textContent = 'Now';
        nowBtn.className = 'action-btn';
        nowBtn.style.background = '#444';
        nowBtn.style.padding = '4px 8px';
        nowBtn.style.fontSize = '12px';
        nowBtn.onclick = () => {
            const d = new Date();
            const pad = (n) => n < 10 ? '0' + n : n;
            const localIso = d.getFullYear() + '-' +
                pad(d.getMonth() + 1) + '-' +
                pad(d.getDate()) + 'T' +
                pad(d.getHours()) + ':' +
                pad(d.getMinutes());
            input.value = localIso;
        };

        wrapper.appendChild(input);
        wrapper.appendChild(nowBtn);
        div.appendChild(wrapper);
    } else {
        const input = createInputForWidget(field, value);
        div.appendChild(input);
    }

    container.appendChild(div);
}

function createInputForWidget(field, value) {
    let input;

    if (field.widget === 'boolean') {
        input = document.createElement('input');
        input.type = 'checkbox';
        input.className = 'fm-checkbox';
        input.checked = value === true;
        input.dataset.key = field.name;
        input.dataset.widget = 'boolean';

    } else if (field.widget === 'datetime') {
        input = document.createElement('input');
        input.type = 'datetime-local';
        input.className = 'fm-input';
        if (value) {
            try {
                const d = new Date(value);
                const pad = (n) => n < 10 ? '0' + n : n;
                const localIso = d.getFullYear() + '-' +
                    pad(d.getMonth() + 1) + '-' +
                    pad(d.getDate()) + 'T' +
                    pad(d.getHours()) + ':' +
                    pad(d.getMinutes());
                input.value = localIso;
            } catch (e) {
                input.value = value;
            }
        }
        input.dataset.key = field.name;
        input.dataset.widget = 'datetime';

    } else if (field.widget === 'list') {
        input = document.createElement('input');
        input.type = 'text';
        input.className = 'fm-input';
        input.placeholder = "Comma separated values";
        if (Array.isArray(value)) {
            input.value = value.join(', ');
        } else if (value) {
            input.value = String(value);
        }
        input.dataset.key = field.name;
        input.dataset.widget = 'list';

    } else {
        input = document.createElement('input');
        input.type = 'text';
        input.className = 'fm-input';
        input.value = (value === null || value === undefined) ? (field.default || '') : value;
        input.dataset.key = field.name;
        input.dataset.widget = 'string';
    }
    return input;
}

function collectFrontMatter() {
    if (!currentData || !currentData.frontmatter) return null;

    const fm = {};
    const inputs = document.querySelectorAll('#fm-container input');

    inputs.forEach(input => {
        const key = input.dataset.key;
        const widget = input.dataset.widget;

        if (widget === 'boolean') {
            fm[key] = input.checked;
        } else if (widget === 'list') {
            const val = input.value.trim();
            if (val === "") {
                fm[key] = [];
            } else {
                fm[key] = val.split(',').map(s => s.trim()).filter(s => s !== "");
            }
        } else if (widget === 'datetime') {
            if (input.value) {
                const d = new Date(input.value);
                const pad = (n) => (n < 10 ? '0' : '') + n;
                const tzo = -d.getTimezoneOffset();
                const dif = tzo >= 0 ? '+' : '-';
                const offH = pad(Math.floor(Math.abs(tzo) / 60));
                const offM = pad(Math.abs(tzo) % 60);

                fm[key] = d.getFullYear() + '-' +
                    pad(d.getMonth() + 1) + '-' +
                    pad(d.getDate()) + 'T' +
                    pad(d.getHours()) + ':' +
                    pad(d.getMinutes()) + ':' +
                    pad(d.getSeconds()) +
                    dif + offH + ':' + offM;
            } else {
                fm[key] = null;
            }
        } else {
            fm[key] = input.value;
        }
    });
    return fm;
}

function getPayload() {
    const payload = { path: currentPath };
    const fm = collectFrontMatter();
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
    if (!currentPath) return alert("No file selected");

    const btn = document.querySelector('button[onclick="saveFile()"]');
    const originalText = btn.textContent;
    btn.textContent = "Saving...";

    await fetch('/api/article', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(getPayload())
    });

    btn.textContent = originalText;
}

async function resetChanges() {
    if (!currentPath) return;
    if (!confirm("Are you sure you want to discard all changes?")) return;
    await loadFile(currentPath);
}

async function showDiff() {
    if (!currentPath) return;
    const payload = getPayload();

    const res = await fetch('/api/diff', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
    });
    const data = await res.json();

    const body = document.getElementById('modal-body');
    // Basic syntax highlight for diff
    let html = data.diff.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
    html = html.split('\n').map(line => {
        if (line.startsWith('+')) return `<span class="diff-added">${line}</span>`;
        if (line.startsWith('-')) return `<span class="diff-removed">${line}</span>`;
        return line;
    }).join('\n');

    body.innerHTML = html || "No differences";
    document.getElementById('modal-overlay').style.display = 'flex';
}

function closeModal() {
    document.getElementById('modal-overlay').style.display = 'none';
}

async function runBuild() {
    const btn = document.querySelector('button[onclick="runBuild()"]');
    btn.textContent = "Building...";
    btn.disabled = true;

    try {
        const res = await fetch('/api/build', { method: 'POST' });
        const data = await res.json();

        if (data.status === 'ok') {
            const frame = document.getElementById('preview-frame');
            let previewPath = currentPath.replace(/\.md$/, "");

            if (previewPath.endsWith("/index") || previewPath.endsWith("/_index")) {
                previewPath = previewPath.substring(0, previewPath.lastIndexOf("/"));
            } else if (previewPath === "index" || previewPath === "_index") {
                previewPath = "";
            }

            const targetUrl = "/preview/" + previewPath + (previewPath ? "/" : "");
            frame.src = targetUrl + "?t=" + Date.now();
            switchTab('preview', document.querySelectorAll('nav button')[2]);
        } else {
            alert("Build Error:\n" + data.log);
        }
    } catch (e) {
        alert("Network Error");
    } finally {
        btn.textContent = "Build";
        btn.disabled = false;
    }
}

async function runSync() {
    if (!confirm("GitHubから最新の状態を取得しますか？\n（ローカルの未保存の変更は注意してください）")) return;

    const btn = document.querySelector('button[onclick="runSync()"]');
    const originalText = btn.textContent;
    btn.textContent = "Syncing...";

    try {
        const res = await fetch('/api/sync', { method: 'POST' });
        const data = await res.json();
        if (data.status === 'ok') {
            alert("Sync Complete!\n" + data.log);
            fetchFiles();
        } else {
            alert("Sync Error:\n" + data.log);
        }
    } catch (e) {
        alert("Network Error");
    } finally {
        btn.textContent = originalText;
    }
}

async function runPublish() {
    if (!confirm("この記事の変更をGitHubにPushして公開しますか？")) return;

    const btn = document.querySelector('button[onclick="runPublish()"]');
    btn.textContent = "Pushing...";
    btn.disabled = true;

    try {
        const res = await fetch('/api/publish', { method: 'POST' });
        const data = await res.json();
        if (data.status === 'ok') {
            alert("Published Successfully! 🚀\nCloudflare Pages will deploy shortly.");
        } else {
            alert("Publish Error:\n" + data.log);
        }
    } catch (e) {
        alert("Network Error");
    } finally {
        btn.textContent = "Publish";
        btn.disabled = false;
    }
}

async function createNewFile() {
    let path = prompt("Enter file path (e.g., posts/my-new-post.md):", "posts/");
    if (!path) return;

    if (!path.endsWith(".md") && !path.endsWith(".markdown")) {
        if (!confirm("Filename does not end with .md. Continue?")) return;
    }

    const content = "---\ntitle: New Post\ndraft: true\n---\n";

    try {
        const res = await fetch('/api/create', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ path: path, content: content })
        });

        if (res.ok) {
            await fetchFiles();
            await loadFile(path);
        } else {
            const data = await res.json();
            alert("Create Failed: " + data.error);
        }
    } catch (e) {
        alert("Network Error");
    }
}
