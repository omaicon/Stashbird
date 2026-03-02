// ═══════════════════════════════════════════════
// GRAPH VIEW MODULE — Visualização em Grafo
// Estilo Obsidian: notas como nós, wikilinks como arestas
// ═══════════════════════════════════════════════

const graphView = {
    simulation: null,
    data: null,
    activeFolder: '',
    isLoaded: false,
};

// ── Initialize Graph View ──
function initGraphView() {
    const refreshBtn = document.getElementById('graphRefreshBtn');
    if (refreshBtn) {
        refreshBtn.addEventListener('click', () => loadGraphView());
    }

    const filter = document.getElementById('graphFolderFilter');
    if (filter) {
        filter.addEventListener('change', () => {
            graphView.activeFolder = filter.value;
            loadGraphView();
        });
    }
}

// ── Load Graph Data ──
async function loadGraphView() {
    const container = document.getElementById('graphViewContainer');
    if (!container) return;

    // Show loading
    const svg = document.getElementById('graphViewSvg');
    svg.innerHTML = '';
    document.getElementById('graphStats').innerHTML = '';
    document.getElementById('graphTooltip').classList.add('hidden');

    container.insertAdjacentHTML('beforeend',
        '<div class="graph-loading" id="graphLoading"><div class="spinner"></div>Carregando grafo...</div>'
    );

    try {
        // Populate folder filter
        await populateGraphFolderFilter();

        const folder = graphView.activeFolder;
        let url = '/api/notes/graph';
        if (folder) url += `?folder=${encodeURIComponent(folder)}`;

        const data = await API.get(url);
        
        // Remove loading
        const loading = document.getElementById('graphLoading');
        if (loading) loading.remove();

        if (!data || !data.nodes || data.nodes.length === 0) {
            showGraphEmpty();
            return;
        }

        graphView.data = data;
        graphView.isLoaded = true;
        renderGraphView(data);

    } catch (e) {
        console.error('[GraphView] Error:', e);
        const loading = document.getElementById('graphLoading');
        if (loading) loading.remove();
        showGraphEmpty('Erro ao carregar grafo');
    }
}

// ── Populate folder filter ──
async function populateGraphFolderFilter() {
    const filter = document.getElementById('graphFolderFilter');
    if (!filter) return;

    const current = filter.value;
    filter.innerHTML = '<option value="">Todas as pastas</option>';

    try {
        if (!state.folders || state.folders.length === 0) {
            state.folders = await API.get('/api/folders');
        }
        if (state.folders && state.folders.length > 0) {
            state.folders.forEach(f => {
                const opt = document.createElement('option');
                opt.value = f.id;
                opt.textContent = f.label || f.id;
                if (f.id === current) opt.selected = true;
                filter.appendChild(opt);
            });
        }
    } catch (_) {}
}

// ── Empty State ──
function showGraphEmpty(msg) {
    const svg = document.getElementById('graphViewSvg');
    svg.innerHTML = '';

    const container = document.getElementById('graphViewContainer');
    // Remove any previous empty
    const prev = container.querySelector('.graph-empty');
    if (prev) prev.remove();

    container.insertAdjacentHTML('beforeend', `
        <div class="graph-empty">
            <svg width="64" height="64" viewBox="0 0 24 24" fill="#8b949e">
                <circle cx="5" cy="12" r="2"/><circle cx="12" cy="5" r="2"/><circle cx="19" cy="12" r="2"/><circle cx="12" cy="19" r="2"/>
                <line x1="7" y1="11" x2="10" y2="6.5" stroke="#8b949e" stroke-width="1.2"/>
                <line x1="14" y1="6.5" x2="17" y2="11" stroke="#8b949e" stroke-width="1.2"/>
                <line x1="17" y1="13" x2="14" y2="17.5" stroke="#8b949e" stroke-width="1.2"/>
                <line x1="10" y1="17.5" x2="7" y2="13" stroke="#8b949e" stroke-width="1.2"/>
            </svg>
            <h3>${msg || 'Nenhuma nota com links encontrada'}</h3>
            <p>Use <code>[[wikilinks]]</code> nas suas anotações Markdown para conectar notas e visualizar o grafo de relações.</p>
        </div>
    `);

    document.getElementById('graphStats').innerHTML = '';
}

