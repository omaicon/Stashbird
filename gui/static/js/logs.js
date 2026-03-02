// ═══════════ LOGS ═══════════
async function loadLogs() {
    try {
        const logs = await API.get('/api/logs');
        const container = document.getElementById('logContainer');
        if (!logs || logs.length === 0) {
            container.innerHTML = '<div class="empty-state"><p>Nenhum log disponível.</p></div>';
            return;
        }
        container.innerHTML = logs.map(l =>
            `<div class="log-entry"><span class="log-time">${escHTML(l.time)}</span><span class="log-msg">${escHTML(l.message)}</span></div>`
        ).join('');
        container.scrollTop = container.scrollHeight;
    } catch (e) { console.error('Logs error:', e); }
}

