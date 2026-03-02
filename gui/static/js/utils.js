// ═══════════ UTILITIES ═══════════
function formatSize(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function escHTML(s) {
    if (!s) return '';
    const d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
}

function escAttr(s) {
    return (s || '').replace(/'/g, "\\'").replace(/"/g, '&quot;');
}

function toast(msg, type) {
    const container = document.getElementById('toastContainer');
    const t = document.createElement('div');
    t.className = 'toast';
    t.textContent = msg;
    if (type === 'error') t.style.background = '#d93025';
    container.appendChild(t);
    setTimeout(() => t.remove(), 3000);
}

