// ═══════════ MODALS ═══════════
function initModals() {
    document.getElementById('modalOverlay').addEventListener('click', (e) => {
        if (e.target.id === 'modalOverlay') closeModal();
    });
    document.getElementById('modalClose').addEventListener('click', closeModal);
    document.addEventListener('keydown', (e) => {
        if (e.key === 'Escape') closeModal();
    });
}

function showModal(title, bodyHTML, footerHTML) {
    document.getElementById('modalTitle').textContent = title;
    document.getElementById('modalBody').innerHTML = bodyHTML;
    document.getElementById('modalFooter').innerHTML = footerHTML || '';
    document.getElementById('modalOverlay').classList.remove('hidden');
}

function closeModal() {
    document.getElementById('modalOverlay').classList.add('hidden');
    // Reset modal width override
    const modal = document.getElementById('modalContent');
    modal.style.maxWidth = '';
    modal.style.width = '';
}

function showMarkdownGuide() {
    // Widen the modal for readability
    const modal = document.getElementById('modalContent');
    modal.style.maxWidth = '680px';
    modal.style.width = '92vw';

    showModal('📖 Guia de Markdown — Stashbird', `
        <div class="md-guide">

            <p class="md-guide-intro">Referência completa para escrever e formatar documentos no Stashbird.</p>

            <div class="md-guide-section">
                <h4>Fundamentos do Markdown</h4>
                <p>Escreva seu conteúdo usando a sintaxe Markdown padrão. Todos os elementos comuns são suportados nativamente.</p>
                <ul class="md-guide-list">
                    <li><span class="md-guide-badge">Títulos</span> use <code>#</code> até <code>######</code></li>
                    <li><span class="md-guide-badge">Listas</span> use <code>-</code> ou <code>1.</code> para listas ordenadas</li>
                    <li><span class="md-guide-badge">Negrito</span> envolva o texto com <code>**dois asteriscos**</code></li>
                    <li><span class="md-guide-badge">Itálico</span> envolva o texto com <code>*um asterisco*</code></li>
                    <li><span class="md-guide-badge">Código inline</span> envolva com <code>\`backticks\`</code></li>
                    <li><span class="md-guide-badge">Bloco de código</span> use três backticks com linguagem opcional</li>
                    <li><span class="md-guide-badge">Tabelas</span> use <code>| pipes |</code> para separar colunas</li>
                    <li><span class="md-guide-badge">Links</span> <code>[texto do link](https://url)</code></li>
                </ul>
            </div>

            <div class="md-guide-section">
                <h4>Placeholders Dinâmicos</h4>
                <p>Use estes placeholders em cabeçalhos, rodapés e no corpo do documento — eles são substituídos automaticamente na renderização:</p>
                <div class="md-guide-placeholders">
                    <div class="md-guide-placeholder-item">
                        <code>{title}</code>
                        <span>Substituído pelo primeiro H1 do seu documento</span>
                    </div>
                    <div class="md-guide-placeholder-item">
                        <code>{date}</code>
                        <span>Substituído pela data atual</span>
                    </div>
                    <div class="md-guide-placeholder-item">
                        <code>{page}</code>
                        <span>Substituído pelo número da página atual</span>
                    </div>
                    <div class="md-guide-placeholder-item">
                        <code>{pages}</code>
                        <span>Substituído pelo total de páginas</span>
                    </div>
                </div>
            </div>

            <div class="md-guide-section">
                <h4>Quebras de Página</h4>
                <p>Insira uma quebra de página em qualquer lugar do documento adicionando o seguinte comentário HTML em uma linha separada:</p>
                <div class="md-guide-codeblock">&lt;!-- pagebreak --&gt;</div>
                <p class="md-guide-note">Cada seção separada por este comentário começará em uma nova página ao exportar.</p>
            </div>

        </div>
    `, `<button class="btn-primary" onclick="closeModal()">Fechar</button>`);
}

