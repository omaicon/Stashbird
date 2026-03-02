// ── Init ──
document.addEventListener('DOMContentLoaded', () => {
    initNavigation();
    initSidebar();
    initThemeToggle();
    initExitButton();
    initModals();
    initActions();
    initGraphView();
    loadStatus();
    loadFolders();
    startPolling();
});

// ═══════════ THEME TOGGLE ═══════════
function initThemeToggle() {
    const saved = localStorage.getItem('stashbird-theme');
    if (saved === 'dark') applyTheme('dark');

    const btn = document.getElementById('navTheme');
    if (btn) {
        btn.addEventListener('click', () => {
            const current = document.documentElement.getAttribute('data-theme');
            const next = current === 'dark' ? 'light' : 'dark';
            applyTheme(next);
            localStorage.setItem('stashbird-theme', next);
        });
    }
}

function applyTheme(theme) {
    const sunIcon = document.getElementById('themeIconSun');
    const moonIcon = document.getElementById('themeIconMoon');
    if (theme === 'dark') {
        document.documentElement.setAttribute('data-theme', 'dark');
        if (sunIcon) sunIcon.style.display = 'none';
        if (moonIcon) moonIcon.style.display = 'block';
    } else {
        document.documentElement.removeAttribute('data-theme');
        if (sunIcon) sunIcon.style.display = 'block';
        if (moonIcon) moonIcon.style.display = 'none';
    }
}

