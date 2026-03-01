// ═══════════ TAILSCALE ═══════════
async function loadTailscaleStatus() {
    try {
        const data = await API.get('/api/tailscale/status');
        const badge = document.getElementById('tsConnBadge');
        badge.textContent = data.connected ? 'Conectado' : 'Desconectado';
        badge.className = 'badge ' + (data.connected ? 'connected' : 'disconnected');
        document.getElementById('tsIP').textContent = data.ip || '—';
        document.getElementById('tsMode').textContent = data.mode || '—';
        document.getElementById('tsCLI').textContent = data.cli_available ? 'Sim ✓' : 'Não ✗';

        const warning = document.getElementById('tsWarning');
        if (data.warning) {
            warning.textContent = '⚠ ' + data.warning;
            warning.classList.remove('hidden');
        } else {
            warning.classList.add('hidden');
        }

        // Set mode select
        const modeMap = { 'auto': 0, 'cli': 1, 'tsnet': 2 };
        document.getElementById('tsModeSelect').selectedIndex = modeMap[data.config_mode] || 0;

        // Load general settings too
        loadSettings();
    } catch (e) { console.error('Tailscale error:', e); }
}

async function loadSettings() {
    try {
        const s = await API.get('/api/settings');
        document.getElementById('settingConflict').value = s.conflict_strategy || 'rename';
        document.getElementById('settingScanInterval').value = s.scan_interval_sec || 30;
        document.getElementById('settingVersioning').checked = s.versioning_enabled !== false;
        document.getElementById('settingMaxVersions').value = s.max_file_versions || 10;
        document.getElementById('settingIntegrity').checked = s.integrity_check !== false;
        document.getElementById('settingRelay').checked = s.peer_relay_enabled === true;
        document.getElementById('settingRemoteAccess').checked = s.web_remote_access === true;
        document.getElementById('settingWebPort').value = s.web_port || 8384;
    } catch (e) { }
}

