// ═══════════ FOLDERS ═══════════
async function loadFolders() {
    try {
        state.folders = await API.get('/api/folders');
        renderFolderGrid();
    } catch (e) { console.error('Folders error:', e); }
}

function renderFolderGrid() {
    const grid = document.getElementById('folderGrid');
    grid.innerHTML = '';

    if (!state.folders || state.folders.length === 0) {
        document.getElementById('folderSelector').classList.remove('hidden');
        document.getElementById('fileBrowser').classList.add('hidden');
        return;
    }

    // If a folder is already selected, stay in file browser
    if (state.currentFolder) {
        document.getElementById('folderSelector').classList.add('hidden');
        document.getElementById('fileBrowser').classList.remove('hidden');
        return;
    }

    document.getElementById('folderSelector').classList.remove('hidden');
    document.getElementById('fileBrowser').classList.add('hidden');

    state.folders.forEach(f => {
        const card = document.createElement('div');
        card.className = 'folder-card';
        card.innerHTML = `
            <div class="folder-icon">
                <svg width="28" height="28" viewBox="0 0 24 24" fill="#5f6368"><path d="M20 6h-8l-2-2H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2zm0 12H4V8h16v10z"/></svg>
            </div>
            <div class="folder-info">
                <div class="folder-name">${escHTML(f.label || f.id)}</div>
                <div class="folder-path">${escHTML(f.path)}</div>
            </div>
            <button class="folder-download" title="Baixar pasta como ZIP" onclick="event.stopPropagation(); downloadFolderRoot('${escAttr(f.id)}', '${escAttr(f.label || f.id)}')">
                <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor"><path d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z"/></svg>
            </button>
            <button class="folder-remove" title="Remover pasta" onclick="event.stopPropagation(); removeFolder('${escAttr(f.id)}')">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>
            </button>
        `;
        card.addEventListener('click', () => openFolder(f));
        grid.appendChild(card);
    });
}

function openFolder(folder) {
    state.currentFolder = folder.id || folder.label;
    state.currentPath = '';
    document.getElementById('folderSelector').classList.add('hidden');
    document.getElementById('fileBrowser').classList.remove('hidden');
    updateBreadcrumb(folder.label || folder.id, '');
    loadFiles(state.currentFolder, '');
}

async function loadFiles(folderID, subPath) {
    try {
        let url = `/api/folders/files?id=${encodeURIComponent(folderID)}`;
        if (subPath) url += `&path=${encodeURIComponent(subPath)}`;
        const data = await API.get(url);
        state.files = data.items || [];
        renderFileList();
    } catch (e) { console.error('Files error:', e); }
}

function renderFileList() {
    const list = document.getElementById('fileList');
    list.innerHTML = '';

    if (state.files.length === 0) {
        list.innerHTML = '<div class="empty-state"><p>Pasta vazia</p></div>';
        return;
    }

    // Sort: folders first, then files
    const sorted = [...state.files].sort((a, b) => {
        if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
        return a.name.localeCompare(b.name);
    });

    sorted.forEach(file => {
        const row = document.createElement('div');
        row.className = 'file-row';
        row.innerHTML = `
            <div class="file-name">
                <div class="file-icon ${file.mime_type}">${getFileIcon(file.mime_type)}</div>
                <span>${escHTML(file.name)}</span>
            </div>
            <div class="file-status">${renderSyncStatus(file)}</div>
            <div class="file-modified">${file.mod_time || '—'}</div>
            <div class="file-size">${file.is_dir ? '—' : formatSize(file.size)}</div>
        `;
        if (file.is_dir) {
            row.addEventListener('click', () => navigateToSubfolder(file));
        }
        list.appendChild(row);
    });
}

