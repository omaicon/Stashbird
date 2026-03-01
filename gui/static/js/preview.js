// ═══════════════════════════════════════════════
// FILE PREVIEW MODULE
// ═══════════════════════════════════════════════

// ── File type detection helpers ──
function getPreviewCategory(file) {
    const name = (file.name || '').toLowerCase();
    const ext = name.substring(name.lastIndexOf('.'));
    const mime = (file.mime_type || '').toLowerCase();

    // By mime_type from server
    if (mime === 'image') return 'image';
    if (mime === 'video') return 'video';
    if (mime === 'audio') return 'audio';
    if (mime === 'pdf') return 'pdf';
    if (mime === 'text' || mime === 'code') return 'text';

    // Fallback: by extension
    const imageExts = ['.png', '.jpg', '.jpeg', '.gif', '.webp', '.bmp', '.svg', '.ico'];
    const videoExts = ['.mp4', '.webm', '.ogg', '.ogv', '.avi', '.mkv', '.mov'];
    const audioExts = ['.mp3', '.wav', '.flac', '.aac', '.oga', '.wma', '.m4a'];
    const pdfExts = ['.pdf'];
    const textExts = [
        '.txt', '.md', '.log', '.csv', '.json', '.xml', '.html', '.css', '.js',
        '.go', '.py', '.java', '.c', '.cpp', '.h', '.rs', '.ts', '.yaml', '.yml',
        '.toml', '.ini', '.sh', '.bat', '.ps1', '.env', '.gitignore', '.dockerfile',
    ];

    if (imageExts.includes(ext)) return 'image';
    if (videoExts.includes(ext)) return 'video';
    if (audioExts.includes(ext)) return 'audio';
    if (pdfExts.includes(ext)) return 'pdf';
    if (textExts.includes(ext)) return 'text';

    return 'unknown';
}

function getFileServeURL(folderID, filePath, download) {
    let url = `/api/files/serve?folder=${encodeURIComponent(folderID)}&path=${encodeURIComponent(filePath)}`;
    if (download) url += '&download=1';
    return url;
}

// ── Open File Preview ──
function openFilePreview(file) {
    if (!file || !state.currentFolder) return;

    const folderID = state.currentFolder;
    const category = getPreviewCategory(file);

    // Markdown files still open in the editor
    if (!file.is_dir && file.name.toLowerCase().endsWith('.md')) {
        openMarkdownEditor(file);
        return;
    }

    const overlay = document.getElementById('previewOverlay');
    const body = document.getElementById('previewModalBody');
    const fileName = document.getElementById('previewFileName');
    const downloadBtn = document.getElementById('previewDownloadBtn');

    // Set header info
    fileName.textContent = file.name;
    downloadBtn.href = getFileServeURL(folderID, file.path, true);
    downloadBtn.download = file.name;

    // Show loading
    body.innerHTML = '<div class="preview-loading"><div class="preview-spinner"></div><span>Carregando...</span></div>';
    overlay.classList.remove('hidden');

    // Render content based on type
    switch (category) {
        case 'image':
            renderImagePreview(body, folderID, file);
            break;
        case 'video':
            renderVideoPreview(body, folderID, file);
            break;
        case 'audio':
            renderAudioPreview(body, folderID, file);
            break;
        case 'pdf':
            renderPDFPreview(body, folderID, file);
            break;
        case 'text':
            renderTextPreview(body, folderID, file);
            break;
        default:
            renderFallbackPreview(body, folderID, file);
            break;
    }
}

// ── Image Preview ──
function renderImagePreview(container, folderID, file) {
    const url = getFileServeURL(folderID, file.path, false);
    const img = document.createElement('img');
    img.className = 'preview-image';
    img.alt = file.name;
    img.src = url;
    img.onload = () => {
        container.innerHTML = '';
        container.appendChild(img);
    };
    img.onerror = () => {
        renderFallbackPreview(container, folderID, file, 'Não foi possível carregar a imagem.');
    };
}

// ── Video Preview ──
function renderVideoPreview(container, folderID, file) {
    const url = getFileServeURL(folderID, file.path, false);
    container.innerHTML = '';

    const video = document.createElement('video');
    video.className = 'preview-video';
    video.controls = true;
    video.autoplay = false;
    video.preload = 'metadata';
    video.src = url;

    video.onerror = () => {
        renderFallbackPreview(container, folderID, file, 'Formato de vídeo não suportado pelo navegador.');
    };

    container.appendChild(video);
}

