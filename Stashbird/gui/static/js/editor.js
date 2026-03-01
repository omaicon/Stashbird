// ═══════════════════════════════════════════════
// MARKDOWN EDITOR MODULE
// ═══════════════════════════════════════════════

const editor = {
    view: null,          // CodeMirror EditorView instance
    currentFile: null,   // { folder, path, name }
    modified: false,
    previewTimer: null,
    cmLoaded: false,
    cmModules: {},       // cached CM modules
    notesList: [],       // for wikilink autocomplete
    imageFolder: 'attachments', // configured image folder
};

// ── PDF Config State ──
const pdfConfig = {
    // Text
    font: "Merriweather, serif",
    fontSize: 15,
    lineHeight: 1.6,
    textColor: '#1a1a1a',
    paragraphSpacing: 4,
    headingStyle: 'comfortable',
    // Code
    codeSize: 10,
    codeBg: '#f5f5f5',
    // Page
    pageFormat: 'a4',
    orientation: 'portrait',
    marginTop: 15,
    marginBottom: 15,
    marginLeft: 15,
    marginRight: 15,
    pageBg: '#ffffff',
    // Header / Footer
    headerEnabled: false,
    headerText: '',
    headerAlign: 'center',
    footerEnabled: false,
    footerText: '',
    pageNumbers: false,
    // Brand
    watermarkEnabled: false,
    watermarkText: '',
    watermarkOpacity: 0.15,
    watermarkColor: '#888888',
    // CSS
    customCss: '',
};

// ── Load CodeMirror 6 from CDN ──
async function loadCodeMirror() {
    if (editor.cmLoaded) return;

    try {
        const [
            cmModule,
            stateModule,
            mdModule,
            viewModule,
            commandsModule,
        ] = await Promise.all([
            import('https://esm.sh/codemirror@6.0.1'),
            import('https://esm.sh/@codemirror/state@6.5.2'),
            import('https://esm.sh/@codemirror/lang-markdown@6.3.2'),
            import('https://esm.sh/@codemirror/view@6.36.5'),
            import('https://esm.sh/@codemirror/commands@6.8.0'),
        ]);

        const EditorView = viewModule.EditorView || cmModule.EditorView;
        const basicSetup = cmModule.basicSetup;
        const EditorState = stateModule.EditorState;
        const markdown = mdModule.markdown;
        const keymap = viewModule.keymap;
        const indentWithTab = commandsModule.indentWithTab;

        if (!EditorView || !EditorState || !basicSetup || !markdown || !keymap) {
            throw new Error('Missing required CodeMirror exports');
        }

        editor.cmModules = { EditorView, basicSetup, EditorState, markdown, keymap, indentWithTab };
        editor.cmLoaded = true;
        console.log('[Editor] CodeMirror 6 loaded successfully');
    } catch (e) {
        console.error('Failed to load CodeMirror:', e);
        toast('Erro ao carregar editor CodeMirror', 'error');
    }
}

// ── Initialize Editor ──
function createEditorView(content) {
    const { EditorView, basicSetup, EditorState, markdown, keymap, indentWithTab } = editor.cmModules;

    const host = document.getElementById('codemirrorHost');
    host.innerHTML = '';

    const saveKeymap = keymap.of([
        {
            key: 'Mod-s',
            run: () => { saveCurrentFile(); return true; },
        },
        {
            key: 'Mod-b',
            run: (view) => { toolbarAction('bold'); return true; },
        },
        {
            key: 'Mod-i',
            run: (view) => { toolbarAction('italic'); return true; },
        },
        indentWithTab,
    ]);

    const updateListener = EditorView.updateListener.of((update) => {
        if (update.docChanged) {
            editor.modified = true;
            updateSaveStatus('modified');
            schedulePreviewUpdate();
        }
    });

    const editorState = EditorState.create({
        doc: content || '',
        extensions: [
            basicSetup,
            markdown(),
            saveKeymap,
            updateListener,
            EditorView.lineWrapping,
        ],
    });

    editor.view = new EditorView({
        state: editorState,
        parent: host,
    });

    // Setup paste handler for images
    host.addEventListener('paste', handleImagePaste);

    return editor.view;
}

// ── Open Markdown File ──
async function openMarkdownEditor(file) {
    if (!file) return;

    const folderID = state.currentFolder;
    if (!folderID) {
        toast('Nenhuma pasta selecionada', 'error');
        return;
    }
    if (!file.path) {
        toast('Caminho do arquivo inválido', 'error');
        return;
    }

    // Show loading state
    switchView('editor');
    document.getElementById('editorFilename').textContent = file.name;
    document.getElementById('previewContent').innerHTML = '<p class="preview-placeholder">Carregando...</p>';

    // Load CodeMirror if not loaded
    if (!editor.cmLoaded) {
        document.getElementById('codemirrorHost').innerHTML = '<p style="padding:20px;color:#80868b;">Carregando editor...</p>';
        await loadCodeMirror();
    }

    // Read file content
    let content = '';
    try {
        const data = await API.get(`/api/files/read?folder=${encodeURIComponent(folderID)}&path=${encodeURIComponent(file.path)}`);

        if (data.error) {
            console.error('API error:', data.error);
            toast('Erro ao ler arquivo: ' + data.error, 'error');
            switchView('files');
            return;
        }
        content = data.content || '';
    } catch (e) {
        console.error('Error fetching file:', e);
        toast('Erro de conexão ao ler arquivo: ' + (e.message || e), 'error');
        switchView('files');
        return;
    }

    editor.currentFile = {
        folder: folderID,
        path: file.path,
        name: file.name,
    };
    editor.modified = false;

    // Create editor (CodeMirror or fallback textarea)
    try {
        if (editor.cmLoaded) {
            createEditorView(content);
        } else {
            createFallbackEditor(content);
        }
    } catch (e) {
        console.error('Error creating editor view:', e);
        createFallbackEditor(content);
    }

    // Render preview
    renderPreview(content);

    // Load backlinks
    loadBacklinks();

    // Load notes list for autocomplete
    loadNotesList();

    // Load image folder config for this folder
    loadImageFolderConfig();

    updateSaveStatus('saved');
}

// ── Fallback Textarea Editor (when CodeMirror unavailable) ──
function createFallbackEditor(content) {
    const host = document.getElementById('codemirrorHost');
    host.innerHTML = '';
    const ta = document.createElement('textarea');
    ta.id = 'fallbackEditor';
    ta.value = content || '';
    ta.style.cssText = 'width:100%;height:100%;border:none;outline:none;resize:none;padding:16px;font-family:monospace;font-size:14px;background:var(--bg);color:var(--text-primary);';
    ta.addEventListener('input', () => {
        editor.modified = true;
        updateSaveStatus('modified');
        schedulePreviewUpdate();
    });
    ta.addEventListener('keydown', (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key === 'b') { e.preventDefault(); toolbarAction('bold'); }
        if ((e.ctrlKey || e.metaKey) && e.key === 'i') { e.preventDefault(); toolbarAction('italic'); }
        if ((e.ctrlKey || e.metaKey) && e.key === 's') { e.preventDefault(); saveCurrentFile(); }
    });
    host.appendChild(ta);

    // Setup paste handler for images on fallback editor too
    host.addEventListener('paste', handleImagePaste);

    // Provide an adapter with same surface as CodeMirror view
    editor.view = null;
    editor._fallbackTA = ta;
}

// ── Get Editor Content (works with CM6 and fallback) ──
function getEditorContent() {
    if (editor.view) return editor.view.state.doc.toString();
    if (editor._fallbackTA) return editor._fallbackTA.value;
    return '';
}

// ── Get Selection Info (works with CM6 and fallback) ──
function getEditorSelection() {
    if (editor.view) {
        try {
            const sel = editor.view.state.selection.main;
            return {
                from: sel.from,
                to: sel.to,
                text: editor.view.state.sliceDoc(sel.from, sel.to),
            };
        } catch (_) {}
    }
    if (editor._fallbackTA) {
        const ta = editor._fallbackTA;
        return {
            from: ta.selectionStart,
            to: ta.selectionEnd,
            text: ta.value.substring(ta.selectionStart, ta.selectionEnd),
        };
    }
    return { from: 0, to: 0, text: '' };
}

// ── Replace Selection / Insert at Cursor (works with CM6 and fallback) ──
function editorReplace(from, to, text, newCursorPos) {
    if (editor.view) {
        const tx = { changes: { from, to, insert: text } };
        if (typeof newCursorPos === 'number') {
            tx.selection = { anchor: newCursorPos };
        }
        editor.view.dispatch(tx);
        editor.view.focus();
        editor.modified = true;
        updateSaveStatus('modified');
        schedulePreviewUpdate();
        return;
    }
    if (editor._fallbackTA) {
        const ta = editor._fallbackTA;
        const val = ta.value;
        ta.value = val.substring(0, from) + text + val.substring(to);
        const cursor = typeof newCursorPos === 'number' ? newCursorPos : from + text.length;
        ta.selectionStart = ta.selectionEnd = cursor;
        ta.focus();
        ta.dispatchEvent(new Event('input'));
        return;
    }
}

