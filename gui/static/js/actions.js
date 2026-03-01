// ═══════════ ACTIONS ═══════════
function initActions() {
    // Add folder button
    document.getElementById('addFolderBtn').addEventListener('click', showAddFolderModal);

    // Sync now
    document.getElementById('syncNowBtn').addEventListener('click', async () => {
        try {
            await API.post('/api/sync/trigger');
            toast('Sincronização iniciada');
        } catch (e) { toast('Erro', 'error'); }
    });

    // View mode toggle (grid / list)
    const viewGridBtn = document.getElementById('viewGridBtn');
    const viewListBtn = document.getElementById('viewListBtn');
    function applyViewMode(mode) {
        state.viewMode = mode;
        localStorage.setItem('stashbird-viewMode', mode);
        viewGridBtn.classList.toggle('active', mode === 'grid');
        viewListBtn.classList.toggle('active', mode === 'list');
        // Show/hide the list-only header row
        const listHeader = document.querySelector('.file-list-header');
        if (listHeader) listHeader.style.display = mode === 'grid' ? 'none' : '';
        renderFileList();
    }
    viewGridBtn.addEventListener('click', () => applyViewMode('grid'));
    viewListBtn.addEventListener('click', () => applyViewMode('list'));
    // Apply saved mode on init
    viewGridBtn.classList.toggle('active', state.viewMode === 'grid');
    viewListBtn.classList.toggle('active', state.viewMode === 'list');

    // Settings btn
    document.getElementById('settingsBtn').addEventListener('click', () => switchView('tailscale'));

    // Discover peers
    document.getElementById('discoverPeersBtn').addEventListener('click', showDiscoverPeersModal);

    // Add peer manual
    document.getElementById('addPeerBtn').addEventListener('click', showAddPeerModal);

    // Tailscale connect/disconnect
    document.getElementById('tsConnectBtn').addEventListener('click', async () => {
        const key = document.getElementById('tsAuthKeyInput').value;
        try {
            await API.post('/api/tailscale/connect', { auth_key: key });
            toast('Tailscale conectado!');
            loadTailscaleStatus();
        } catch (e) { toast('Erro ao conectar', 'error'); }
    });

    document.getElementById('tsDisconnectBtn').addEventListener('click', async () => {
        try {
            await API.post('/api/tailscale/disconnect');
            toast('Tailscale desconectado');
            loadTailscaleStatus();
        } catch (e) { toast('Erro', 'error'); }
    });

    // Tailscale save config
    document.getElementById('tsSaveBtn') && document.getElementById('tsSaveBtn').addEventListener('click', async () => {});

    // Save general settings (legacy — replaced by saveAllBtn)
    document.getElementById('saveSettingsBtn') && document.getElementById('saveSettingsBtn').addEventListener('click', async () => {});

    // Botão único: salva Tailscale + todas as configurações
    document.getElementById('saveAllBtn').addEventListener('click', async () => {
        const key = document.getElementById('tsAuthKeyInput').value;
        const modeSelect = document.getElementById('tsModeSelect');
        const modes = ['auto', 'cli', 'tsnet'];
        const mode = modes[modeSelect.selectedIndex];

        try {
            await API.put('/api/settings', {
                ...(key ? { tailscale_auth_key: key } : {}),
                conflict_strategy: document.getElementById('settingConflict').value,
                scan_interval_sec: parseInt(document.getElementById('settingScanInterval').value) || 30,
                versioning_enabled: document.getElementById('settingVersioning').checked,
                max_file_versions: parseInt(document.getElementById('settingMaxVersions').value) || 10,
                integrity_check: document.getElementById('settingIntegrity').checked,
                peer_relay_enabled: document.getElementById('settingRelay').checked,
                web_remote_access: document.getElementById('settingRemoteAccess').checked,
                web_port: parseInt(document.getElementById('settingWebPort').value) || 8384,
            });
            await API.post('/api/tailscale/mode', { mode });
            toast('Configurações salvas — reinicie se alterou o acesso remoto');
            loadTailscaleStatus();
        } catch (e) { toast('Erro ao salvar', 'error'); }
    });

    // Refresh logs
    document.getElementById('refreshLogsBtn').addEventListener('click', loadLogs);

    // Search with dropdown suggestions
    const searchInput = document.getElementById('searchInput');
    let searchTimeout = null;
    let searchDropdown = document.getElementById('searchDropdown');
    if (!searchDropdown) {
        searchDropdown = document.createElement('div');
        searchDropdown.id = 'searchDropdown';
        searchDropdown.className = 'search-dropdown hidden';
        document.getElementById('searchBox').appendChild(searchDropdown);
    }

    searchInput.addEventListener('input', (e) => {
        const query = e.target.value.trim();
        if (searchTimeout) clearTimeout(searchTimeout);
        if (!query) {
            searchDropdown.classList.add('hidden');
            searchDropdown.innerHTML = '';
            // Restore current file list if browsing
            if (state.currentFolder) renderFileList();
            return;
        }
        searchTimeout = setTimeout(() => performSearch(query), 250);
    });

    searchInput.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            searchDropdown.classList.add('hidden');
            searchInput.blur();
        }
    });

    // Close dropdown when clicking outside
    document.addEventListener('click', (e) => {
        if (!document.getElementById('searchBox').contains(e.target)) {
            searchDropdown.classList.add('hidden');
        }
    });
}

