// ═══════════════════════════════════════════════
// Stashbird — SPA Application Logic
// ═══════════════════════════════════════════════

const API = {
    get: async (url) => {
        const r = await fetch(url);
        if (!r.ok) {
            let msg = `HTTP ${r.status}`;
            try { const j = await r.json(); if (j.error) msg = j.error; } catch (_) {}
            return { error: msg };
        }
        return r.json();
    },
    post: async (url, data) => {
        const r = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) });
        if (!r.ok) {
            let msg = `HTTP ${r.status}`;
            try { const j = await r.json(); if (j.error) msg = j.error; } catch (_) {}
            return { error: msg };
        }
        return r.json();
    },
    put: async (url, data) => {
        const r = await fetch(url, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) });
        if (!r.ok) {
            let msg = `HTTP ${r.status}`;
            try { const j = await r.json(); if (j.error) msg = j.error; } catch (_) {}
            return { error: msg };
        }
        return r.json();
    },
    del: async (url) => {
        const r = await fetch(url, { method: 'DELETE' });
        if (!r.ok) {
            let msg = `HTTP ${r.status}`;
            try { const j = await r.json(); if (j.error) msg = j.error; } catch (_) {}
            return { error: msg };
        }
        return r.json();
    },
};