function showAddFolderModal() {
    showModal('Adicionar Pasta', `
        <div class="form-group">
            <label>Caminho</label>
            <div class="folder-picker-input">
                <input type="text" id="newFolderPath" placeholder="Selecione ou digite o caminho..." readonly>
                <button class="btn-outline btn-browse" onclick="openFolderPicker()">📂 Buscar</button>
            </div>
        </div>
        <div id="folderPickerPanel" class="folder-picker-panel hidden">
            <div class="folder-picker-header">
                <button class="btn-text btn-picker-up" id="pickerUpBtn" onclick="pickerGoUp()" disabled>⬆ Voltar</button>
                <span class="picker-current-path" id="pickerCurrentPath">Selecione um local</span>
            </div>
            <div class="folder-picker-list" id="pickerList">
                <div class="picker-loading">Carregando...</div>
            </div>
            <div class="folder-picker-footer">
                <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap;">
                    <button class="btn-primary btn-sm" onclick="pickerSelect()">✓ Selecionar esta pasta</button>
                    <button class="btn-outline btn-sm" onclick="showCreateFolderInPicker()">📁 Criar nova pasta aqui</button>
                </div>
            </div>
        </div>
        <div id="createFolderInline" class="create-folder-inline hidden">
            <div class="form-group" style="margin-bottom:8px;">
                <label>Nome da nova pasta</label>
                <div style="display:flex;gap:8px;">
                    <input type="text" id="pickerNewFolderName" placeholder="Nome da pasta..." style="flex:1;">
                    <button class="btn-primary btn-sm" onclick="doCreateFolderInPicker()">Criar</button>
                    <button class="btn-outline btn-sm" onclick="hideCreateFolderInPicker()">Cancelar</button>
                </div>
            </div>
        </div>
        <div class="form-group">
            <label><input type="checkbox" id="newFolderSyncDel" checked> Sincronizar exclusões</label>
        </div>
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" onclick="doAddFolder()">Adicionar</button>
    `);
}

function showCreateFolderInPicker() {
    document.getElementById('createFolderInline').classList.remove('hidden');
    document.getElementById('pickerNewFolderName').value = '';
    setTimeout(() => document.getElementById('pickerNewFolderName').focus(), 50);
}

function hideCreateFolderInPicker() {
    document.getElementById('createFolderInline').classList.add('hidden');
}