// ── Get Line Info at Position (works with CM6 and fallback) ──
function getLineAt(pos) {
    if (editor.view) {
        try {
            const line = editor.view.state.doc.lineAt(pos);
            return { from: line.from, to: line.to, text: line.text };
        } catch (_) {}
    }
    if (editor._fallbackTA) {
        const val = editor._fallbackTA.value;
        let lineStart = val.lastIndexOf('\n', pos - 1) + 1;
        let lineEnd = val.indexOf('\n', pos);
        if (lineEnd === -1) lineEnd = val.length;
        return { from: lineStart, to: lineEnd, text: val.substring(lineStart, lineEnd) };
    }
    return { from: 0, to: 0, text: '' };
}

// ── Focus Editor ──
function focusEditor() {
    if (editor.view) { editor.view.focus(); return; }
    if (editor._fallbackTA) { editor._fallbackTA.focus(); return; }
}

// ── Save Current File ──
async function saveCurrentFile() {
    if (!editor.currentFile) return;
    if (!editor.view && !editor._fallbackTA) return;

    const content = getEditorContent();
    updateSaveStatus('saving');

    try {
        await API.put('/api/files/write', {
            folder: editor.currentFile.folder,
            path: editor.currentFile.path,
            content: content,
        });

        editor.modified = false;
        updateSaveStatus('saved');
        toast('Arquivo salvo!');

        // Refresh backlinks after save
        loadBacklinks();
    } catch (e) {
        updateSaveStatus('error');
        toast('Erro ao salvar', 'error');
    }
}

function updateSaveStatus(status) {
    const el = document.getElementById('editorSaveStatus');
    switch (status) {
        case 'saving':
            el.textContent = 'Salvando...';
            el.className = 'editor-save-status saving';
            break;
        case 'saved':
            el.textContent = 'Salvo ✓';
            el.className = 'editor-save-status saved';
            break;
        case 'modified':
            el.textContent = '● Não salvo';
            el.className = 'editor-save-status';
            break;
        case 'error':
            el.textContent = '✗ Erro ao salvar';
            el.className = 'editor-save-status error';
            break;
    }
}

// ── Live Preview ──
function schedulePreviewUpdate() {
    if (editor.previewTimer) clearTimeout(editor.previewTimer);
    editor.previewTimer = setTimeout(() => {
        const content = getEditorContent();
        if (content !== undefined) renderPreview(content);
    }, 300);
}

async function renderPreview(content) {
    try {
        const result = await API.post('/api/markdown/render', {
            content: content,
            folder_id: editor.currentFile ? editor.currentFile.folder : '',
        });

        const previewEl = document.getElementById('previewContent');
        previewEl.innerHTML = result.html || '<p class="preview-placeholder">Conteúdo vazio</p>';

        // Atualizar contagem de palavras na barra da pane
        const wordEl = document.getElementById('previewWordCount');
        if (wordEl && content) {
            const words = content.trim().split(/\s+/).filter(Boolean).length;
            const chars = content.length;
            wordEl.textContent = `${words} palavras · ${chars} chars`;
        }

        // Make wikilinks clickable
        previewEl.querySelectorAll('.wikilink').forEach(link => {
            link.addEventListener('click', (e) => {
                e.preventDefault();
                const target = link.dataset.target;
                if (target) openWikiLink(target);
            });
        });

    } catch (e) {
        console.error('Preview error:', e);
    }
}

function openWikiLink(target) {
    if (!state.currentFolder) return;

    // Try to navigate to the linked note
    const file = {
        name: target.split('/').pop(),
        path: target,
        is_dir: false,
        mime_type: 'text',
    };
    openMarkdownEditor(file);
}

// ── Export Preview to PDF (uses pdfConfig) ──
async function exportPreviewToPDF() {
    const previewEl = document.getElementById('previewContent');
    if (!previewEl || !previewEl.innerHTML || previewEl.querySelector('.preview-placeholder')) {
        toast('Nenhum conteúdo para exportar', 'error');
        return;
    }

    const fileName = editor.currentFile
        ? editor.currentFile.name.replace(/\.md$/i, '') + '.pdf'
        : 'documento.pdf';

    toast('Gerando PDF...');

    // Clone the preview content
    const clone = previewEl.cloneNode(true);

    // ── Apply pdfConfig styles to the clone ──
    const ptToPx = (pt) => (pt * 96 / 72).toFixed(2) + 'px';

    clone.style.padding = '0';
    clone.style.color = pdfConfig.textColor;
    clone.style.backgroundColor = pdfConfig.pageBg;
    clone.style.fontFamily = pdfConfig.font;
    clone.style.fontSize = ptToPx(pdfConfig.fontSize);
    clone.style.lineHeight = String(pdfConfig.lineHeight);

    // Paragraph spacing
    clone.querySelectorAll('p').forEach(p => {
        p.style.marginBottom = pdfConfig.paragraphSpacing + 'mm';
    });

    // Heading margin based on style
    const headingMargins = {
        compact:   { top: '8px',  bottom: '4px'  },
        comfortable: { top: '20px', bottom: '10px' },
        spacious:  { top: '36px', bottom: '16px' },
    };
    const hm = headingMargins[pdfConfig.headingStyle] || headingMargins.comfortable;
    clone.querySelectorAll('h1, h2, h3, h4, h5, h6').forEach(h => {
        h.style.marginTop    = hm.top;
        h.style.marginBottom = hm.bottom;
        h.style.color = pdfConfig.textColor;
    });

    // Code blocks
    clone.querySelectorAll('pre, code').forEach(el => {
        el.style.fontSize = ptToPx(pdfConfig.codeSize);
        el.style.backgroundColor = pdfConfig.codeBg;
    });

    // Force all text to use configured color (avoid dark-mode overrides)
    clone.querySelectorAll('*').forEach(el => {
        if (!el.style.color) el.style.color = pdfConfig.textColor;
    });

    // Ensure images fit
    clone.querySelectorAll('img').forEach(img => {
        img.style.maxWidth = '100%';
        img.style.height = 'auto';
        img.style.display = 'block';
        img.style.margin = '8px 0';
    });

    // Build wrapper with optional header/footer/watermark
    const wrapper = document.createElement('div');
    wrapper.style.cssText = `background:${pdfConfig.pageBg};position:relative;`;

    // Header
    if (pdfConfig.headerEnabled && pdfConfig.headerText) {
        const hdr = document.createElement('div');
        hdr.style.cssText = `text-align:${pdfConfig.headerAlign};font-size:10px;color:#666;padding:8px 0 16px;border-bottom:1px solid #ddd;margin-bottom:20px;font-family:${pdfConfig.font};`;
        hdr.textContent = pdfConfig.headerText;
        wrapper.appendChild(hdr);
    }

    wrapper.appendChild(clone);

    // Footer
    if (pdfConfig.footerEnabled && (pdfConfig.footerText || pdfConfig.pageNumbers)) {
        const ftr = document.createElement('div');
        ftr.style.cssText = `text-align:center;font-size:10px;color:#666;padding:16px 0 8px;border-top:1px solid #ddd;margin-top:20px;font-family:${pdfConfig.font};`;
        ftr.textContent = pdfConfig.footerText + (pdfConfig.pageNumbers ? '  •  Página 1' : '');
        wrapper.appendChild(ftr);
    }

    // Watermark
    if (pdfConfig.watermarkEnabled && pdfConfig.watermarkText) {
        const wm = document.createElement('div');
        wm.style.cssText = `position:absolute;top:50%;left:50%;transform:translate(-50%,-50%) rotate(-35deg);font-size:72px;font-weight:900;color:${pdfConfig.watermarkColor};opacity:${pdfConfig.watermarkOpacity};pointer-events:none;white-space:nowrap;z-index:999;font-family:${pdfConfig.font};`;
        wm.textContent = pdfConfig.watermarkText;
        wrapper.appendChild(wm);
    }

    // Custom CSS
    if (pdfConfig.customCss) {
        const style = document.createElement('style');
        style.textContent = pdfConfig.customCss;
        wrapper.appendChild(style);
    }

    const margins = [pdfConfig.marginTop, pdfConfig.marginRight, pdfConfig.marginBottom, pdfConfig.marginLeft];

    const opt = {
        margin:       margins,
        filename:     fileName,
        image:        { type: 'jpeg', quality: 0.97 },
        html2canvas:  { scale: 2, useCORS: true, allowTaint: true, logging: false, backgroundColor: pdfConfig.pageBg },
        jsPDF:        { unit: 'mm', format: pdfConfig.pageFormat, orientation: pdfConfig.orientation },
        pagebreak:    { mode: ['avoid-all', 'css', 'legacy'] },
    };

    try {
        await html2pdf().set(opt).from(wrapper).save();
        toast('PDF exportado com sucesso!');
    } catch (e) {
        console.error('Erro ao exportar PDF:', e);
        toast('Erro ao gerar PDF: ' + (e.message || e), 'error');
    }
}