async function performSearch(query) {
    const dropdown = document.getElementById('searchDropdown');
    try {
        const data = await API.get(`/api/search?q=${encodeURIComponent(query)}`);
        const results = data.results || [];

        if (results.length === 0) {
            dropdown.innerHTML = '<div class="search-no-results">Nenhum resultado encontrado</div>';
            dropdown.classList.remove('hidden');
            return;
        }

        dropdown.innerHTML = results.map(r => {
            const icon = getFileIcon(r.mime_type);
            const size = r.is_dir ? 'Pasta' : formatSize(r.size);
            const pathParts = r.path.split('/');
            const parentPath = pathParts.length > 1 ? pathParts.slice(0, -1).join('/') : '';
            return `
                <div class="search-result-item" data-folder-id="${escAttr(r.folder_id)}" data-path="${escAttr(r.path)}" data-is-dir="${r.is_dir}" data-name="${escAttr(r.name)}" data-mime="${escAttr(r.mime_type)}" data-size="${r.size || 0}" data-mod="${escAttr(r.mod_time || '')}">
                    <div class="search-result-icon ${r.mime_type}">${icon}</div>
                    <div class="search-result-info">
                        <div class="search-result-name">${escHTML(r.name)}</div>
                        <div class="search-result-path">${escHTML(r.folder_label || r.folder_id)}${parentPath ? ' › ' + escHTML(parentPath) : ''}</div>
                    </div>
                    <div class="search-result-meta">${size}</div>
                </div>
            `;
        }).join('');

        // Add click handlers
        dropdown.querySelectorAll('.search-result-item').forEach(item => {
            item.addEventListener('click', () => {
                const folderID = item.dataset.folderId;
                const path = item.dataset.path;
                const isDir = item.dataset.isDir === 'true';
                const name = item.dataset.name;
                const mimeType = item.dataset.mime;
                const size = parseInt(item.dataset.size) || 0;
                const modTime = item.dataset.mod;
                navigateToSearchResult(folderID, path, isDir, name, mimeType, size, modTime);
                dropdown.classList.add('hidden');
                document.getElementById('searchInput').value = '';
            });
        });

        dropdown.classList.remove('hidden');
    } catch (e) {
        console.error('Search error:', e);
        dropdown.innerHTML = '<div class="search-no-results">Erro na busca</div>';
        dropdown.classList.remove('hidden');
    }
}

function navigateToSearchResult(folderID, filePath, isDir, name, mimeType, size, modTime) {
    // Find the folder config
    const folder = state.folders.find(f => f.id === folderID || f.label === folderID);
    if (!folder) {
        toast('Pasta não encontrada', 'error');
        return;
    }

    // Set folder context so openFilePreview/openMarkdownEditor work
    state.currentFolder = folder.id || folder.label;
    document.getElementById('folderSelector').classList.add('hidden');
    document.getElementById('fileBrowser').classList.remove('hidden');

    // Update breadcrumb to parent path
    const parts = filePath.split('/');
    const parentPath = parts.length > 1 ? parts.slice(0, -1).join('/') : '';
    state.currentPath = parentPath;
    updateBreadcrumb(folder.label || folder.id, parentPath);

    if (isDir) {
        // For directories, navigate into them and show files view
        switchView('files');
        state.currentPath = filePath;
        updateBreadcrumb(folder.label || folder.id, filePath);
        loadFiles(state.currentFolder, filePath);
    } else {
        // Build a file object compatible with openFilePreview/openMarkdownEditor
        const file = {
            name: name,
            path: filePath,
            is_dir: false,
            size: size,
            mod_time: modTime,
            mime_type: mimeType,
        };

        // Load the parent directory file list in the background
        loadFiles(state.currentFolder, parentPath);

        // Open the file directly
        openFilePreview(file);
    }
}

