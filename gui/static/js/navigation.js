// ═══════════ NAVIGATION ═══════════
function initNavigation() {
    document.querySelectorAll('.nav-item[data-view]').forEach(item => {
        item.addEventListener('click', () => {
            const view = item.dataset.view;
            switchView(view);
        });
    });
}

function switchView(view) {
    state.currentView = view;
    document.querySelectorAll('.nav-item').forEach(n => n.classList.toggle('active', n.dataset.view === view));
    document.querySelectorAll('.view').forEach(v => v.classList.toggle('active', v.id === 'view' + capitalize(view)));

    if (view === 'devices') loadPeers();
    if (view === 'tailscale') loadTailscaleStatus();
    if (view === 'logs') loadLogs();
    if (view === 'files') loadFolders();
    if (view === 'graph') loadGraphView();
}

function capitalize(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

// ═══════════ EXIT ═══════════
function initExitButton() {
    const exitBtn = document.getElementById('navExit');
    if (exitBtn) {
        exitBtn.addEventListener('click', async () => {
            if (confirm('Tem certeza que deseja encerrar o Stashbird?')) {
                try {
                    await fetch('/api/shutdown', { method: 'POST' });
                } catch (_) {
                    // Conexão será encerrada pelo servidor
                }
                document.body.innerHTML = '<div style="display:flex;align-items:center;justify-content:center;height:100vh;font-family:Inter,sans-serif;color:#5f6368;"><h2>Stashbird encerrado. Você pode fechar esta aba.</h2></div>';
            }
        });
    }
}

// ═══════════ SIDEBAR ═══════════
function initSidebar() {
    document.getElementById('menuToggle').addEventListener('click', () => {
        document.getElementById('sidebar').classList.toggle('collapsed');
    });
}

