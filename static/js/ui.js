// ui.js - 画面描画ロジック

export function switchTab(tabName) {
    document.querySelectorAll('.tab-content').forEach(el => el.classList.remove('active'));
    document.getElementById('tab-' + tabName).classList.add('active');
    
    document.querySelectorAll('nav button').forEach(el => el.classList.remove('active'));
    // ボタンのハイライト切り替えは、nav内のボタンの順序に依存しないように修正
    const buttons = {
        'files': 0,
        'edit': 1,
        'preview': 2
    };
    const btnIndex = buttons[tabName];
    if (btnIndex !== undefined) {
        document.querySelectorAll('nav button')[btnIndex].classList.add('active');
    }
}

export async function showLoadingEditor() {
    const fmContainer = document.getElementById('fm-container');
    const editor = document.getElementById('editor');
    
    switchTab('edit');
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
        editor.value = data.body;
    } else {
        fmContainer.style.display = 'none';
        fmContainer.innerHTML = '';
        editor.value = data.content;
    }
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
    container.innerHTML = '<div style="color:#aaa; font-weight:bold; margin-bottom:10px;">Front Matter</div>';
    
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
            const pad = (n) => n < 10 ? '0'+n : n;
            const localIso = d.getFullYear() + '-' + 
                           pad(d.getMonth()+1) + '-' + 
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
                const pad = (n) => n < 10 ? '0'+n : n;
                const localIso = d.getFullYear() + '-' + 
                               pad(d.getMonth()+1) + '-' + 
                               pad(d.getDate()) + 'T' + 
                               pad(d.getHours()) + ':' + 
                               pad(d.getMinutes());
                input.value = localIso;
            } catch(e) {
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

    const targetUrl = "/preview/" + previewPath + (previewPath ? "/" : "");
    frame.src = targetUrl + "?t=" + Date.now();
}

export function showDiffModal(diffHtml) {
    const body = document.getElementById('modal-body');
    body.innerHTML = diffHtml || "No differences";
    document.getElementById('modal-overlay').style.display = 'flex';
}

export function closeModal() {
    document.getElementById('modal-overlay').style.display = 'none';
}