// ── Image Paste Handler ──
async function handleImagePaste(e) {
    if (!editor.currentFile) return;

    const items = e.clipboardData?.items;
    if (!items) return;

    for (const item of items) {
        if (item.type.startsWith('image/')) {
            e.preventDefault();
            e.stopPropagation();

            const blob = item.getAsFile();
            if (!blob) return;

            // Upload image to configured image folder
            const formData = new FormData();
            formData.append('image', blob, `paste.${blob.type.split('/')[1]}`);
            formData.append('folder', editor.currentFile.folder);
            formData.append('note_path', editor.currentFile.path);

            try {
                toast('Enviando imagem...');
                const response = await fetch('/api/files/upload-image', {
                    method: 'POST',
                    body: formData,
                });
                const result = await response.json();

                if (result.filename) {
                    const imgSyntax = `![[${result.filename}]]`;
                    insertTextAtCursor(imgSyntax);
                    toast('Imagem salva em: ' + (result.image_folder || editor.imageFolder));
                    // Update preview after inserting image
                    schedulePreviewUpdate();
                } else if (result.error) {
                    toast('Erro: ' + result.error, 'error');
                }
            } catch (err) {
                toast('Erro ao enviar imagem', 'error');
                console.error('Image upload error:', err);
            }
            return;
        }
    }
}

// ── Insert text at cursor position (uses unified helpers) ──
function insertTextAtCursor(text) {
    const sel = getEditorSelection();
    editorReplace(sel.from, sel.from, text);
}

// ── Toolbar Actions ──
function toolbarAction(action) {
    // Actions that don't need editor — handle first
    if (action === 'graph') { showGraphView(); return; }
    if (action === 'imageFolder') { showImageFolderConfig(); return; }
    if (action === 'exportPDF') { exportPreviewToPDF(); return; }

    // Actions that need modals
    if (action === 'link') { showLinkModal(); return; }
    if (action === 'wikilink') { showWikilinkModal(); return; }
    if (action === 'image') { showImageInsertModal(); return; }
    if (action === 'callout') { showCalloutModal(); return; }

    // Direct text manipulation actions — need editor
    if (!editor.view && !editor._fallbackTA) return;

    const sel = getEditorSelection();
    const { from, to, text: selected } = sel;

    switch (action) {
        case 'bold': {
            // Toggle: if already wrapped in **, remove; otherwise add
            if (selected.startsWith('**') && selected.endsWith('**') && selected.length > 4) {
                editorReplace(from, to, selected.slice(2, -2));
            } else if (selected) {
                editorReplace(from, to, `**${selected}**`);
            } else {
                const ins = '**texto**';
                editorReplace(from, to, ins, from + 2);
            }
            break;
        }
        case 'italic': {
            if (selected.startsWith('*') && selected.endsWith('*') && !selected.startsWith('**')) {
                editorReplace(from, to, selected.slice(1, -1));
            } else if (selected) {
                editorReplace(from, to, `*${selected}*`);
            } else {
                const ins = '*texto*';
                editorReplace(from, to, ins, from + 1);
            }
            break;
        }
        case 'heading': {
            const line = getLineAt(from);
            const match = line.text.match(/^(#{1,6})\s/);
            if (match) {
                const level = match[1].length;
                if (level >= 6) {
                    // Remove heading
                    editorReplace(line.from, line.from + match[0].length, '');
                } else {
                    // Increase level
                    const newPrefix = '#'.repeat(level + 1) + ' ';
                    editorReplace(line.from, line.from + match[0].length, newPrefix);
                }
            } else {
                // Add heading
                editorReplace(line.from, line.from, '## ');
            }
            break;
        }
        case 'ul': {
            // Handle multi-line: prefix each line
            if (selected.includes('\n')) {
                const lines = selected.split('\n');
                const toggled = lines.map(l => {
                    if (l.match(/^- /)) return l.substring(2);
                    return '- ' + l;
                }).join('\n');
                editorReplace(from, to, toggled);
            } else {
                const line = getLineAt(from);
                if (line.text.match(/^- /)) {
                    editorReplace(line.from, line.from + 2, '');
                } else {
                    editorReplace(line.from, line.from, '- ');
                }
            }
            break;
        }
        case 'checklist': {
            if (selected.includes('\n')) {
                const lines = selected.split('\n');
                const toggled = lines.map(l => {
                    if (l.match(/^- \[[ x]\] /)) return l.replace(/^- \[[ x]\] /, '');
                    return '- [ ] ' + l;
                }).join('\n');
                editorReplace(from, to, toggled);
            } else {
                const line = getLineAt(from);
                if (line.text.match(/^- \[ \] /)) {
                    // Toggle to checked
                    editorReplace(line.from, line.from + 6, '- [x] ');
                } else if (line.text.match(/^- \[x\] /)) {
                    // Remove checklist
                    editorReplace(line.from, line.from + 6, '');
                } else {
                    editorReplace(line.from, line.from, '- [ ] ');
                }
            }
            break;
        }
        case 'code': {
            if (selected.includes('\n')) {
                editorReplace(from, to, '```\n' + selected + '\n```');
            } else if (selected) {
                editorReplace(from, to, '`' + selected + '`');
            } else {
                const ins = '`código`';
                editorReplace(from, to, ins, from + 1);
            }
            break;
        }
        default:
            return;
    }
}

// ── Link Modal ──
function showLinkModal() {
    const sel = getEditorSelection();
    const selectedText = sel.text || '';

    showModal('Inserir Link', `
        <div class="form-group">
            <label>Texto do link</label>
            <input type="text" id="linkTextInput" value="${escAttr(selectedText)}" placeholder="Texto exibido">
        </div>
        <div class="form-group">
            <label>URL</label>
            <input type="text" id="linkUrlInput" placeholder="https://exemplo.com" autofocus>
        </div>
        <div style="margin-top:8px;font-size:12px;color:var(--text-muted);">
            Resultado: <code>[texto](url)</code>
        </div>
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" onclick="doInsertLink()">Inserir</button>
    `);

    setTimeout(() => {
        const urlInput = document.getElementById('linkUrlInput');
        if (urlInput) urlInput.focus();
        // Allow Enter to submit
        const handleEnter = (e) => { if (e.key === 'Enter') { e.preventDefault(); doInsertLink(); } };
        document.getElementById('linkTextInput')?.addEventListener('keydown', handleEnter);
        urlInput?.addEventListener('keydown', handleEnter);
    }, 100);
}

function doInsertLink() {
    const text = (document.getElementById('linkTextInput')?.value || '').trim();
    const url = (document.getElementById('linkUrlInput')?.value || '').trim();

    if (!url) {
        toast('Informe a URL do link', 'error');
        return;
    }

    const linkText = text || url;
    const markdown = `[${linkText}](${url})`;

    const sel = getEditorSelection();
    editorReplace(sel.from, sel.to, markdown);
    closeModal();
    focusEditor();
}

// ── Wikilink Modal with Autocomplete ──
function showWikilinkModal() {
    const sel = getEditorSelection();
    const selectedText = sel.text || '';

    const noteItems = (editor.notesList || []).map(n => {
        const name = typeof n === 'string' ? n : (n.name || n.path || '');
        return name;
    }).filter(Boolean);

    showModal('Inserir Wikilink', `
        <div class="form-group">
            <label>Nota destino</label>
            <input type="text" id="wikilinkInput" value="${escAttr(selectedText)}" placeholder="Digite para buscar notas..." autocomplete="off">
            <div id="wikilinkSuggestions" class="wikilink-suggestions"></div>
        </div>
        <div class="form-group">
            <label>Texto alternativo (opcional)</label>
            <input type="text" id="wikilinkAliasInput" placeholder="Texto exibido (se diferente)">
        </div>
        <div style="margin-top:8px;font-size:12px;color:var(--text-muted);">
            Resultado: <code>[[nota]]</code> ou <code>[[nota|alias]]</code>
        </div>
        ${noteItems.length > 0 ? `
        <div style="margin-top:12px;">
            <label style="font-weight:500;font-size:13px;">Notas existentes:</label>
            <div id="wikilinkNoteList" class="wikilink-note-list">
                ${noteItems.map(n => `
                    <div class="wikilink-note-item" data-note="${escAttr(n)}" onclick="selectWikilinkNote(this)">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor"><path d="M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z"/></svg>
                        ${escHTML(n)}
                    </div>
                `).join('')}
            </div>
        </div>` : ''}
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" onclick="doInsertWikilink()">Inserir</button>
    `);

    setTimeout(() => {
        const input = document.getElementById('wikilinkInput');
        if (input) {
            input.focus();
            input.addEventListener('input', filterWikilinkNotes);
            input.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); doInsertWikilink(); } });
        }
        document.getElementById('wikilinkAliasInput')?.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') { e.preventDefault(); doInsertWikilink(); }
        });
    }, 100);
}

function filterWikilinkNotes() {
    const input = document.getElementById('wikilinkInput');
    const query = (input?.value || '').toLowerCase();
    const items = document.querySelectorAll('.wikilink-note-item');
    items.forEach(item => {
        const note = (item.dataset.note || '').toLowerCase();
        item.style.display = (!query || note.includes(query)) ? '' : 'none';
    });
}