// ── Render Graph with D3.js ──
function renderGraphView(data) {
    const svgEl = document.getElementById('graphViewSvg');
    const svg = d3.select('#graphViewSvg');
    svg.selectAll('*').remove();

    const container = document.getElementById('graphViewContainer');
    // Remove empty state if present
    const empty = container.querySelector('.graph-empty');
    if (empty) empty.remove();

    const width = container.clientWidth;
    const height = container.clientHeight;

    // Build adjacency info
    const nodeById = new Map();
    const linkCountMap = new Map();
    data.nodes.forEach(n => {
        nodeById.set(n.id, n);
        linkCountMap.set(n.id, 0);
    });
    data.links.forEach(l => {
        const sId = typeof l.source === 'object' ? l.source.id : l.source;
        const tId = typeof l.target === 'object' ? l.target.id : l.target;
        linkCountMap.set(sId, (linkCountMap.get(sId) || 0) + 1);
        linkCountMap.set(tId, (linkCountMap.get(tId) || 0) + 1);
    });

    // Node radius based on connections
    function nodeRadius(d) {
        const count = linkCountMap.get(d.id) || 0;
        return Math.max(4, Math.min(14, 4 + count * 1.5));
    }

    // Node color
    function nodeColor(d) {
        if (!d.has_file) return '#484f58'; // ghost/unresolved node
        const count = linkCountMap.get(d.id) || 0;
        if (count >= 6) return '#a371f7'; // hub — purple
        if (count >= 3) return '#58a6ff'; // connected — blue
        return '#7ee787'; // leaf — green
    }

    // Simulation
    const simulation = d3.forceSimulation(data.nodes)
        .force('link', d3.forceLink(data.links).id(d => d.id).distance(80).strength(0.4))
        .force('charge', d3.forceManyBody().strength(-180))
        .force('center', d3.forceCenter(width / 2, height / 2))
        .force('collision', d3.forceCollide().radius(d => nodeRadius(d) + 12))
        .force('x', d3.forceX(width / 2).strength(0.03))
        .force('y', d3.forceY(height / 2).strength(0.03));

    graphView.simulation = simulation;

    const g = svg.append('g');

    // Zoom & Pan
    const zoom = d3.zoom()
        .scaleExtent([0.1, 6])
        .on('zoom', (event) => g.attr('transform', event.transform));
    svg.call(zoom);

    // Arrow defs
    svg.append('defs').append('marker')
        .attr('id', 'gv-arrowhead')
        .attr('viewBox', '0 -5 10 10')
        .attr('refX', 18)
        .attr('refY', 0)
        .attr('markerWidth', 6)
        .attr('markerHeight', 6)
        .attr('orient', 'auto')
        .append('path')
        .attr('d', 'M0,-5L10,0L0,5')
        .attr('class', 'gv-arrow');

    // Links
    const link = g.append('g')
        .selectAll('line')
        .data(data.links)
        .join('line')
        .attr('class', 'gv-link')
        .attr('stroke-width', 1)
        .attr('marker-end', 'url(#gv-arrowhead)');

    // Nodes group
    const node = g.append('g')
        .selectAll('g')
        .data(data.nodes)
        .join('g')
        .attr('class', 'gv-node')
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

    // Node circles
    node.append('circle')
        .attr('r', d => nodeRadius(d))
        .attr('fill', d => nodeColor(d))
        .attr('stroke', d => d.has_file ? 'rgba(255,255,255,0.1)' : 'rgba(255,255,255,0.05)')
        .attr('stroke-width', 1.5);

    // Node labels
    node.append('text')
        .attr('dx', d => nodeRadius(d) + 6)
        .attr('dy', 4)
        .text(d => d.name);

    // ── Hover Interactions (Obsidian-style highlight) ──
    const tooltip = document.getElementById('graphTooltip');
    const connectedNodes = new Set();

    node.on('mouseenter', function (event, d) {
        connectedNodes.clear();
        connectedNodes.add(d.id);

        // Find all connected nodes
        data.links.forEach(l => {
            const sId = typeof l.source === 'object' ? l.source.id : l.source;
            const tId = typeof l.target === 'object' ? l.target.id : l.target;
            if (sId === d.id) connectedNodes.add(tId);
            if (tId === d.id) connectedNodes.add(sId);
        });

        // Highlight/dim nodes
        node.classed('highlighted', n => connectedNodes.has(n.id))
            .classed('dimmed', n => !connectedNodes.has(n.id));

        // Highlight/dim links
        link.classed('highlighted', l => {
            const sId = typeof l.source === 'object' ? l.source.id : l.source;
            const tId = typeof l.target === 'object' ? l.target.id : l.target;
            return sId === d.id || tId === d.id;
        }).classed('dimmed', l => {
            const sId = typeof l.source === 'object' ? l.source.id : l.source;
            const tId = typeof l.target === 'object' ? l.target.id : l.target;
            return sId !== d.id && tId !== d.id;
        });

        // Show tooltip
        const linkCount = linkCountMap.get(d.id) || 0;
        tooltip.innerHTML = `
            <div class="tt-name">${escHTML(d.name)}</div>
            <div class="tt-path">${escHTML(d.path)}</div>
            <div class="tt-links"><span>${linkCount}</span> conexão(ões) ${d.has_file ? '' : '· <em>não resolvido</em>'}</div>
        `;
        tooltip.classList.remove('hidden');
    })
    .on('mousemove', function (event) {
        const rect = container.getBoundingClientRect();
        const x = event.clientX - rect.left + 14;
        const y = event.clientY - rect.top - 10;
        tooltip.style.left = x + 'px';
        tooltip.style.top = y + 'px';
    })
    .on('mouseleave', function () {
        node.classed('highlighted', false).classed('dimmed', false);
        link.classed('highlighted', false).classed('dimmed', false);
        tooltip.classList.add('hidden');
    });

    // ── Click to open note ──
    node.on('click', (event, d) => {
        if (d.has_file && d.path && d.folder_id) {
            // Set folder context so openWikiLink / openMarkdownEditor work  
            state.currentFolder = d.folder_id;
            if (typeof openWikiLink === 'function') {
                openWikiLink(d.path);
            } else {
                switchView('files');
            }
        }
    });

    // ── Tick ──
    simulation.on('tick', () => {
        link
            .attr('x1', d => d.source.x)
            .attr('y1', d => d.source.y)
            .attr('x2', d => d.target.x)
            .attr('y2', d => d.target.y);

        node.attr('transform', d => `translate(${d.x},${d.y})`);
    });

    // ── Stats ──
    const statsEl = document.getElementById('graphStats');
    statsEl.innerHTML = `
        <span>Notas: <span class="stat-value">${data.nodes.length}</span></span>
        <span>Links: <span class="stat-value">${data.links.length}</span></span>
    `;

    // Auto-zoom to fit
    simulation.on('end', () => {
        fitGraphToView(svg, g, width, height, zoom);
    });

    // Also fit after some stabilization time
    setTimeout(() => {
        fitGraphToView(svg, g, width, height, zoom);
    }, 2000);
}

// ── Fit graph to viewport ──
function fitGraphToView(svg, g, width, height, zoom) {
    try {
        const bounds = g.node().getBBox();
        if (bounds.width === 0 || bounds.height === 0) return;

        const padding = 60;
        const fullWidth = bounds.width + padding * 2;
        const fullHeight = bounds.height + padding * 2;
        const scale = Math.min(width / fullWidth, height / fullHeight, 1.5);
        const tx = (width - bounds.width * scale) / 2 - bounds.x * scale;
        const ty = (height - bounds.height * scale) / 2 - bounds.y * scale;

        svg.transition().duration(750).call(
            zoom.transform,
            d3.zoomIdentity.translate(tx, ty).scale(scale)
        );
    } catch (_) {}
}

// ── Cleanup ──
function destroyGraphView() {
    if (graphView.simulation) {
        graphView.simulation.stop();
        graphView.simulation = null;
    }
    graphView.isLoaded = false;
}