// ── Audio Preview ──
function renderAudioPreview(container, folderID, file) {
    const url = getFileServeURL(folderID, file.path, false);
    container.innerHTML = '';

    const wrapper = document.createElement('div');
    wrapper.className = 'preview-audio-container';

    wrapper.innerHTML = `
        <div class="preview-audio-icon">
            <svg viewBox="0 0 24 24"><path d="M12 3v10.55c-.59-.34-1.27-.55-2-.55-2.21 0-4 1.79-4 4s1.79 4 4 4 4-1.79 4-4V7h4V3h-6z"/></svg>
        </div>
        <div class="preview-audio-name">${escHTML(file.name)}</div>
    `;

    const audio = document.createElement('audio');
    audio.className = 'preview-audio';
    audio.controls = true;
    audio.preload = 'metadata';
    audio.src = url;

    audio.onerror = () => {
        renderFallbackPreview(container, folderID, file, 'Formato de áudio não suportado pelo navegador.');
    };

    wrapper.appendChild(audio);
    container.appendChild(wrapper);
}

// ── PDF Preview ──
function renderPDFPreview(container, folderID, file) {
    const url = getFileServeURL(folderID, file.path, false);
    container.innerHTML = '';

    const iframe = document.createElement('iframe');
    iframe.className = 'preview-pdf';
    iframe.src = url;
    iframe.title = file.name;

    iframe.onerror = () => {
        renderFallbackPreview(container, folderID, file, 'Não foi possível exibir o PDF.');
    };

    container.appendChild(iframe);
}

// ── Text/Code Preview ──
async function renderTextPreview(container, folderID, file) {
    try {
        const data = await API.get(`/api/files/read?folder=${encodeURIComponent(folderID)}&path=${encodeURIComponent(file.path)}`);
        if (data.error) {
            renderFallbackPreview(container, folderID, file, data.error);
            return;
        }

        container.innerHTML = '';
        const wrapper = document.createElement('div');
        wrapper.className = 'preview-text-container';

        const pre = document.createElement('pre');
        pre.textContent = data.content || '';

        wrapper.appendChild(pre);
        container.appendChild(wrapper);
    } catch (e) {
        renderFallbackPreview(container, folderID, file, 'Erro ao carregar arquivo de texto.');
    }
}

// ── Fallback (unsupported types) ──
function renderFallbackPreview(container, folderID, file, message) {
    const url = getFileServeURL(folderID, file.path, true);
    container.innerHTML = `
        <div class="preview-fallback">
            <div class="preview-fallback-icon">
                <svg viewBox="0 0 24 24"><path d="M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm4 18H6V4h7v5h5v11z"/></svg>
            </div>
            <h3>${escHTML(file.name)}</h3>
            <p>${message ? escHTML(message) : 'Este tipo de arquivo não pode ser pré-visualizado no navegador.'}</p>
            <p style="font-size:12px;color:#80868b;">${file.size ? formatSize(file.size) : ''}</p>
            <a class="btn-primary" href="${url}" download="${escAttr(file.name)}">
                <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><path d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z"/></svg>
                Baixar Arquivo
            </a>
        </div>
    `;
}

// ── Close Preview ──
function closeFilePreview() {
    const overlay = document.getElementById('previewOverlay');
    const body = document.getElementById('previewModalBody');

    // Stop any playing media
    const video = body.querySelector('video');
    if (video) { video.pause(); video.src = ''; }
    const audio = body.querySelector('audio');
    if (audio) { audio.pause(); audio.src = ''; }

    body.innerHTML = '';
    overlay.classList.add('hidden');
}

// ── Init Preview Events ──
function initPreviewEvents() {
    document.getElementById('previewCloseBtn').addEventListener('click', closeFilePreview);
    document.getElementById('previewOverlay').addEventListener('click', (e) => {
        if (e.target.id === 'previewOverlay') {
            closeFilePreview();
        }
    });

    // ESC to close
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') {
            const overlay = document.getElementById('previewOverlay');
            if (overlay && !overlay.classList.contains('hidden')) {
                e.preventDefault();
                closeFilePreview();
            }
        }
    });
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    initPreviewEvents();
});