function selectWikilinkNote(el) {
    const note = el.dataset.note || '';
    const input = document.getElementById('wikilinkInput');
    if (input) input.value = note;
    // Highlight selected
    document.querySelectorAll('.wikilink-note-item').forEach(i => i.classList.remove('selected'));
    el.classList.add('selected');
}

function doInsertWikilink() {
    let target = (document.getElementById('wikilinkInput')?.value || '').trim();
    const alias = (document.getElementById('wikilinkAliasInput')?.value || '').trim();

    if (!target) {
        toast('Informe a nota destino', 'error');
        return;
    }

    // Remove .md extension for cleaner wikilinks
    target = target.replace(/\.md$/i, '');

    const markdown = alias ? `[[${target}|${alias}]]` : `[[${target}]]`;

    const sel = getEditorSelection();
    editorReplace(sel.from, sel.to, markdown);
    closeModal();
    focusEditor();
}

// ── Image Insert Modal ──
function showImageInsertModal() {
    showModal('Inserir Imagem', `
        <div class="editor-modal-tabs">
            <button class="editor-modal-tab active" onclick="switchImageTab('url')">URL</button>
            <button class="editor-modal-tab" onclick="switchImageTab('upload')">Upload</button>
        </div>
        <div id="imageTabUrl" class="editor-modal-tab-content">
            <div class="form-group">
                <label>Descrição (alt text)</label>
                <input type="text" id="imageAltInput" placeholder="Descrição da imagem">
            </div>
            <div class="form-group">
                <label>URL da imagem</label>
                <input type="text" id="imageUrlInput" placeholder="https://exemplo.com/imagem.png">
            </div>
            <div style="margin-top:8px;font-size:12px;color:var(--text-muted);">
                Resultado: <code>![descrição](url)</code>
            </div>
        </div>
        <div id="imageTabUpload" class="editor-modal-tab-content" style="display:none;">
            <div class="form-group">
                <label>Selecionar arquivo de imagem</label>
                <input type="file" id="imageFileInput" accept="image/*" style="padding:8px 0;">
            </div>
            <div id="imageUploadPreview" style="margin-top:8px;text-align:center;min-height:40px;"></div>
            <div style="margin-top:8px;font-size:12px;color:var(--text-muted);">
                A imagem será salva na pasta: <strong>${escHTML(editor.imageFolder)}</strong>
                <br>Resultado: <code>![[nome.ext]]</code>
            </div>
        </div>
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" id="imageInsertBtn" onclick="doInsertImage()">Inserir</button>
    `);

    setTimeout(() => {
        document.getElementById('imageAltInput')?.focus();
        document.getElementById('imageFileInput')?.addEventListener('change', previewImageUpload);
        const handleEnter = (e) => { if (e.key === 'Enter') { e.preventDefault(); doInsertImage(); } };
        document.getElementById('imageAltInput')?.addEventListener('keydown', handleEnter);
        document.getElementById('imageUrlInput')?.addEventListener('keydown', handleEnter);
    }, 100);
}

function switchImageTab(tab) {
    document.querySelectorAll('.editor-modal-tab').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.editor-modal-tab-content').forEach(c => c.style.display = 'none');
    if (tab === 'url') {
        document.querySelector('.editor-modal-tab:first-child').classList.add('active');
        document.getElementById('imageTabUrl').style.display = '';
    } else {
        document.querySelector('.editor-modal-tab:last-child').classList.add('active');
        document.getElementById('imageTabUpload').style.display = '';
    }
}

function previewImageUpload() {
    const file = document.getElementById('imageFileInput')?.files[0];
    const preview = document.getElementById('imageUploadPreview');
    if (!file || !preview) return;

    const reader = new FileReader();
    reader.onload = (e) => {
        preview.innerHTML = `<img src="${e.target.result}" style="max-width:100%;max-height:200px;border-radius:8px;border:1px solid var(--border);">`;
    };
    reader.readAsDataURL(file);
}

async function doInsertImage() {
    const urlTab = document.getElementById('imageTabUrl');
    const isUrlTab = urlTab && urlTab.style.display !== 'none';

    if (isUrlTab) {
        const alt = (document.getElementById('imageAltInput')?.value || '').trim();
        const url = (document.getElementById('imageUrlInput')?.value || '').trim();

        if (!url) {
            toast('Informe a URL da imagem', 'error');
            return;
        }

        const markdown = `![${alt || 'imagem'}](${url})`;
        const sel = getEditorSelection();
        editorReplace(sel.from, sel.to, markdown);
        closeModal();
        focusEditor();
    } else {
        // Upload tab
        const file = document.getElementById('imageFileInput')?.files[0];
        if (!file) {
            toast('Selecione um arquivo de imagem', 'error');
            return;
        }
        if (!editor.currentFile) {
            toast('Nenhum arquivo aberto', 'error');
            return;
        }

        const btn = document.getElementById('imageInsertBtn');
        if (btn) { btn.disabled = true; btn.textContent = 'Enviando...'; }

        const formData = new FormData();
        formData.append('image', file, file.name);
        formData.append('folder', editor.currentFile.folder);
        formData.append('note_path', editor.currentFile.path);

        try {
            const response = await fetch('/api/files/upload-image', {
                method: 'POST',
                body: formData,
            });
            const result = await response.json();

            if (result.filename) {
                const imgSyntax = `![[${result.filename}]]`;
                const sel = getEditorSelection();
                editorReplace(sel.from, sel.to, imgSyntax);
                closeModal();
                focusEditor();
                toast('Imagem salva em: ' + (result.image_folder || editor.imageFolder));
                schedulePreviewUpdate();
            } else if (result.error) {
                toast('Erro: ' + result.error, 'error');
                if (btn) { btn.disabled = false; btn.textContent = 'Inserir'; }
            }
        } catch (err) {
            toast('Erro ao enviar imagem', 'error');
            if (btn) { btn.disabled = false; btn.textContent = 'Inserir'; }
        }
    }
}

// ── Callout Modal ──
function showCalloutModal() {
    const calloutTypes = [
        { type: 'NOTE', icon: '📝', label: 'Note', desc: 'Informação geral' },
        { type: 'TIP', icon: '💡', label: 'Tip', desc: 'Dica útil' },
        { type: 'INFO', icon: 'ℹ️', label: 'Info', desc: 'Informação adicional' },
        { type: 'WARNING', icon: '⚠️', label: 'Warning', desc: 'Aviso importante' },
        { type: 'DANGER', icon: '🚨', label: 'Danger', desc: 'Perigo / erro crítico' },
        { type: 'SUCCESS', icon: '✅', label: 'Success', desc: 'Sucesso / feito' },
        { type: 'QUESTION', icon: '❓', label: 'Question', desc: 'Pergunta / FAQ' },
        { type: 'QUOTE', icon: '💬', label: 'Quote', desc: 'Citação' },
        { type: 'EXAMPLE', icon: '📋', label: 'Example', desc: 'Exemplo prático' },
        { type: 'BUG', icon: '🐛', label: 'Bug', desc: 'Bug / problema conhecido' },
        { type: 'ABSTRACT', icon: '📄', label: 'Abstract', desc: 'Resumo / sumário' },
        { type: 'TODO', icon: '📌', label: 'Todo', desc: 'Tarefa pendente' },
    ];

    showModal('Inserir Callout', `
        <div class="form-group">
            <label>Título do callout</label>
            <input type="text" id="calloutTitleInput" placeholder="Título (opcional)">
        </div>
        <div class="form-group">
            <label>Conteúdo</label>
            <textarea id="calloutContentInput" rows="3" placeholder="Conteúdo do callout..." style="width:100%;padding:8px 12px;border:1px solid var(--border);border-radius:var(--radius);background:var(--bg);color:var(--text-primary);font-size:14px;resize:vertical;"></textarea>
        </div>
        <div class="form-group">
            <label>Tipo</label>
            <div class="callout-type-grid">
                ${calloutTypes.map(c => `
                    <div class="callout-type-option${c.type === 'INFO' ? ' selected' : ''}" data-type="${c.type}" onclick="selectCalloutType(this)">
                        <span class="callout-type-icon">${c.icon}</span>
                        <span class="callout-type-label">${c.label}</span>
                    </div>
                `).join('')}
            </div>
        </div>
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" onclick="doInsertCallout()">Inserir</button>
    `);

    setTimeout(() => document.getElementById('calloutTitleInput')?.focus(), 100);
}

function selectCalloutType(el) {
    document.querySelectorAll('.callout-type-option').forEach(o => o.classList.remove('selected'));
    el.classList.add('selected');
}

function doInsertCallout() {
    const selectedType = document.querySelector('.callout-type-option.selected');
    const type = selectedType ? selectedType.dataset.type : 'INFO';
    const title = (document.getElementById('calloutTitleInput')?.value || '').trim();
    const content = (document.getElementById('calloutContentInput')?.value || '').trim();

    const headerTitle = title || type.charAt(0) + type.slice(1).toLowerCase();
    const bodyLines = (content || 'Conteúdo').split('\n').map(l => '> ' + l).join('\n');

    const markdown = `> [!${type}] ${headerTitle}\n${bodyLines}`;

    const sel = getEditorSelection();
    editorReplace(sel.from, sel.to, markdown);
    closeModal();
    focusEditor();
}

