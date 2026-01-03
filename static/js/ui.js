// ui.js - 画面描画ロジック
import * as API from './api.js';

export function switchView(viewName) {
    const contentArea = document.getElementById('content-area');
    contentArea.classList.remove('split-mode');
    document.getElementById('btn-view-split').classList.remove('active');

    document.getElementById('edit-view').style.display = 'none';
    document.getElementById('preview-view').style.display = 'none';

    if (viewName === 'edit') {
        document.getElementById('edit-view').style.display = 'flex';
    } else if (viewName === 'preview') {
        document.getElementById('preview-view').style.display = 'block';
    }

    const toggles = document.querySelectorAll('.view-toggle');
    toggles.forEach(btn => {
        if (btn.id === 'btn-view-' + viewName) {
            btn.classList.add('active');
        } else {
            btn.classList.remove('active');
        }
    });
}

export function toggleSplitView() {
    const contentArea = document.getElementById('content-area');
    const isSplit = contentArea.classList.toggle('split-mode');
    const splitBtn = document.getElementById('btn-view-split');

    if (isSplit) {
        splitBtn.classList.add('active');
        document.getElementById('btn-view-edit').classList.remove('active');
        document.getElementById('btn-view-preview').classList.remove('active');

        // Trigger build since preview is shown
        if (window.buildAndPreview) window.buildAndPreview();
    } else {
        splitBtn.classList.remove('active');
        switchView('edit');
    }
}

export function toggleSidebar() {
    document.querySelector('aside').classList.toggle('sidebar-open');
    const backdrop = document.getElementById('sidebar-backdrop');
    if (backdrop) {
        if (document.querySelector('aside').classList.contains('sidebar-open')) {
            backdrop.style.display = 'block';
        } else {
            backdrop.style.display = 'none';
        }
    }
}

export async function showLoadingEditor() {
    const fmContainer = document.getElementById('fm-container');
    const editor = document.getElementById('editor');

    // switchView('edit'); // Removed to preserve current view
    fmContainer.style.display = 'none';
    editor.value = "Loading...";
    editor.disabled = true;

    // 描画待ち
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
}

export function updateEditorContent(data, path, config) {
    const fmContainer = document.getElementById('fm-container');
    const editor = document.getElementById('editor');
    editor.disabled = false;

    if (data.frontmatter) {
        renderFrontMatterForm(data.frontmatter, path, config, fmContainer);
        fmContainer.style.display = 'block';
        editor.value = data.body || "";
    } else {
        fmContainer.style.display = 'none';
        fmContainer.innerHTML = '';
        editor.value = data.content || "";
    }
    editor.placeholder = "Write content here...";
}

export function showEditorError(error) {
    const editor = document.getElementById('editor');
    editor.value = "Error loading file: " + error;
    editor.disabled = false;
}

