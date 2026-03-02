// ═══════════ PEERS / DEVICES ═══════════
async function loadPeers() {
    try {
        state.peers = await API.get('/api/peers');
        renderDeviceList();
    } catch (e) { console.error('Peers error:', e); }
}

function renderDeviceList() {
    const list = document.getElementById('deviceList');
    if (!state.peers || state.peers.length === 0) {
        list.innerHTML = `
            <div class="empty-state">
                <svg width="64" height="64" viewBox="0 0 24 24" fill="#dadce0"><path d="M4 6h18V4H4c-1.1 0-2 .9-2 2v11H0v3h14v-3H4V6zm19 2h-6c-.55 0-1 .45-1 1v10c0 .55.45 1 1 1h6c.55 0 1-.45 1-1V9c0-.55-.45-1-1-1zm-1 9h-4v-7h4v7z"/></svg>
                <h3>Nenhum dispositivo</h3>
                <p>Busque dispositivos na rede Tailscale ou adicione manualmente.</p>
            </div>`;
        return;
    }

    list.innerHTML = '';
    state.peers.forEach(p => {
        const isConn = p.connected;
        const initials = (p.name || '?').substring(0, 2).toUpperCase();
        const card = document.createElement('div');
        card.className = 'device-card';
        card.innerHTML = `
            <div class="device-avatar ${isConn ? 'online' : 'offline'}">${initials}</div>
            <div class="device-details">
                <div class="device-name">${escHTML(p.name)}</div>
                <div class="device-ip">${escHTML(p.tailscale_ip)} :${p.port}</div>
            </div>
            <div class="device-status">
                <span class="dot ${isConn ? 'online' : 'offline'}"></span>
                ${isConn ? 'Conectado' : 'Desconectado'}
            </div>
            <div class="device-actions">
                <button class="icon-btn small" title="Remover" onclick="removePeer('${escAttr(p.id)}')">
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="#5f6368"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>
                </button>
            </div>`;
        list.appendChild(card);
    });
}

async function removePeer(id) {
    if (!confirm('Remover este dispositivo?')) return;
    try {
        await API.del(`/api/peers?id=${encodeURIComponent(id)}`);
        toast('Dispositivo removido');
        loadPeers();
    } catch (e) { toast('Erro ao remover dispositivo', 'error'); }
}