// ── Backlinks ──
async function loadBacklinks() {
    if (!editor.currentFile) return;

    try {
        const backlinks = await API.get(
            `/api/notes/backlinks?folder=${encodeURIComponent(editor.currentFile.folder)}&path=${encodeURIComponent(editor.currentFile.path)}`
        );

        const countEl = document.getElementById('backlinksCount');
        const listEl = document.getElementById('backlinksList');
        countEl.textContent = backlinks.length;

        if (backlinks.length === 0) {
            listEl.innerHTML = '<div style="padding:8px 12px;color:#80868b;font-size:13px;">Nenhuma nota aponta para este arquivo.</div>';
        } else {
            listEl.innerHTML = backlinks.map(bl => `
                <div class="backlink-item" onclick="openWikiLink('${escAttr(bl.path)}')">
                    <svg width="14" height="14" viewBox="0 0 24 24"><path d="M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z"/></svg>
                    ${escHTML(bl.name)}
                </div>
            `).join('');
        }
    } catch (e) {
        console.error('Backlinks error:', e);
    }
}

// ── Notes List (for autocomplete) ──
async function loadNotesList() {
    if (!state.currentFolder) return;
    try {
        editor.notesList = await API.get(`/api/notes/list?folder=${encodeURIComponent(state.currentFolder)}`);
    } catch (e) {
        console.error('Notes list error:', e);
    }
}

// ── Image Folder Configuration ──
async function loadImageFolderConfig() {
    if (!state.currentFolder) return;
    try {
        const data = await API.get(`/api/folders/image-folder?id=${encodeURIComponent(state.currentFolder)}`);
        if (data && !data.error) {
            editor.imageFolder = data.image_folder || 'attachments';
        }
    } catch (e) {
        console.error('Image folder config error:', e);
    }
}

function showImageFolderConfig() {
    if (!state.currentFolder) {
        toast('Nenhuma pasta selecionada', 'error');
        return;
    }

    showModal('Configurar Pasta de Imagens', `
        <div style="margin-bottom:16px;padding:12px;background:rgba(124,58,237,0.08);border-radius:8px;font-size:13px;color:#7c3aed;">
            <strong>📁 Como funciona:</strong><br>
            Defina a pasta onde as imagens coladas ou inseridas serão salvas automaticamente.
            Funciona como o Obsidian — ao colar um print, ele é salvo nesta pasta e inserido como <code>![[nome.png]]</code>.
        </div>
        <div class="form-group">
            <label>Pasta de Imagens (relativa à pasta sincronizada)</label>
            <input type="text" id="imageFolderInput" value="${escAttr(editor.imageFolder)}" placeholder="attachments">
        </div>
        <div style="margin-top:8px;font-size:12px;color:#80868b;">
            <strong>Exemplos:</strong> attachments, images, assets, imgs, _resources<br>
            <strong>Pasta atual:</strong> ${escHTML(editor.imageFolder)}
        </div>
        <div id="imageFolderBrowse" style="margin-top:12px;">
            <label style="font-weight:500;font-size:13px;">Ou selecione uma subpasta existente:</label>
            <div id="imageFolderSubfolders" style="margin-top:8px;max-height:200px;overflow-y:auto;border:1px solid #e0e0e0;border-radius:8px;padding:4px;"></div>
        </div>
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" onclick="saveImageFolderConfig()">Salvar</button>
    `);

    // Load subfolders to suggest
    loadImageFolderSubfolders();
    setTimeout(() => document.getElementById('imageFolderInput').focus(), 100);
}

async function loadImageFolderSubfolders() {
    const container = document.getElementById('imageFolderSubfolders');
    if (!container) return;
    container.innerHTML = '<div style="padding:8px;color:#80868b;font-size:12px;">Carregando subpastas...</div>';

    try {
        const data = await API.get(`/api/folders/files?id=${encodeURIComponent(state.currentFolder)}`);
        const dirs = (data.items || []).filter(f => f.is_dir);

        if (dirs.length === 0) {
            container.innerHTML = '<div style="padding:8px;color:#80868b;font-size:12px;">Nenhuma subpasta encontrada. A pasta será criada automaticamente.</div>';
            return;
        }

        container.innerHTML = dirs.map(d => `
            <div style="padding:6px 10px;cursor:pointer;border-radius:6px;display:flex;align-items:center;gap:8px;font-size:13px;"
                 class="subfolder-option"
                 onmouseover="this.style.background='#f1f3f4'" onmouseout="this.style.background='transparent'"
                 onclick="document.getElementById('imageFolderInput').value='${escAttr(d.name)}'">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="#f9ab00"><path d="M20 6h-8l-2-2H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2zm0 12H4V8h16v10z"/></svg>
                <span>${escHTML(d.name)}</span>
                ${d.name === editor.imageFolder ? '<span style="color:#1a73e8;font-size:11px;margin-left:auto;">✓ atual</span>' : ''}
            </div>
        `).join('');
    } catch (e) {
        container.innerHTML = '<div style="padding:8px;color:#d93025;font-size:12px;">Erro ao carregar subpastas.</div>';
    }
}

async function saveImageFolderConfig() {
    const input = document.getElementById('imageFolderInput');
    const value = (input?.value || '').trim();

    if (!value) {
        toast('Informe o nome da pasta de imagens', 'error');
        return;
    }

    // Basic validation: no absolute paths, no traversal
    if (value.startsWith('/') || value.startsWith('\\') || value.includes('..')) {
        toast('Use um caminho relativo sem ".."', 'error');
        return;
    }

    try {
        const result = await API.put('/api/folders/image-folder', {
            folder_id: state.currentFolder,
            image_folder: value,
        });

        if (result.error) {
            toast('Erro: ' + result.error, 'error');
            return;
        }

        editor.imageFolder = result.image_folder || value;
        closeModal();
        toast('Pasta de imagens configurada: ' + editor.imageFolder);
    } catch (e) {
        toast('Erro ao salvar configuração', 'error');
        console.error('Save image folder error:', e);
    }
}

// ── Graph View ──
async function showGraphView() {
    if (!state.currentFolder) {
        toast('Selecione uma pasta primeiro');
        return;
    }

    const overlay = document.getElementById('graphModalOverlay');
    overlay.classList.remove('hidden');

    try {
        const graphData = await API.get(`/api/notes/graph?folder=${encodeURIComponent(state.currentFolder)}`);
        renderGraph(graphData);
    } catch (e) {
        console.error('Graph error:', e);
        toast('Erro ao carregar grafo', 'error');
    }
}

function renderGraph(data) {
    const svg = d3.select('#graphSvg');
    svg.selectAll('*').remove();

    const container = document.querySelector('.graph-modal-body');
    const width = container.clientWidth;
    const height = container.clientHeight;

    if (!data.nodes || data.nodes.length === 0) {
        svg.append('text')
            .attr('x', width / 2)
            .attr('y', height / 2)
            .attr('text-anchor', 'middle')
            .attr('fill', '#94a3b8')
            .attr('font-size', '16px')
            .text('Nenhuma nota com links encontrada. Use [[wikilinks]] para conectar notas.');
        return;
    }

    const simulation = d3.forceSimulation(data.nodes)
        .force('link', d3.forceLink(data.links).id(d => d.id).distance(100))
        .force('charge', d3.forceManyBody().strength(-200))
        .force('center', d3.forceCenter(width / 2, height / 2))
        .force('collision', d3.forceCollide().radius(30));

    const g = svg.append('g');

    // Zoom
    svg.call(d3.zoom()
        .scaleExtent([0.1, 4])
        .on('zoom', (event) => g.attr('transform', event.transform))
    );

    // Links
    const link = g.append('g')
        .selectAll('line')
        .data(data.links)
        .join('line')
        .attr('class', 'graph-link')
        .attr('stroke-width', 1.5);

    // Nodes
    const node = g.append('g')
        .selectAll('g')
        .data(data.nodes)
        .join('g')
        .attr('class', 'graph-node')
        .call(d3.drag()
            .on('start', (event, d) => {
                if (!event.active) simulation.alphaTarget(0.3).restart();
                d.fx = d.x;
                d.fy = d.y;
            })
            .on('drag', (event, d) => {
                d.fx = event.x;
                d.fy = event.y;
            })
            .on('end', (event, d) => {
                if (!event.active) simulation.alphaTarget(0);
                d.fx = null;
                d.fy = null;
            })
        );

    node.append('circle')
        .attr('r', d => d.has_file ? 6 : 4)
        .attr('fill', d => {
            if (editor.currentFile && d.path === editor.currentFile.path) return '#7c3aed';
            return d.has_file ? '#3b82f6' : '#64748b';
        })
        .attr('stroke', '#1e293b')
        .attr('stroke-width', 2);

    node.append('text')
        .attr('dx', 12)
        .attr('dy', 4)
        .text(d => d.name);

    // Click to open note
    node.on('click', (event, d) => {
        if (d.has_file && d.path) {
            document.getElementById('graphModalOverlay').classList.add('hidden');
            openWikiLink(d.path);
        }
    });

    // Highlight current note
    node.selectAll('circle')
        .filter(d => editor.currentFile && d.path === editor.currentFile.path)
        .attr('r', 9)
        .attr('fill', '#7c3aed')
        .attr('stroke', '#a78bfa')
        .attr('stroke-width', 3);

    simulation.on('tick', () => {
        link
            .attr('x1', d => d.source.x)
            .attr('y1', d => d.source.y)
            .attr('x2', d => d.target.x)
            .attr('y2', d => d.target.y);

        node.attr('transform', d => `translate(${d.x},${d.y})`);
    });
}

