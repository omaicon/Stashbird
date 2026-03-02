// ═══════════ STATUS POLLING ═══════════
function startPolling() {
    state.pollTimer = setInterval(() => {
        loadStatus();
        if (state.currentView === 'files' && state.currentFolder) {
            loadFiles(state.currentFolder, state.currentPath);
        }
        if (state.currentView === 'devices') loadPeers();
    }, 3000);
}

async function loadStatus() {
    try {
        const data = await API.get('/api/status');
        updateStatusIndicator(data);
        updateDeviceInfo(data);
        updateStorageLabel(data);
        updateRemoteAccessInfo(data);
    } catch (e) { console.error('Status error:', e); }
}

function updateStatusIndicator(data) {
    const dot = document.querySelector('#syncIndicator .sync-dot');
    const text = document.getElementById('syncStatusText');
    const sync = data.sync;

    if (sync && sync.IsSyncing) {
        dot.className = 'sync-dot syncing';
        const pct = sync.SyncProgress ? Math.round(sync.SyncProgress * 100) : 0;
        text.textContent = `Sincronizando... ${pct}%`;
    } else if (data.tailscale && !data.tailscale.connected) {
        dot.className = 'sync-dot error';
        text.textContent = 'Tailscale desconectado';
    } else {
        dot.className = 'sync-dot synced';
        text.textContent = 'Sincronizado';
    }
}

function updateDeviceInfo(data) {
    const el = document.getElementById('deviceInfoText');
    el.textContent = `${data.device_name} • ${data.device_id?.substring(0, 12)}... • Porta ${data.listen_port}`;
}

function updateStorageLabel(data) {
    const el = document.getElementById('storageLabel');
    el.textContent = `${data.folders_count} pasta(s) • ${data.peers_count} dispositivo(s)`;
}

function updateRemoteAccessInfo(data) {
    const infoBox = document.getElementById('remoteAccessInfo');
    const urlEl = document.getElementById('remoteAccessURL');
    const disabledBox = document.getElementById('remoteAccessDisabled');
    if (!infoBox || !urlEl || !disabledBox) return;

    if (data.web_remote_access && data.remote_url) {
        infoBox.classList.remove('hidden');
        urlEl.textContent = data.remote_url;
        disabledBox.style.display = 'none';
    } else {
        infoBox.classList.add('hidden');
        urlEl.textContent = '';
        disabledBox.style.display = '';
    }
}