async function doCreateFolderInPicker() {
    const name = document.getElementById('pickerNewFolderName').value.trim();
    if (!name) {
        toast('Digite o nome da pasta', 'error');
        return;
    }
    if (/[\/\\:*?"<>|]/.test(name)) {
        toast('Nome contém caracteres inválidos', 'error');
        return;
    }

    const currentPath = document.getElementById('newFolderPath').value || document.getElementById('pickerCurrentPath').textContent;
    if (!currentPath || currentPath === 'Selecione um local' || currentPath === 'Raízes do sistema') {
        toast('Navegue até um diretório primeiro', 'error');
        return;
    }

    // Use the browse API to construct the path and create via OS
    const newPath = currentPath.replace(/\\/g, '/').replace(/\/$/, '') + '/' + name;

    try {
        // Try to create using a simple POST to a create endpoint
        const result = await API.post('/api/browse/create-folder', { path: newPath });
        if (result.error) {
            toast('Erro: ' + result.error, 'error');
            return;
        }
        toast('Pasta criada: ' + name);
        hideCreateFolderInPicker();
        // Navigate into the created folder
        document.getElementById('newFolderPath').value = newPath;
        pickerHistory.push(currentPath);
        pickerNavigate(newPath);
    } catch (e) {
        toast('Erro ao criar pasta', 'error');
    }
}

// Folder picker state
let pickerHistory = [];

async function openFolderPicker() {
    const panel = document.getElementById('folderPickerPanel');
    panel.classList.remove('hidden');
    pickerHistory = [];
    await pickerNavigate('');
}

async function pickerNavigate(path) {
    const list = document.getElementById('pickerList');
    list.innerHTML = '<div class="picker-loading">🔍 Carregando...</div>';

    try {
        let url = '/api/browse';
        if (path) url += '?path=' + encodeURIComponent(path);
        const data = await API.get(url);

        // Update header
        document.getElementById('pickerCurrentPath').textContent = data.current || 'Raízes do sistema';
        document.getElementById('pickerUpBtn').disabled = !data.parent && !data.current;

        // Store current path
        if (data.current) {
            document.getElementById('newFolderPath').value = data.current;
        }

        // Build list
        list.innerHTML = '';

        if (!data.dirs || data.dirs.length === 0) {
            list.innerHTML = '<div class="picker-empty">Nenhuma subpasta encontrada</div>';
            return;
        }

        data.dirs.forEach(dir => {
            const item = document.createElement('div');
            item.className = 'picker-item';
            item.innerHTML = `
                <span class="picker-icon">📁</span>
                <span class="picker-name">${escHTML(dir.name)}</span>
                <span class="picker-arrow">›</span>
            `;
            item.addEventListener('click', () => {
                pickerHistory.push(data.current || '');
                document.getElementById('newFolderPath').value = dir.path;
                pickerNavigate(dir.path);
            });
            list.appendChild(item);
        });
    } catch (e) {
        list.innerHTML = `<div class="picker-empty">Erro ao listar diretórios</div>`;
    }
}

function pickerGoUp() {
    if (pickerHistory.length > 0) {
        const prev = pickerHistory.pop();
        pickerNavigate(prev);
    } else {
        pickerNavigate('');
    }
}

function pickerSelect() {
    const path = document.getElementById('newFolderPath').value;
    if (!path) { toast('Selecione uma pasta'); return; }

    document.getElementById('folderPickerPanel').classList.add('hidden');
    toast('Pasta selecionada: ' + path);
}

async function doAddFolder() {
    const path = document.getElementById('newFolderPath').value;
    const syncDel = document.getElementById('newFolderSyncDel').checked;

    if (!path) { toast('Selecione uma pasta'); return; }

    // Gera o label automaticamente a partir do nome da pasta
    const parts = path.replace(/\\/g, '/').split('/').filter(Boolean);
    const label = parts[parts.length - 1] || path;

    try {
        await API.post('/api/folders', { label, path, sync_delete: syncDel, enabled: true });
        toast('Pasta adicionada!');
        closeModal();
        loadFolders();
    } catch (e) { toast('Erro ao adicionar pasta', 'error'); }
}

function showAddPeerModal() {
    showModal('Adicionar Dispositivo', `
        <div class="form-group">
            <label>Nome</label>
            <input type="text" id="newPeerName" placeholder="Nome do dispositivo">
        </div>
        <div class="form-group">
            <label>IP Tailscale</label>
            <input type="text" id="newPeerIP" placeholder="100.x.x.x">
        </div>
        <div class="form-group">
            <label>Porta</label>
            <input type="number" id="newPeerPort" value="22000">
        </div>
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" onclick="doAddPeer()">Adicionar</button>
    `);
}

async function doAddPeer() {
    const name = document.getElementById('newPeerName').value;
    const ip = document.getElementById('newPeerIP').value;
    const port = parseInt(document.getElementById('newPeerPort').value) || 22000;

    if (!name || !ip) { toast('Preencha nome e IP'); return; }

    try {
        await API.post('/api/peers', { name, tailscale_ip: ip, port, enabled: true });
        toast('Dispositivo adicionado! Conectando...');
        closeModal();
        loadPeers();
    } catch (e) { toast('Erro', 'error'); }
}

async function showDiscoverPeersModal() {
    showModal('Buscando Máquinas na Rede...', '<div class="empty-state"><p>Buscando...</p></div>', '');

    try {
        const peers = await API.get('/api/peers/discover');
        if (!peers || peers.length === 0) {
            document.getElementById('modalBody').innerHTML = '<div class="empty-state"><p>Nenhuma máquina encontrada na rede Tailscale.</p></div>';
            document.getElementById('modalFooter').innerHTML = '<button class="btn-outline" onclick="closeModal()">Fechar</button>';
            return;
        }

        let html = `<p class="mb-16">Selecione as máquinas para sincronizar:</p>`;
        peers.forEach((p, i) => {
            const statusCls = p.already_added ? 'added' : (p.online ? 'online' : 'offline');
            const statusLabel = p.already_added ? 'Já adicionado' : (p.online ? 'Online' : 'Offline');
            const disabled = p.already_added ? 'disabled' : '';
            html += `
                <div class="discover-peer">
                    <label>
                        <input type="checkbox" name="discoverPeer" value="${i}" ${disabled}>
                        <strong>${escHTML(p.display_name)}</strong> — ${escHTML(p.ip)} [${escHTML(p.os || '?')}]
                    </label>
                    <span class="peer-badge ${statusCls}">${statusLabel}</span>
                </div>`;
        });

        document.getElementById('modalBody').innerHTML = html;
        document.getElementById('modalFooter').innerHTML = `
            <button class="btn-outline" onclick="closeModal()">Fechar</button>
            <button class="btn-primary" onclick="doAddDiscoveredPeers()">Adicionar Selecionados</button>`;

        // Store peers data for later
        window._discoveredPeers = peers;
    } catch (e) {
        document.getElementById('modalBody').innerHTML = `<div class="empty-state"><p>Erro: ${escHTML(e.message)}</p></div>`;
        document.getElementById('modalFooter').innerHTML = '<button class="btn-outline" onclick="closeModal()">Fechar</button>';
    }
}

async function doAddDiscoveredPeers() {
    const checks = document.querySelectorAll('input[name="discoverPeer"]:checked');
    const peers = window._discoveredPeers;
    let added = 0;

    for (const cb of checks) {
        const p = peers[parseInt(cb.value)];
        if (p.already_added) continue;
        try {
            await API.post('/api/peers', {
                name: p.display_name,
                tailscale_ip: p.ip,
                port: 22000,
                enabled: true,
            });
            added++;
        } catch (e) { }
    }

    if (added > 0) {
        toast(`${added} dispositivo(s) adicionado(s)!`);
        closeModal();
        loadPeers();
    } else {
        toast('Nenhum dispositivo selecionado');
    }
}

// ═══════════ NEW NOTE MODAL ═══════════
function showNewNoteModal() {
    if (!state.currentFolder) {
        toast('Selecione uma pasta primeiro', 'error');
        return;
    }

    const pathLabel = state.currentPath
        ? state.currentFolder + ' / ' + state.currentPath
        : state.currentFolder;

    showModal('Nova Anotação', `
        <div style="margin-bottom:12px;padding:10px 14px;background:rgba(26,115,232,0.06);border-radius:8px;font-size:13px;color:#1a73e8;">
            📁 <strong>Local:</strong> ${escHTML(pathLabel)}
        </div>
        <div class="form-group">
            <label>Nome do arquivo</label>
            <div style="display:flex;align-items:center;gap:4px;">
                <input type="text" id="newNoteNameInput" placeholder="minha-anotacao" style="flex:1;">
                <span style="color:#5f6368;font-size:14px;font-weight:500;">.md</span>
            </div>
            <small style="color:#80868b;margin-top:4px;display:block;">O arquivo será criado como Markdown (.md)</small>
        </div>
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" onclick="doCreateNote()">Criar e Abrir</button>
    `);
    setTimeout(() => document.getElementById('newNoteNameInput').focus(), 100);

    // Allow Enter to submit
    setTimeout(() => {
        const input = document.getElementById('newNoteNameInput');
        if (input) {
            input.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') { e.preventDefault(); doCreateNote(); }
            });
        }
    }, 120);
}