// ── View Toggle ──
function setEditorViewMode(mode) {
    const el = document.getElementById('viewEditor');
    el.classList.remove('source-mode', 'preview-mode');
    if (mode === 'source') el.classList.add('source-mode');
    if (mode === 'preview') el.classList.add('preview-mode');

    document.querySelectorAll('.toggle-btn').forEach(btn => btn.classList.remove('active'));
    document.getElementById(mode === 'source' ? 'toggleSource' : mode === 'preview' ? 'togglePreview' : 'toggleSplit').classList.add('active');

    // If switching to preview, update it
    if (mode === 'preview') {
        const content = getEditorContent();
        if (content !== undefined) renderPreview(content);
    }
}

// ── Close Editor and return to files ──
function closeEditor() {
    if (editor.modified) {
        if (!confirm('Você tem alterações não salvas. Deseja sair?')) return;
    }

    editor.currentFile = null;
    editor.modified = false;

    if (editor.view) {
        editor.view.destroy();
        editor.view = null;
    }
    if (editor._fallbackTA) {
        editor._fallbackTA.remove();
        editor._fallbackTA = null;
    }

    switchView('files');
}

// ═══════════════════════════════════════════════
// CONFIG PANE — PDF Builder
// ═══════════════════════════════════════════════

// ── Toggle visibility of the config pane ──
function toggleConfigPane() {
    const viewEditor = document.getElementById('viewEditor');
    const isOpen = viewEditor.classList.toggle('config-open');
    document.getElementById('toggleConfig').classList.toggle('active', isOpen);
    if (isOpen) {
        applyConfigToPreview();
    } else {
        // Remove the live-preview styling so the editor theme is restored
        const previewPane = document.getElementById('previewPane');
        if (previewPane) {
            previewPane.classList.remove('preview-configured');
            const cfgVars = [
                '--cfg-font', '--cfg-font-size', '--cfg-line-height',
                '--cfg-text-color', '--cfg-para-spacing', '--cfg-code-size',
                '--cfg-code-bg', '--cfg-page-bg'
            ];
            cfgVars.forEach(v => previewPane.style.removeProperty(v));
        }
    }
}

// ── Switch tab inside the config pane ──
function switchConfigTab(tab) {
    document.querySelectorAll('.config-tab').forEach(t => t.classList.toggle('active', t.dataset.tab === tab));
    document.querySelectorAll('.config-tab-content').forEach(c => c.classList.add('hidden'));
    const tabMap = { text: 'configTabText', page: 'configTabPage', header: 'configTabHeader', brand: 'configTabBrand', css: 'configTabCss' };
    const el = document.getElementById(tabMap[tab]);
    if (el) el.classList.remove('hidden');
}

// ── Sync slider ↔ number input ──
function syncSlider(targetId, sourceId) {
    const src = document.getElementById(sourceId);
    const tgt = document.getElementById(targetId);
    if (src && tgt) tgt.value = src.value;
}

// ── Read all form values → pdfConfig, then apply to preview ──
function updatePdfConfig() {
    const g = (id) => document.getElementById(id);

    // Text
    const fontSel = g('cfgFont');
    if (fontSel) pdfConfig.font = fontSel.value;
    pdfConfig.fontSize     = parseFloat(g('cfgFontSize')?.value   || 15);
    pdfConfig.lineHeight   = parseFloat(g('cfgLineHeight')?.value  || 1.6);
    pdfConfig.textColor    = g('cfgTextColor')?.value || '#1a1a1a';
    pdfConfig.paragraphSpacing = parseInt(g('cfgParaSpacing')?.value || 4, 10);
    pdfConfig.headingStyle = g('cfgHeadingStyle')?.value || 'comfortable';
    // Code
    pdfConfig.codeSize = parseFloat(g('cfgCodeSize')?.value || 10);
    pdfConfig.codeBg   = g('cfgCodeBg')?.value || '#f5f5f5';
    // Page
    pdfConfig.pageFormat  = g('cfgPageFormat')?.value   || 'a4';
    pdfConfig.orientation = g('cfgOrientation')?.value  || 'portrait';
    pdfConfig.marginTop    = parseInt(g('cfgMarginTop')?.value    || 15, 10);
    pdfConfig.marginBottom = parseInt(g('cfgMarginBottom')?.value || 15, 10);
    pdfConfig.marginLeft   = parseInt(g('cfgMarginLeft')?.value   || 15, 10);
    pdfConfig.marginRight  = parseInt(g('cfgMarginRight')?.value  || 15, 10);
    pdfConfig.pageBg = g('cfgPageBg')?.value || '#ffffff';
    // Header / Footer
    pdfConfig.headerEnabled = g('cfgHeaderEnabled')?.checked || false;
    pdfConfig.headerText  = g('cfgHeaderText')?.value   || '';
    pdfConfig.headerAlign = g('cfgHeaderAlign')?.value  || 'center';
    pdfConfig.footerEnabled = g('cfgFooterEnabled')?.checked || false;
    pdfConfig.footerText  = g('cfgFooterText')?.value   || '';
    pdfConfig.pageNumbers = g('cfgPageNumbers')?.checked || false;
    // Brand
    pdfConfig.watermarkEnabled = g('cfgWatermarkEnabled')?.checked || false;
    pdfConfig.watermarkText    = g('cfgWatermarkText')?.value     || '';
    pdfConfig.watermarkOpacity = parseFloat(g('cfgWatermarkOpacity')?.value || 0.15);
    pdfConfig.watermarkColor   = g('cfgWatermarkColor')?.value    || '#888888';
    // CSS
    pdfConfig.customCss = g('cfgCustomCss')?.value || '';

    applyConfigToPreview();
}

// ── Apply pdfConfig → live preview via CSS custom properties ──
function applyConfigToPreview() {
    const previewPane = document.getElementById('previewPane');
    const previewContent = document.getElementById('previewContent');
    if (!previewPane || !previewContent) return;

    previewPane.classList.add('preview-configured');
    previewPane.dataset.headingStyle = pdfConfig.headingStyle;

    // Inject CSS vars
    const ptToPx = (pt) => (pt * 96 / 72).toFixed(2) + 'px';
    previewPane.style.setProperty('--cfg-font', pdfConfig.font);
    previewPane.style.setProperty('--cfg-font-size', ptToPx(pdfConfig.fontSize));
    previewPane.style.setProperty('--cfg-line-height', pdfConfig.lineHeight);
    previewPane.style.setProperty('--cfg-text-color', pdfConfig.textColor);
    previewPane.style.setProperty('--cfg-para-spacing', pdfConfig.paragraphSpacing + 'mm');
    previewPane.style.setProperty('--cfg-code-size', ptToPx(pdfConfig.codeSize));
    previewPane.style.setProperty('--cfg-code-bg', pdfConfig.codeBg);
    previewPane.style.setProperty('--cfg-page-bg', pdfConfig.pageBg);

    // Load web fonts dynamically if needed
    loadPreviewFont(pdfConfig.font);

    // Watermark
    let wm = previewPane.querySelector('.preview-watermark');
    if (pdfConfig.watermarkEnabled && pdfConfig.watermarkText) {
        if (!wm) {
            wm = document.createElement('div');
            wm.className = 'preview-watermark';
            previewPane.appendChild(wm);
        }
        wm.textContent = pdfConfig.watermarkText;
        wm.style.color = pdfConfig.watermarkColor;
        wm.style.opacity = pdfConfig.watermarkOpacity;
    } else if (wm) {
        wm.remove();
    }

    // Custom CSS injection into preview
    let styleTag = document.getElementById('cfgCustomStyle');
    if (!styleTag) {
        styleTag = document.createElement('style');
        styleTag.id = 'cfgCustomStyle';
        document.head.appendChild(styleTag);
    }
    styleTag.textContent = pdfConfig.customCss || '';
}