export function renderFileList(files, config) {
    const list = document.getElementById('file-list');
    list.innerHTML = "";

    const grouped = {};
    const others = [];

    if (config && config.collections) {
        config.collections.forEach(col => {
            grouped[col.name] = { label: col.label, files: [] };
        });
    }

    files.forEach(f => {
        let matched = false;
        if (config && config.collections) {
            for (const col of config.collections) {
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

    if (config && config.collections) {
        config.collections.forEach(col => {
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
            titleDiv.style.color = "#e2c08d";
        }
        titleDiv.textContent = titleText;

        const pathDiv = document.createElement('div');
        pathDiv.style.fontSize = '12px';
        pathDiv.style.color = '#888';
        pathDiv.textContent = f.path;

        div.appendChild(titleDiv);
        div.appendChild(pathDiv);

        // グローバル関数 loadFile を呼び出す
        div.onclick = () => window.loadFile(f.path);
        details.appendChild(div);
    });

    container.appendChild(details);
}

function getCollectionForPath(path, config) {
    if (!config || !config.collections) return null;
    for (const col of config.collections) {
        const colFolder = col.folder.replace(/^content\//, '');
        if (path.startsWith(colFolder + "/")) {
            return col;
        }
    }
    return null;
}

function renderFrontMatterForm(fm, path, config, container) {
    container.innerHTML = '';

    const details = document.createElement('details');
    details.style.marginBottom = '10px';

    const summary = document.createElement('summary');
    summary.textContent = "Article Settings";
    summary.style.fontWeight = 'bold';
    summary.style.cursor = 'pointer';
    summary.style.padding = '8px';
    summary.style.backgroundColor = '#2a2a2a';
    summary.style.color = '#ccc';
    summary.style.borderRadius = '4px';
    summary.style.outline = 'none';

    details.appendChild(summary);

    const wrapper = document.createElement('div');
    wrapper.style.padding = '10px';
    wrapper.style.border = '1px solid #333';
    wrapper.style.borderTop = 'none';

    const fragment = document.createDocumentFragment();
    const collection = getCollectionForPath(path, config);
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

    wrapper.appendChild(fragment);
    details.appendChild(wrapper);
    container.appendChild(details);
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
                           pad(d.getMonth()+1) + '-' +
                           pad(d.getDate()) + 'T' +
                           pad(d.getHours()) + ':' +
                           pad(d.getMinutes()) + ':' +
                           pad(d.getSeconds());
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
                               pad(d.getMonth()+1) + '-' +
                               pad(d.getDate()) + 'T' +
                               pad(d.getHours()) + ':' +
                               pad(d.getMinutes()) + ':' +
                               pad(d.getSeconds());
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

export function collectFrontMatter() {
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

export function setPreviewUrl(path) {
    const frame = document.getElementById('preview-frame');
    let previewPath = path.replace(/\.md$/, "");

    if (previewPath.endsWith("/index") || previewPath.endsWith("/_index")) {
        previewPath = previewPath.substring(0, previewPath.lastIndexOf("/"));
    } else if (previewPath === "index" || previewPath === "_index") {
        previewPath = "";
    }

    const rootRelativePath = "/" + previewPath.replace(/^\//, "");
    const targetUrl = rootRelativePath + (rootRelativePath.endsWith("/") ? "" : "/");

    frame.src = targetUrl + "?t=" + Date.now();
}

export function showDiffModal(diffHtml) {
    const body = document.getElementById('modal-body');
    body.innerHTML = diffHtml || "No differences";
    document.getElementById('modal-overlay').style.display = 'flex';
}

export function toggleHeaderMenu() {
    document.getElementById("header-menu-dropdown").classList.toggle("show");
}

// Close the dropdown if the user clicks outside of it
window.onclick = function (event) {
    if (!event.target.matches('.mobile-actions button') && !event.target.matches('.mobile-actions button *')) {
        const dropdowns = document.getElementsByClassName("dropdown-content");
        for (let i = 0; i < dropdowns.length; i++) {
            const openDropdown = dropdowns[i];
            if (openDropdown.classList.contains('show')) {
                openDropdown.classList.remove('show');
            }
        }
    }
}

export function showCreationModal(config, onCreate) {
    const overlay = document.getElementById('modal-overlay');
    const header = document.getElementById('modal-header');
    const body = document.getElementById('modal-body');

    header.querySelector('span').textContent = "New Article";
    body.innerHTML = '';
    overlay.style.display = 'flex';

    if (!config || !config.collections || config.collections.length === 0) {
        body.innerHTML = '<p>No collections defined in config.</p>';
        return;
    }

    // Collection Selector
    const selWrapper = document.createElement('div');
    selWrapper.style.marginBottom = '15px';
    selWrapper.innerHTML = '<strong>Collection: </strong>';

    const select = document.createElement('select');
    select.className = 'fm-input';
    select.style.width = 'auto';
    select.style.display = 'inline-block';

    config.collections.forEach(c => {
        const opt = document.createElement('option');
        opt.value = c.name;
        opt.textContent = c.label || c.name;
        select.appendChild(opt);
    });
    selWrapper.appendChild(select);
    body.appendChild(selWrapper);

    // Fields Container
    const fieldsContainer = document.createElement('div');
    fieldsContainer.id = 'creation-fields';
    body.appendChild(fieldsContainer);

    // Render fields for initial selection
    const render = () => {
        fieldsContainer.innerHTML = '';
        const colName = select.value;
        const col = config.collections.find(c => c.name === colName);
        if (col && col.fields) {
            col.fields.forEach(field => {
                if (field.name === "body") return;
                // Pre-fill defaults
                const val = field.default !== undefined ? field.default : null;
                renderField(fieldsContainer, field, val);
            });
        }
    };
    select.onchange = render;
    render();

    // Create Button
    const btnDiv = document.createElement('div');
    btnDiv.style.marginTop = '20px';
    btnDiv.style.textAlign = 'right';

    const createBtn = document.createElement('button');
    createBtn.className = 'action-btn';
    createBtn.style.background = '#2da44e';
    createBtn.textContent = 'Create';
    createBtn.onclick = () => {
        const colName = select.value;
        // Collect data
        const fields = {};
        const inputs = fieldsContainer.querySelectorAll('input');
        inputs.forEach(input => {
            const key = input.dataset.key;
            const widget = input.dataset.widget;
            if (widget === 'boolean') {
                fields[key] = input.checked;
            } else if (widget === 'list') {
                const val = input.value.trim();
                fields[key] = val === "" ? [] : val.split(',').map(s => s.trim());
            } else if (widget === 'datetime') {
                if (input.value) {
                    const d = new Date(input.value);
                    const pad = (n) => (n < 10 ? '0' : '') + n;
                    const tzo = -d.getTimezoneOffset();
                    const dif = tzo >= 0 ? '+' : '-';
                    const offH = pad(Math.floor(Math.abs(tzo) / 60));
                    const offM = pad(Math.abs(tzo) % 60);

                    fields[key] = d.getFullYear() + '-' +
                        pad(d.getMonth() + 1) + '-' +
                        pad(d.getDate()) + 'T' +
                        pad(d.getHours()) + ':' +
                        pad(d.getMinutes()) + ':' +
                        pad(d.getSeconds()) +
                        dif + offH + ':' + offM;
                } else {
                    fields[key] = null;
                }
            } else {
                fields[key] = input.value;
            }
        });
        onCreate(colName, fields);
        closeModal();
    };

    btnDiv.appendChild(createBtn);
    body.appendChild(btnDiv);
}

export function closeModal() {
    document.getElementById('modal-overlay').style.display = 'none';
}

// Toast Notifications
export function showToast(message, type = 'info') {
    let container = document.getElementById('toast-container');
    if (!container) {
        container = document.createElement('div');
        container.id = 'toast-container';
        document.body.appendChild(container);
    }

    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.innerHTML = `<span>${message}</span>`;

    // Close button
    const closeBtn = document.createElement('span');
    closeBtn.innerHTML = '&times;';
    closeBtn.style.cursor = 'pointer';
    closeBtn.style.marginLeft = '10px';
    closeBtn.onclick = () => {
        toast.style.opacity = '0';
        setTimeout(() => toast.remove(), 300);
    };
    toast.appendChild(closeBtn);

    container.appendChild(toast);

    // Auto remove
    setTimeout(() => {
        if (toast.parentElement) {
            toast.style.animation = 'fadeOut 0.3s forwards';
            setTimeout(() => toast.remove(), 300);
        }
    }, 5000);
}

export async function showMediaLibrary(onSelect, collectionName = null, currentPath = null) {
    const overlay = document.getElementById('modal-overlay');
    const header = document.getElementById('modal-header');
    const body = document.getElementById('modal-body');

    header.querySelector('span').textContent = "Media Library";
    body.innerHTML = '';
    overlay.style.display = 'flex';

    // Tabs
    const tabs = document.createElement('div');
    tabs.style.display = 'flex';
    tabs.style.gap = '10px';
    tabs.style.marginBottom = '15px';
    tabs.style.borderBottom = '1px solid #444';
    tabs.style.paddingBottom = '5px';

    const createTab = (id, label) => {
        const t = document.createElement('button');
        t.textContent = label;
        t.style.padding = '8px 16px';
        t.style.border = 'none';
        t.style.background = 'transparent';
        t.style.color = '#888';
        t.style.cursor = 'pointer';
        t.style.fontSize = '14px';
        t.style.fontWeight = 'bold';
        t.id = `tab-media-${id}`;
        return t;
    };

    const tabStatic = createTab('static', 'Static');
    const tabArticle = createTab('content', 'Article');

    const isBundle = currentPath && (currentPath.endsWith('/index.md') || currentPath.endsWith('/_index.md'));

    if (!isBundle) {
        tabArticle.disabled = true;
        tabArticle.style.opacity = '0.5';
        tabArticle.title = "Only available for page bundles (index.md)";
    }

    tabs.appendChild(tabStatic);
    tabs.appendChild(tabArticle);
    body.appendChild(tabs);

    const contentArea = document.createElement('div');
    body.appendChild(contentArea);

    // Tab Logic
    const switchTab = (mode) => {
        // Update styles
        [tabStatic, tabArticle].forEach(t => {
            t.style.borderBottom = 'none';
            t.style.color = '#888';
        });
        const activeTab = mode === 'static' ? tabStatic : tabArticle;
        activeTab.style.borderBottom = '2px solid #6f42c1';
        activeTab.style.color = '#fff';

        // Load content
        loadAndRenderMedia(contentArea, mode, currentPath, onSelect);
    };

    tabStatic.onclick = () => switchTab('static');
    tabArticle.onclick = () => { if(isBundle) switchTab('content'); };

    // Default tab
    switchTab(isBundle ? 'content' : 'static');
}

async function loadAndRenderMedia(container, mode, currentPath, onSelect) {
    container.innerHTML = 'Loading...';
    try {
        const files = await API.fetchMedia(mode, currentPath);
        renderMediaGrid(container, files || [], mode, currentPath, onSelect);
    } catch (e) {
        container.innerHTML = `<p style="color:red">Failed to load media: ${e.message}</p>`;
    }
}

function renderMediaGrid(container, files, mode, currentPath, onSelect) {
    container.innerHTML = '';

    // Toolbar (Upload)
    const toolbar = document.createElement('div');
    toolbar.style.marginBottom = '10px';
    toolbar.style.display = 'flex';
    toolbar.style.justifyContent = 'space-between';
    
    const fileInput = document.createElement('input');
    fileInput.type = 'file';
    fileInput.accept = 'image/*';
    fileInput.style.display = 'none';
    fileInput.onchange = async (e) => {
        if (e.target.files.length > 0) {
            const file = e.target.files[0];
            showToast("Uploading...", "info");
            try {
                await API.uploadMedia(file, mode, currentPath);
                showToast("Uploaded!", "success");
                loadAndRenderMedia(container, mode, currentPath, onSelect);
            } catch (err) {
                showToast("Upload failed: " + err.message, "error");
            }
        }
    };

    const uploadBtn = document.createElement('button');
    uploadBtn.className = 'action-btn';
    uploadBtn.textContent = `⬆ Upload to ${mode === 'static' ? 'Static' : 'Article'}`;
    uploadBtn.onclick = () => fileInput.click();

    toolbar.appendChild(uploadBtn);
    container.appendChild(toolbar);
    container.appendChild(fileInput);

    // Grid
    const grid = document.createElement('div');
    grid.style.display = 'grid';
    grid.style.gridTemplateColumns = 'repeat(auto-fill, minmax(100px, 1fr))';
    grid.style.gap = '10px';
    grid.style.maxHeight = '400px';
    grid.style.overflowY = 'auto';

    if (files.length === 0) {
        grid.innerHTML = '<p style="grid-column: 1/-1; text-align:center; color:#888;">No images found.</p>';
    }

    files.forEach(f => {
        const item = document.createElement('div');
        item.style.border = '1px solid #444';
        item.style.borderRadius = '4px';
        item.style.overflow = 'hidden';
        item.style.cursor = 'pointer';
        item.style.position = 'relative';
        item.style.backgroundColor = '#222';

        const img = document.createElement('img');
        img.src = f.url;
        img.style.width = '100%';
        img.style.height = '100px';
        img.style.objectFit = 'cover';
        img.title = f.name;

        const name = document.createElement('div');
        name.textContent = f.name;
        name.style.position = 'absolute';
        name.style.bottom = '0';
        name.style.width = '100%';
        name.style.background = 'rgba(0,0,0,0.7)';
        name.style.fontSize = '10px';
        name.style.padding = '2px';
        name.style.whiteSpace = 'nowrap';
        name.style.overflow = 'hidden';
        name.style.textOverflow = 'ellipsis';
        name.style.textAlign = 'center';

        const delBtn = document.createElement('button');
        delBtn.textContent = '×';
        delBtn.style.position = 'absolute';
        delBtn.style.top = '0';
        delBtn.style.right = '0';
        delBtn.style.background = 'red';
        delBtn.style.color = 'white';
        delBtn.style.border = 'none';
        delBtn.style.cursor = 'pointer';
        delBtn.style.padding = '0 5px';
        
        delBtn.onclick = async (e) => {
            e.stopPropagation();
            if (!confirm(`Delete ${f.name}?`)) return;
            try {
                await API.deleteMedia(f.repo_path);
                showToast("Deleted", "success");
                loadAndRenderMedia(container, mode, currentPath, onSelect);
            } catch (err) {
                showToast("Delete failed", "error");
            }
        };

        item.onclick = () => {
            onSelect(f);
            closeModal();
        };

        item.appendChild(img);
        item.appendChild(name);
        item.appendChild(delBtn);
        grid.appendChild(item);
    });

    container.appendChild(grid);
}