// ── Sync Status Rendering (like Google Drive desktop icons) ──
function renderSyncStatus(file) {
    const status = (file.status || 'synced').toLowerCase();

    // Status icons matching Drive desktop behavior
    const statusMap = {
        'sincronizado': { icon: '✅', label: 'Sincronizado', cls: 'text-success' },
        'synced': { icon: '✅', label: 'Sincronizado', cls: 'text-success' },
        'sincronizando': { icon: '🔄', label: 'Sincronizando', cls: 'text-warning' },
        'syncing': { icon: '🔄', label: 'Sincronizando', cls: 'text-warning' },
        'pendente': { icon: '☁️', label: 'Pendente', cls: 'text-muted' },
        'pending': { icon: '☁️', label: 'Pendente', cls: 'text-muted' },
        'erro': { icon: '❌', label: 'Erro', cls: 'text-error' },
        'error': { icon: '❌', label: 'Erro', cls: 'text-error' },
        'somente local': { icon: '💻', label: 'Somente local', cls: 'text-muted' },
        'local': { icon: '💻', label: 'Somente local', cls: 'text-muted' },
    };

    const s = statusMap[status] || statusMap['synced'];

    // Show progress bar for syncing files
    if (status === 'sincronizando' || status === 'syncing') {
        const pct = Math.round((file.progress || 0) * 100);
        return `<span class="${s.cls}" title="${s.label}">${s.icon} ${pct}%</span>
                <div style="width:60px;height:3px;background:#e0e0e0;border-radius:2px;margin-top:3px;">
                    <div style="width:${pct}%;height:100%;background:#f9ab00;border-radius:2px;transition:width .3s;"></div>
                </div>`;
    }

    return `<span class="${s.cls}" title="${s.label}">${s.icon} ${s.label}</span>`;
}

function navigateToSubfolder(file) {
    state.currentPath = file.path;
    updateBreadcrumb(state.currentFolder, state.currentPath);
    loadFiles(state.currentFolder, state.currentPath);
}

function updateBreadcrumb(folderName, path) {
    const bc = document.getElementById('breadcrumb');
    bc.innerHTML = '';

    // Root breadcrumb
    const root = document.createElement('span');
    root.className = 'breadcrumb-item';
    root.textContent = 'Meus Arquivos';
    root.addEventListener('click', () => {
        state.currentFolder = null;
        state.currentPath = '';
        document.getElementById('folderSelector').classList.remove('hidden');
        document.getElementById('fileBrowser').classList.add('hidden');
        loadFolders();
        updateBreadcrumb('', '');
    });
    bc.appendChild(root);

    if (!folderName) {
        root.classList.add('active');
        return;
    }

    // Folder breadcrumb
    bc.appendChild(createSep());
    const folderCrumb = document.createElement('span');
    folderCrumb.className = 'breadcrumb-item' + (path ? '' : ' active');
    folderCrumb.textContent = folderName;
    if (path) {
        folderCrumb.addEventListener('click', () => {
            state.currentPath = '';
            updateBreadcrumb(folderName, '');
            loadFiles(state.currentFolder, '');
        });
    }
    bc.appendChild(folderCrumb);

    // Sub-path breadcrumbs
    if (path) {
        const parts = path.split('/');
        parts.forEach((part, i) => {
            bc.appendChild(createSep());
            const crumb = document.createElement('span');
            crumb.className = 'breadcrumb-item' + (i === parts.length - 1 ? ' active' : '');
            crumb.textContent = part;
            if (i < parts.length - 1) {
                const subPath = parts.slice(0, i + 1).join('/');
                crumb.addEventListener('click', () => {
                    state.currentPath = subPath;
                    updateBreadcrumb(folderName, subPath);
                    loadFiles(state.currentFolder, subPath);
                });
            }
            bc.appendChild(crumb);
        });
    }
}

function createSep() {
    const sep = document.createElement('span');
    sep.className = 'breadcrumb-sep';
    sep.textContent = '›';
    return sep;
}

async function removeFolder(id) {
    if (!confirm('Remover esta pasta da sincronização?')) return;
    try {
        await API.del(`/api/folders?id=${encodeURIComponent(id)}`);
        toast('Pasta removida');
        if (state.currentFolder === id) {
            state.currentFolder = null;
            state.currentPath = '';
        }
        loadFolders();
    } catch (e) { toast('Erro ao remover pasta', 'error'); }
}

function downloadFolderRoot(folderID, folderName) {
    const url = `/api/files/download-zip?folder=${encodeURIComponent(folderID)}&path=`;
    const a = document.createElement('a');
    a.href = url;
    a.download = folderName + '.zip';
    a.target = '_blank';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
}