async function doCreateNote() {
    const nameInput = document.getElementById('newNoteNameInput');
    let name = (nameInput ? nameInput.value : '').trim();
    if (!name) {
        toast('Digite o nome do arquivo', 'error');
        return;
    }

    // Sanitize
    if (/[\/\\:*?"<>|]/.test(name)) {
        toast('Nome contém caracteres inválidos', 'error');
        return;
    }

    // Add .md if not present
    if (!name.toLowerCase().endsWith('.md')) {
        name += '.md';
    }

    try {
        const result = await API.post('/api/files/create', {
            folder_id: state.currentFolder,
            path: state.currentPath || '',
            name: name,
        });

        if (result.error) {
            toast('Erro: ' + result.error, 'error');
            return;
        }

        toast('Anotação criada: ' + name);
        closeModal();

        // Refresh file list and open the new note in editor
        await loadFiles(state.currentFolder, state.currentPath || '');

        const file = {
            name: name,
            path: result.path || name,
            is_dir: false,
            mime_type: 'text',
        };
        openMarkdownEditor(file);
    } catch (e) {
        toast('Erro ao criar anotação', 'error');
    }
}

// ═══════════ NEW SUBFOLDER MODAL ═══════════
function showNewSubfolderModal() {
    if (!state.currentFolder) {
        toast('Selecione uma pasta primeiro', 'error');
        return;
    }

    const pathLabel = state.currentPath
        ? state.currentFolder + ' / ' + state.currentPath
        : state.currentFolder;

    showModal('Nova Pasta', `
        <div style="margin-bottom:12px;padding:10px 14px;background:rgba(26,115,232,0.06);border-radius:8px;font-size:13px;color:#1a73e8;">
            📁 <strong>Local:</strong> ${escHTML(pathLabel)}
        </div>
        <div class="form-group">
            <label>Nome da pasta</label>
            <input type="text" id="newSubfolderNameInput" placeholder="Nome da nova pasta...">
        </div>
    `, `
        <button class="btn-outline" onclick="closeModal()">Cancelar</button>
        <button class="btn-primary" onclick="doCreateSubfolder()">Criar Pasta</button>
    `);
    setTimeout(() => document.getElementById('newSubfolderNameInput').focus(), 100);

    // Allow Enter to submit
    setTimeout(() => {
        const input = document.getElementById('newSubfolderNameInput');
        if (input) {
            input.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') { e.preventDefault(); doCreateSubfolder(); }
            });
        }
    }, 120);
}

async function doCreateSubfolder() {
    const nameInput = document.getElementById('newSubfolderNameInput');
    const name = (nameInput ? nameInput.value : '').trim();
    if (!name) {
        toast('Digite o nome da pasta', 'error');
        return;
    }

    if (/[\/\\:*?"<>|]/.test(name)) {
        toast('Nome contém caracteres inválidos', 'error');
        return;
    }

    try {
        const result = await API.post('/api/folders/create-subfolder', {
            folder_id: state.currentFolder,
            path: state.currentPath || '',
            name: name,
        });

        if (result.error) {
            toast('Erro: ' + result.error, 'error');
            return;
        }

        toast('Pasta criada: ' + name);
        closeModal();

        // Refresh file list
        loadFiles(state.currentFolder, state.currentPath || '');
    } catch (e) {
        toast('Erro ao criar pasta', 'error');
    }
}