// ── Dynamically load a Google Font for preview ──
function loadPreviewFont(fontFamily) {
    // Extract the first font name from the family string
    const name = fontFamily.split(',')[0].replace(/['"]/g, '').trim();
    const systemFonts = ['Arial', 'Helvetica', 'Georgia', 'Times New Roman', 'Inter'];
    if (systemFonts.includes(name)) return;

    const id = 'gfont-' + name.replace(/\s+/g, '-').toLowerCase();
    if (document.getElementById(id)) return;

    const link = document.createElement('link');
    link.id = id;
    link.rel = 'stylesheet';
    const encoded = encodeURIComponent(name);
    link.href = `https://fonts.googleapis.com/css2?family=${encoded}:wght@400;600;700&display=swap`;
    document.head.appendChild(link);
}

// ── Apply a quick preset ──
function applyPdfPreset(preset) {
    // Highlight the active preset button
    document.querySelectorAll('.config-preset-btn').forEach(b => b.classList.remove('active'));
    const btn = document.querySelector(`.config-preset-btn[onclick="applyPdfPreset('${preset}')"]`);
    if (btn) btn.classList.add('active');

    const configs = {
        relatorio: {
            font: "Inter, sans-serif", fontSize: 11, lineHeight: 1.5,
            textColor: '#1a1a1a', paragraphSpacing: 3, headingStyle: 'compact',
            codeSize: 9, codeBg: '#f5f5f5',
            marginTop: 20, marginBottom: 20, marginLeft: 20, marginRight: 20,
            pageBg: '#ffffff',
        },
        memo: {
            font: "Roboto, sans-serif", fontSize: 10, lineHeight: 1.4,
            textColor: '#1a1a1a', paragraphSpacing: 2, headingStyle: 'compact',
            codeSize: 8, codeBg: '#f5f5f5',
            marginTop: 12, marginBottom: 12, marginLeft: 12, marginRight: 12,
            pageBg: '#ffffff',
        },
        margem: {
            font: "Merriweather, serif", fontSize: 13, lineHeight: 1.8,
            textColor: '#1a1a1a', paragraphSpacing: 6, headingStyle: 'spacious',
            codeSize: 10, codeBg: '#f8f8f8',
            marginTop: 25, marginBottom: 25, marginLeft: 35, marginRight: 35,
            pageBg: '#ffffff',
        },
    };

    const cfg = configs[preset];
    if (!cfg) return;

    // Apply to pdfConfig
    Object.assign(pdfConfig, cfg);

    // Update form controls
    _setFormFromConfig();
    applyConfigToPreview();
    toast('Preset "' + btn.textContent + '" aplicado!');
}

// ── Sync form controls ← pdfConfig ──
function _setFormFromConfig() {
    const g = (id) => document.getElementById(id);
    const setVal = (id, val) => { const el = g(id); if (el) el.value = val; };
    const setChk = (id, val) => { const el = g(id); if (el) el.checked = val; };

    // Font: find matching option
    const fontSel = g('cfgFont');
    if (fontSel) {
        let found = false;
        for (const opt of fontSel.options) {
            if (opt.value === pdfConfig.font) { fontSel.value = opt.value; found = true; break; }
        }
        if (!found) fontSel.options[0].selected = true;
    }

    setVal('cfgFontSize',   pdfConfig.fontSize);
    setVal('cfgLineHeight', pdfConfig.lineHeight);
    setVal('cfgLineHeightSlider', pdfConfig.lineHeight);
    setVal('cfgTextColor',  pdfConfig.textColor);
    setVal('cfgParaSpacing', pdfConfig.paragraphSpacing);
    setVal('cfgHeadingStyle', pdfConfig.headingStyle);
    setVal('cfgCodeSize',   pdfConfig.codeSize);
    setVal('cfgCodeBg',     pdfConfig.codeBg);
    setVal('cfgPageFormat', pdfConfig.pageFormat);
    setVal('cfgOrientation', pdfConfig.orientation);
    setVal('cfgMarginTop',   pdfConfig.marginTop);
    setVal('cfgMarginBottom', pdfConfig.marginBottom);
    setVal('cfgMarginLeft',  pdfConfig.marginLeft);
    setVal('cfgMarginRight', pdfConfig.marginRight);
    setVal('cfgPageBg',     pdfConfig.pageBg);
    setChk('cfgHeaderEnabled', pdfConfig.headerEnabled);
    setVal('cfgHeaderText', pdfConfig.headerText);
    setVal('cfgHeaderAlign', pdfConfig.headerAlign);
    setChk('cfgFooterEnabled', pdfConfig.footerEnabled);
    setVal('cfgFooterText', pdfConfig.footerText);
    setChk('cfgPageNumbers', pdfConfig.pageNumbers);
    setChk('cfgWatermarkEnabled', pdfConfig.watermarkEnabled);
    setVal('cfgWatermarkText', pdfConfig.watermarkText);
    setVal('cfgWatermarkOpacity', pdfConfig.watermarkOpacity);
    setVal('cfgWatermarkOpacitySlider', pdfConfig.watermarkOpacity);
    setVal('cfgWatermarkColor', pdfConfig.watermarkColor);
    setVal('cfgCustomCss', pdfConfig.customCss);
}

// ── Init Editor Events ──
function initEditorEvents() {
    // Back button
    document.getElementById('editorBackBtn').addEventListener('click', closeEditor);

    // Toolbar buttons
    document.getElementById('editorToolbar').addEventListener('click', (e) => {
        const btn = e.target.closest('.toolbar-btn');
        if (btn && btn.dataset.action) {
            toolbarAction(btn.dataset.action);
        }
    });

    // View toggles
    document.getElementById('toggleSplit').addEventListener('click', () => setEditorViewMode('split'));
    document.getElementById('toggleSource').addEventListener('click', () => setEditorViewMode('source'));
    document.getElementById('togglePreview').addEventListener('click', () => setEditorViewMode('preview'));
    document.getElementById('toggleConfig').addEventListener('click', () => toggleConfigPane());

    // Backlinks toggle
    document.getElementById('backlinksToggle').addEventListener('click', () => {
        document.getElementById('backlinksPanel').classList.toggle('open');
    });

    // Graph modal close
    document.getElementById('graphModalClose').addEventListener('click', () => {
        document.getElementById('graphModalOverlay').classList.add('hidden');
    });
    document.getElementById('graphModalOverlay').addEventListener('click', (e) => {
        if (e.target.id === 'graphModalOverlay') {
            e.target.classList.add('hidden');
        }
    });

    // Global Ctrl+S
    document.addEventListener('keydown', (e) => {
        if ((e.ctrlKey || e.metaKey) && e.key === 's' && state.currentView === 'editor') {
            e.preventDefault();
            saveCurrentFile();
        }
    });
}

// Patch: override switchView to support 'editor' view
const _originalSwitchView = switchView;
switchView = function (view) {
    state.currentView = view;
    document.querySelectorAll('.nav-item').forEach(n => n.classList.toggle('active', n.dataset.view === view));
    document.querySelectorAll('.view').forEach(v => {
        // Handle editor view
        if (v.id === 'viewEditor') {
            v.classList.toggle('active', view === 'editor');
        } else {
            v.classList.toggle('active', v.id === 'view' + capitalize(view));
        }
    });

    if (view === 'devices') loadPeers();
    if (view === 'tailscale') loadTailscaleStatus();
    if (view === 'logs') loadLogs();
    if (view === 'files') loadFolders();
};

// Patch: modify renderFileList to detect .md files and make them clickable, and all other files previewable
// Supports both 'list' and 'grid' view modes via state.viewMode
const _originalRenderFileList = renderFileList;
renderFileList = function () {
    const list = document.getElementById('fileList');
    list.innerHTML = '';

    const isGrid = state.viewMode === 'grid';

    // Toggle CSS classes on the list container
    list.classList.toggle('file-list-grid', isGrid);
    list.classList.remove(isGrid ? 'file-list-rows' : 'file-list-grid');
    if (!isGrid) list.classList.add('file-list-rows');

    // Hide/show column header
    const listHeader = document.querySelector('.file-list-header');
    if (listHeader) listHeader.style.display = isGrid ? 'none' : '';

    if (state.files.length === 0) {
        list.innerHTML = '<div class="empty-state"><p>Pasta vazia</p></div>';
        return;
    }

    const sorted = [...state.files].sort((a, b) => {
        if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
        return a.name.localeCompare(b.name);
    });

    sorted.forEach(file => {
        const isMarkdown = !file.is_dir && file.name.toLowerCase().endsWith('.md');
        const mimeType = isMarkdown ? 'markdown' : file.mime_type;
        const icon = isMarkdown ? getMarkdownIcon() : getFileIcon(mimeType);

        const el = document.createElement('div');

        if (isGrid) {
            /* ── Grid card ── */
            el.className = 'file-grid-card';
            const isImage = mimeType === 'image';
            let thumbHTML = '';
            if (isImage && !file.is_dir) {
                const src = `/api/folders/files/download?id=${encodeURIComponent(state.currentFolder)}&path=${encodeURIComponent(file.path)}`;
                thumbHTML = `<div class="file-grid-thumb"><img src="${escAttr(src)}" alt="" loading="lazy"></div>`;
            } else {
                thumbHTML = `<div class="file-grid-thumb file-grid-thumb-icon">${icon}</div>`;
            }
            el.innerHTML = `
                ${thumbHTML}
                <div class="file-grid-info">
                    <span class="file-grid-name" title="${escAttr(file.name)}">${escHTML(file.name)}</span>
                    ${isMarkdown ? '<span class="file-grid-badge">MD</span>' : ''}
                    <span class="file-grid-meta">${file.is_dir ? 'Pasta' : formatSize(file.size)}</span>
                </div>
                <button class="btn-context-menu btn-context-grid" title="Opções" data-path="${escAttr(file.path)}" data-name="${escAttr(file.name)}" data-isdir="${file.is_dir}">
                    <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="5" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="12" cy="19" r="2"/></svg>
                </button>
            `;
            el.querySelector('.btn-context-menu').addEventListener('click', (e) => {
                e.stopPropagation();
                showFileContextMenu(e.currentTarget, file);
            });
        } else {
            /* ── List row (original) ── */
            el.className = 'file-row';
            el.innerHTML = `
                <div class="file-name">
                    <div class="file-icon ${mimeType}">${icon}</div>
                    <span>${escHTML(file.name)}</span>
                    ${isMarkdown ? '<span style="margin-left:6px;font-size:11px;background:rgba(124,58,237,0.1);color:#7c3aed;padding:1px 6px;border-radius:4px;">MD</span>' : ''}
                </div>
                <div class="file-status">${renderSyncStatus(file)}</div>
                <div class="file-modified">${file.mod_time || '—'}</div>
                <div class="file-size">${file.is_dir ? '—' : formatSize(file.size)}</div>
                <div class="file-actions">
                    <button class="btn-context-menu" title="Opções" data-path="${escAttr(file.path)}" data-name="${escAttr(file.name)}" data-isdir="${file.is_dir}">
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="5" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="12" cy="19" r="2"/></svg>
                    </button>
                </div>
            `;
            el.querySelector('.btn-context-menu').addEventListener('click', (e) => {
                e.stopPropagation();
                showFileContextMenu(e.currentTarget, file);
            });
        }

        /* ── Click handlers (same for both modes) ── */
        if (file.is_dir) {
            el.addEventListener('click', () => navigateToSubfolder(file));
        } else if (isMarkdown) {
            el.style.cursor = 'pointer';
            el.classList.add('clickable');
            el.addEventListener('click', () => openMarkdownEditor(file));
        } else {
            el.style.cursor = 'pointer';
            el.classList.add('clickable');
            el.addEventListener('click', () => openFilePreview(file));
        }

        list.appendChild(el);
    });
};

function getMarkdownIcon() {
    return '<svg width="24" height="24" viewBox="0 0 24 24" fill="#7c3aed"><path d="M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z"/></svg>';
}

// ── Download de arquivo ou pasta (ZIP) ──
function downloadFileOrFolder(path, name, isDir) {
    let url;
    if (isDir) {
        // Baixar pasta como ZIP
        url = `/api/files/download-zip?folder=${encodeURIComponent(state.currentFolder)}&path=${encodeURIComponent(path)}`;
    } else {
        // Baixar arquivo direto
        url = `/api/folders/files/download?id=${encodeURIComponent(state.currentFolder)}&path=${encodeURIComponent(path)}`;
    }

    // Usar <a download> para compatibilidade com Android
    const a = document.createElement('a');
    a.href = url;
    a.download = isDir ? name + '.zip' : name;
    a.target = '_blank';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
}

async function deleteFileOrFolder(path, name, isDir) {
    const tipo = isDir ? 'pasta' : 'arquivo';
    const confirmado = confirm(`Tem certeza que deseja excluir ${tipo == 'pasta' ? 'a pasta' : 'o arquivo'} "${name}"?${isDir ? '\n\nTodo o conteúdo será removido permanentemente.' : ''}`);
    if (!confirmado) return;

    const url = `/api/files/delete?id=${encodeURIComponent(state.currentFolder)}&path=${encodeURIComponent(path)}`;
    const result = await API.del(url);
    if (result && result.error) {
        alert('Erro ao excluir: ' + result.error);
        return;
    }
    // Recarrega a lista
    loadFiles(state.currentFolder, state.currentPath);
}

// ── Context Menu ("...") ──
function showFileContextMenu(btnEl, file) {
    // Remove any existing context menu
    closeFileContextMenu();

    const menu = document.createElement('div');
    menu.className = 'file-context-menu';
    menu.id = 'fileContextMenu';

    const isDir = file.is_dir;
    const tipo = isDir ? 'pasta' : 'arquivo';

    menu.innerHTML = `
        <button class="ctx-menu-item" data-action="download">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z"/></svg>
            <span>Baixar ${escHTML(tipo)}</span>
        </button>
        <button class="ctx-menu-item" data-action="rename">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04c.39-.39.39-1.02 0-1.41l-2.34-2.34a.9959.9959 0 00-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z"/></svg>
            <span>Renomear</span>
        </button>
        <div class="ctx-menu-divider"></div>
        <button class="ctx-menu-item ctx-menu-danger" data-action="delete">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>
            <span>Excluir</span>
        </button>
    `;

    // Position menu relative to the button
    document.body.appendChild(menu);

    const rect = btnEl.getBoundingClientRect();
    let top = rect.bottom + 4;
    let left = rect.right - menu.offsetWidth;

    // Ensure menu doesn't overflow viewport
    if (left < 8) left = 8;
    if (top + menu.offsetHeight > window.innerHeight - 8) {
        top = rect.top - menu.offsetHeight - 4;
    }

    menu.style.top = top + 'px';
    menu.style.left = left + 'px';

    // Handlers
    menu.querySelector('[data-action="download"]').addEventListener('click', (e) => {
        e.stopPropagation();
        closeFileContextMenu();
        downloadFileOrFolder(file.path, file.name, file.is_dir);
    });

    menu.querySelector('[data-action="rename"]').addEventListener('click', (e) => {
        e.stopPropagation();
        closeFileContextMenu();
        showRenameModal(file);
    });

    menu.querySelector('[data-action="delete"]').addEventListener('click', (e) => {
        e.stopPropagation();
        closeFileContextMenu();
        deleteFileOrFolder(file.path, file.name, file.is_dir);
    });

    // Close on click outside (after a tiny delay to avoid instant close)
    setTimeout(() => {
        document.addEventListener('click', _closeCtxMenuHandler);
        document.addEventListener('contextmenu', _closeCtxMenuHandler);
    }, 10);
}

function _closeCtxMenuHandler(e) {
    const menu = document.getElementById('fileContextMenu');
    if (menu && !menu.contains(e.target)) {
        closeFileContextMenu();
    }
}

function closeFileContextMenu() {
    const existing = document.getElementById('fileContextMenu');
    if (existing) existing.remove();
    document.removeEventListener('click', _closeCtxMenuHandler);
    document.removeEventListener('contextmenu', _closeCtxMenuHandler);
}

// ── Rename Modal ──
function showRenameModal(file) {
    const isDir = file.is_dir;
    const tipo = isDir ? 'pasta' : 'arquivo';
    const currentName = file.name;

    // For files with extensions, split name and extension
    let baseName = currentName;
    let ext = '';
    if (!isDir) {
        const dotIdx = currentName.lastIndexOf('.');
        if (dotIdx > 0) {
            baseName = currentName.substring(0, dotIdx);
            ext = currentName.substring(dotIdx);
        }
    }

    showModal(`Renomear ${tipo}`, `
        <div class="form-group">
            <label>Nome atual</label>
            <input type="text" value="${escAttr(currentName)}" disabled style="opacity:0.6;">
        </div>
        <div class="form-group">
            <label>Novo nome</label>
            <div style="display:flex;gap:4px;align-items:center;">
                <input type="text" id="renameInput" value="${escAttr(baseName)}" placeholder="Digite o novo nome" autofocus style="flex:1;">
                ${ext ? `<span style="color:var(--text-muted);font-size:13px;white-space:nowrap;">${escHTML(ext)}</span>` : ''}
            </div>
        </div>
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" id="renameConfirmBtn">Renomear</button>
    `);

    // Focus and select the input text
    const input = document.getElementById('renameInput');
    setTimeout(() => {
        input.focus();
        input.select();
    }, 100);

    // Enter key submits
    input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            doRename(file, ext);
        }
    });

    document.getElementById('renameConfirmBtn').addEventListener('click', () => {
        doRename(file, ext);
    });
}

async function doRename(file, ext) {
    const input = document.getElementById('renameInput');
    if (!input) return;

    const newBase = input.value.trim();
    if (!newBase) {
        toast('O nome não pode ficar vazio', 'error');
        return;
    }

    const newName = newBase + ext;

    if (newName === file.name) {
        closeModal();
        return;
    }

    // Validate: no path separators
    if (/[/\\:]/.test(newBase)) {
        toast('O nome não pode conter / \\ ou :', 'error');
        return;
    }

    try {
        const result = await API.post('/api/files/rename', {
            folder_id: state.currentFolder,
            old_path: file.path,
            new_name: newName,
        });

        if (result && result.error) {
            toast('Erro ao renomear: ' + result.error, 'error');
            return;
        }

        toast(`Renomeado para "${newName}"`);
        closeModal();
        loadFiles(state.currentFolder, state.currentPath);
    } catch (e) {
        toast('Erro ao renomear: ' + (e.message || e), 'error');
    }
}

// Initialize editor events when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    initEditorEvents();
});

