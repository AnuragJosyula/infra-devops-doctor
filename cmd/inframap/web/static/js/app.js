/**
 * InfraMap — Dashboard + Graph UI
 */

const TYPE_STYLE = {
  region:           { color: '#f59e0b', icon: '🌍' },
  vpc:              { color: '#6366f1', icon: '☁️' },
  subnet:           { color: '#818cf8', icon: '🛤️' },
  internet_gateway: { color: '#14b8a6', icon: '🚪' },
  nat_gateway:      { color: '#0ea5e9', icon: '🔀' },
  security_group:   { color: '#f43f5e', icon: '🛡️' },
  alb:              { color: '#a855f7', icon: '⚖️' },
  ec2:              { color: '#f97316', icon: '💻' },
  rds:              { color: '#3b82f6', icon: '🗄️' },
  elasticache:      { color: '#ef4444', icon: '⚡' },
  s3_bucket:        { color: '#22c55e', icon: '🪣' },
  cloudfront:       { color: '#8b5cf6', icon: '🛰️' },
  dns_record:       { color: '#06b6d4', icon: '📡' },
  ecs_cluster:      { color: '#eab308', icon: '🧩' },
  ecs_service:      { color: '#facc15', icon: '⚙️' },
  ecs_task:         { color: '#fde047', icon: '📋' },
  lambda:           { color: '#e879f9', icon: 'λ' },
  cloudwatch:       { color: '#10b981', icon: '📈' },
  sns:              { color: '#ec4899', icon: '📣' },
  sqs:              { color: '#d946ef', icon: '📨' },
  docker_daemon:    { color: '#2496ed', icon: '🐳' },
  container:        { color: '#38bdf8', icon: '📦' },
  image:            { color: '#0284c7', icon: '💿' },
  network:          { color: '#0ea5e9', icon: '🌐' },
  volume:           { color: '#06b6d4', icon: '💾' },
  route53:          { color: '#06b6d4', icon: '🗺️' },
  iam_role:         { color: '#94a3b8', icon: '🔑' },
  eip:              { color: '#0ea5e9', icon: '📍' },
  disk:             { color: '#06b6d4', icon: '💾' },
  // azure
  resource_group:   { color: '#0078d4', icon: '🗂️' },
  vm:               { color: '#0078d4', icon: '💻' },
  vnet:             { color: '#6366f1', icon: '☁️' },
  nsg:              { color: '#f43f5e', icon: '🛡️' },
  storage_account:  { color: '#22c55e', icon: '🪣' },
  sql_server:       { color: '#3b82f6', icon: '🗄️' },
  sql_database:     { color: '#3b82f6', icon: '🗄️' },
  app_service:      { color: '#a855f7', icon: '🌐' },
  aks:              { color: '#326ce5', icon: '☸️' },
  key_vault:        { color: '#eab308', icon: '🔐' },
  load_balancer:    { color: '#a855f7', icon: '⚖️' },
  // gcp
  gcp_project:      { color: '#4285f4', icon: '📁' },
  gce_instance:     { color: '#4285f4', icon: '💻' },
  gcp_network:      { color: '#6366f1', icon: '☁️' },
  gcs_bucket:       { color: '#22c55e', icon: '🪣' },
  cloud_sql:        { color: '#3b82f6', icon: '🗄️' },
  default:          { color: '#64748b', icon: '⬡' },
};

const SEV_META = {
  critical: { color: '#ef4444', order: 0 },
  high:     { color: '#f97316', order: 1 },
  medium:   { color: '#f59e0b', order: 2 },
  low:      { color: '#3b82f6', order: 3 },
};

const STATUS_COLOR = {
  running: '#22c55e', active: '#22c55e', healthy: '#22c55e', available: '#22c55e',
  degraded: '#f59e0b', pending: '#f59e0b',
  stopped: '#94a3b8', error: '#ef4444', unhealthy: '#ef4444',
  unknown: '#64748b',
};
const OK_STATUSES = new Set(['running', 'active', 'healthy', 'available']);

const EDGE_STYLE = {
  contains:    { color: null,      dash: '4,4', label: 'contains' },   // null → theme edge color
  connects_to: { color: '#06b6d4', dash: null,  label: 'connects to' },
  depends_on:  { color: '#a78bfa', dash: null,  label: 'depends on' },
  routes_to:   { color: '#fb923c', dash: null,  label: 'routes to' },
  attached_to: { color: '#34d399', dash: null,  label: 'attached to' },
  mounts:      { color: '#34d399', dash: '2,3', label: 'mounts' },
  default:     { color: '#64748b', dash: null,  label: '' },
};

const PROVIDER_META = {
  aws:    { icon: '☁️', label: 'Amazon Web Services', color: '#f59e0b' },
  azure:  { icon: '🔷', label: 'Microsoft Azure',     color: '#0078d4' },
  gcp:    { icon: '🔴', label: 'Google Cloud',        color: '#4285f4' },
  docker: { icon: '🐳', label: 'Docker Engine',       color: '#2496ed' },
  mock:   { icon: '🧪', label: 'Simulated (demo)',    color: '#a78bfa' },
};

const state = {
  rawData: { nodes: [], edges: [], stats: {} },
  hierarchyRoot: null,
  currentView: 'dashboard',
  lastGraphView: 'force',
  searchQuery: '',
  statusFilter: null,
  contextNode: null,
  selectedId: null,
  lastUpdated: null,
  findings: [],
  nodeSeverity: {},   // nodeId → worst severity
  doctorFilter: null, // severity filter in doctor view
  costMode: false,
  diff: null,         // { map: {nodeId: 'added'|'changed'}, removed: [...] }
};

let svg, g, zoom, simulation;
let minimapSvg, minimapViewport;

// ═══════════════════════════════════════════════════════
// Init
// ═══════════════════════════════════════════════════════

document.addEventListener('DOMContentLoaded', () => {
  initTheme();
  initGraph();
  initMinimap();
  initContextMenu();
  initEventListeners();
  connectWebSocket();
  fetchData();
});

function initTheme() {
  const btn = document.getElementById('btn-theme');
  const sun = document.querySelector('.icon-sun');
  const moon = document.querySelector('.icon-moon');
  const apply = (t) => {
    document.documentElement.setAttribute('data-theme', t);
    sun.style.display = t === 'dark' ? 'block' : 'none';
    moon.style.display = t === 'dark' ? 'none' : 'block';
  };
  apply(localStorage.getItem('theme') || 'dark');
  btn.addEventListener('click', () => {
    const next = document.documentElement.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
    localStorage.setItem('theme', next);
    apply(next);
    renderCurrentView();
  });
}

async function fetchData() {
  try {
    const res = await fetch('/api/graph');
    ingest(await res.json());
    await fetchFindings();
    await loadSnapshotList();
    renderCurrentView();
  } catch (err) {
    console.error('Fetch failed:', err);
  }
}

async function fetchFindings() {
  try {
    state.findings = await (await fetch('/api/findings')).json() || [];
  } catch (_) { state.findings = []; }
  state.nodeSeverity = {};
  state.findings.forEach(f => {
    const cur = state.nodeSeverity[f.node_id];
    if (!cur || SEV_META[f.severity].order < SEV_META[cur].order) state.nodeSeverity[f.node_id] = f.severity;
  });
  const badge = document.getElementById('doctor-badge');
  badge.textContent = state.findings.length;
  badge.classList.toggle('hidden', state.findings.length === 0);
}

function ingest(data) {
  state.rawData = data;
  state.lastUpdated = new Date();
  buildHierarchy();
  buildSidebar();
  updateFooter();
  renderCurrentView();
}

function connectWebSocket() {
  const dot = document.getElementById('live-dot');
  const label = document.getElementById('live-label');
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${proto}://${location.host}/ws`);

  ws.onopen = () => { dot.classList.remove('offline'); label.textContent = 'Live'; };
  ws.onmessage = (ev) => {
    try {
      const msg = JSON.parse(ev.data);
      if (msg.type === 'full_graph') {
        ingest({ nodes: msg.nodes, edges: msg.edges, stats: msg.stats });
        fetchFindings().then(() => { loadSnapshotList(); renderCurrentView(); });
      }
    } catch (_) { /* ignore malformed frames */ }
  };
  ws.onclose = () => {
    dot.classList.add('offline'); label.textContent = 'Reconnecting…';
    setTimeout(connectWebSocket, 3000);
  };
}

function updateFooter() {
  const s = state.rawData.stats || {};
  document.getElementById('footer-stats').textContent =
    `${s.total_nodes ?? 0} resources · ${s.total_edges ?? 0} connections`;
  document.getElementById('footer-updated').textContent =
    state.lastUpdated ? `updated ${state.lastUpdated.toLocaleTimeString()}` : '';
}

// ═══════════════════════════════════════════════════════
// View switching
// ═══════════════════════════════════════════════════════

function renderCurrentView() {
  const v = state.currentView;
  document.getElementById('dashboard').classList.toggle('hidden', v !== 'dashboard');
  document.getElementById('doctor').classList.toggle('hidden', v !== 'doctor');
  document.getElementById('graph-area').classList.toggle('hidden', v === 'dashboard' || v === 'doctor');
  if (v === 'dashboard') renderDashboard();
  else if (v === 'doctor') renderDoctor();
  else { updateGraph(); requestAnimationFrame(() => zoomToFit(0)); }
}

// ═══════════════════════════════════════════════════════
// Dashboard
// ═══════════════════════════════════════════════════════

function renderDashboard() {
  const { nodes, edges, stats } = state.rawData;
  const s = stats || {};

  // ── stat cards
  document.getElementById('stat-nodes').textContent = s.total_nodes ?? nodes.length;
  document.getElementById('stat-nodes-sub').textContent =
    `${Object.keys(s.nodes_by_type || {}).length} distinct types`;

  document.getElementById('stat-edges').textContent = s.total_edges ?? edges.length;
  const relEdges = edges.filter(e => e.type !== 'contains').length;
  document.getElementById('stat-edges-sub').textContent = `${relEdges} cross-resource links`;

  const cost = s.total_monthly_cost || 0;
  document.getElementById('stat-cost').textContent = fmtCost(cost);

  const total = nodes.length || 1;
  const ok = nodes.filter(n => OK_STATUSES.has(n.status)).length;
  const pct = Math.round((ok / total) * 100);
  const healthEl = document.getElementById('stat-health');
  healthEl.textContent = `${pct}%`;
  healthEl.style.color = pct >= 90 ? 'var(--green)' : pct >= 70 ? 'var(--amber)' : 'var(--red)';
  const bad = total - ok;
  document.getElementById('stat-health-sub').innerHTML = bad === 0
    ? `<span class="up">all resources healthy</span>`
    : `<span class="warn">${bad} resource${bad > 1 ? 's' : ''} need attention</span>`;

  renderTypeChart(s.nodes_by_type || {});
  renderStatusDonut(s.nodes_by_status || {}, nodes.length);
  renderProviderList(s.nodes_by_provider || {});
  renderEdgeList(edges);
  renderDashFindings();
}

function findingCard(f, compact) {
  const sev = SEV_META[f.severity] || SEV_META.low;
  return `
    <div class="finding" style="--sev:${sev.color}" data-node="${f.node_id}">
      <div class="finding-head">
        <span class="badge" style="color:${sev.color};background:${hexA(sev.color, 0.12)}">${f.severity}</span>
        <span class="finding-title">${f.title}</span>
        <span class="finding-node">${f.node_name}</span>
        <span class="finding-cat">${f.category.replace('_', ' ')}</span>
      </div>
      ${compact ? '' : `<div class="finding-detail">${f.detail}</div><div class="finding-fix">${f.fix}</div>`}
    </div>`;
}

function wireFindingClicks(container) {
  container.querySelectorAll('.finding').forEach(el => {
    el.addEventListener('click', () => selectResource(el.dataset.node));
  });
}

function renderDashFindings() {
  const el = document.getElementById('dash-findings');
  const sorted = [...state.findings].sort((a, b) => SEV_META[a.severity].order - SEV_META[b.severity].order);
  document.getElementById('findings-hint').textContent = sorted.length ? `${sorted.length} total — see Doctor tab` : '';
  el.innerHTML = sorted.length
    ? sorted.slice(0, 5).map(f => findingCard(f, true)).join('')
    : '<div class="empty-note">✅ No issues found — infrastructure looks healthy</div>';
  wireFindingClicks(el);
}

// ═══════════════════════════════════════════════════════
// Doctor view
// ═══════════════════════════════════════════════════════

function renderDoctor() {
  const summary = document.getElementById('doctor-summary');
  const list = document.getElementById('doctor-list');
  const counts = { critical: 0, high: 0, medium: 0, low: 0 };
  state.findings.forEach(f => counts[f.severity]++);

  summary.innerHTML = Object.entries(counts).map(([sev, n]) => `
    <div class="sev-card ${state.doctorFilter === sev ? 'active' : ''}" style="--sev:${SEV_META[sev].color}" data-sev="${sev}">
      <div class="sev-n" style="color:${SEV_META[sev].color}">${n}</div>
      <div class="sev-l">${sev}</div>
    </div>`).join('');
  summary.querySelectorAll('.sev-card').forEach(card => {
    card.addEventListener('click', () => {
      state.doctorFilter = state.doctorFilter === card.dataset.sev ? null : card.dataset.sev;
      renderDoctor();
    });
  });

  let items = [...state.findings].sort((a, b) => SEV_META[a.severity].order - SEV_META[b.severity].order);
  if (state.doctorFilter) items = items.filter(f => f.severity === state.doctorFilter);
  list.innerHTML = items.length
    ? items.map(f => findingCard(f, false)).join('')
    : '<div class="empty-note">✅ Nothing here — all clear</div>';
  wireFindingClicks(list);
}

function renderTypeChart(byType) {
  const el = document.getElementById('chart-types');
  const entries = Object.entries(byType).sort((a, b) => b[1] - a[1]);
  if (!entries.length) { el.innerHTML = '<div class="empty-note">No data yet</div>'; return; }
  const max = entries[0][1];
  el.innerHTML = entries.map(([type, count]) => {
    const st = typeStyle(type);
    return `
      <div class="type-row">
        <div class="t-label"><span class="t-swatch" style="background:${st.color}"></span>${st.icon} ${type}</div>
        <div class="t-track"><div class="t-bar" style="width:${(count / max) * 100}%;background:${st.color}"></div></div>
        <div class="t-count">${count}</div>
      </div>`;
  }).join('');
}

function renderStatusDonut(byStatus, total) {
  const el = document.getElementById('chart-status');
  el.innerHTML = '';
  const entries = Object.entries(byStatus).sort((a, b) => b[1] - a[1]);
  if (!entries.length) { el.innerHTML = '<div class="empty-note">No data yet</div>'; return; }

  const size = 150, r = size / 2;
  const dsvg = d3.select(el).append('svg').attr('width', size).attr('height', size)
    .append('g').attr('transform', `translate(${r},${r})`);

  const pie = d3.pie().value(d => d[1]).sort(null).padAngle(0.03);
  const arc = d3.arc().innerRadius(r - 22).outerRadius(r - 4).cornerRadius(3);

  dsvg.selectAll('path').data(pie(entries)).enter().append('path')
    .attr('d', arc)
    .attr('fill', d => STATUS_COLOR[d.data[0]] || STATUS_COLOR.unknown);

  dsvg.append('text').attr('class', 'donut-center-label').attr('text-anchor', 'middle').attr('dy', '0.05em').text(total);
  dsvg.append('text').attr('class', 'donut-center-sub').attr('text-anchor', 'middle').attr('dy', '1.9em').text('resources');

  const legend = document.createElement('div');
  legend.className = 'donut-legend';
  legend.innerHTML = entries.map(([status, n]) => `
    <div class="dl-row">
      <span class="dot" style="background:${STATUS_COLOR[status] || STATUS_COLOR.unknown}"></span>
      ${status}<span class="n">${n}</span>
    </div>`).join('');
  el.appendChild(legend);
}

function renderProviderList(byProvider) {
  const el = document.getElementById('provider-list');
  const entries = Object.entries(byProvider).sort((a, b) => b[1] - a[1]);
  if (!entries.length) { el.innerHTML = '<div class="empty-note">No providers connected</div>'; return; }
  el.innerHTML = entries.map(([p, n]) => {
    const meta = PROVIDER_META[p] || { icon: '⬡', label: p, color: '#64748b' };
    return `
      <div class="p-row">
        <div class="p-icon" style="background:${hexA(meta.color, 0.15)}">${meta.icon}</div>
        <div class="p-body">
          <div class="p-name">${p}</div>
          <div class="p-sub">${meta.label}</div>
        </div>
        <div class="p-count" style="color:${meta.color}">${n}</div>
      </div>`;
  }).join('');
}

function renderEdgeList(edges) {
  const el = document.getElementById('edge-list');
  const counts = {};
  edges.forEach(e => { counts[e.type] = (counts[e.type] || 0) + 1; });
  const entries = Object.entries(counts).sort((a, b) => b[1] - a[1]);
  if (!entries.length) { el.innerHTML = '<div class="empty-note">No connections yet</div>'; return; }
  const max = entries[0][1];
  el.innerHTML = entries.map(([type, n]) => {
    const st = EDGE_STYLE[type] || EDGE_STYLE.default;
    const color = st.color || 'var(--edge-color)';
    return `
      <div class="e-row">
        <div class="e-label"><span class="e-line ${st.dash ? 'dashed' : ''}" style="border-color:${color}"></span>${st.label || type}</div>
        <div class="e-track"><div class="e-bar" style="width:${(n / max) * 100}%;background:${color}"></div></div>
        <div class="e-count">${n}</div>
      </div>`;
  }).join('');
}

// ═══════════════════════════════════════════════════════
// Sidebar
// ═══════════════════════════════════════════════════════

function buildSidebar() {
  buildStatusChips();
  buildNav();
}

function buildStatusChips() {
  const el = document.getElementById('status-chips');
  const counts = {};
  state.rawData.nodes.forEach(n => { if (n.status) counts[n.status] = (counts[n.status] || 0) + 1; });

  let html = `<button class="chip ${state.statusFilter === null ? 'active' : ''}" data-status="">All <span class="n">${state.rawData.nodes.length}</span></button>`;
  Object.entries(counts).sort((a, b) => b[1] - a[1]).forEach(([status, n]) => {
    html += `<button class="chip ${state.statusFilter === status ? 'active' : ''}" data-status="${status}">
      <span class="dot" style="background:${STATUS_COLOR[status] || STATUS_COLOR.unknown}"></span>${status} <span class="n">${n}</span>
    </button>`;
  });
  el.innerHTML = html;

  el.querySelectorAll('.chip').forEach(chip => {
    chip.addEventListener('click', () => {
      state.statusFilter = chip.dataset.status || null;
      buildStatusChips();
      applyFilters();
    });
  });
}

function buildNav() {
  const container = document.getElementById('nav-content');
  const groups = {};
  state.rawData.nodes.forEach(n => {
    const gName = n.group || 'Other';
    (groups[gName] = groups[gName] || []).push(n);
  });

  const caret = `<span class="caret"><svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3"><polyline points="9 18 15 12 9 6"/></svg></span>`;

  container.innerHTML = Object.keys(groups).sort().map(gName => {
    const nodes = groups[gName].sort((a, b) => a.name.localeCompare(b.name));
    return `
      <div class="nav-group expanded">
        <div class="nav-group-header">${caret}<span>${gName}</span><span class="count">${nodes.length}</span></div>
        <ul class="nav-items">
          ${nodes.map(n => `
            <li class="nav-item" data-id="${n.id}" data-status="${n.status || ''}" data-search="${(n.name + ' ' + n.type + ' ' + n.id).toLowerCase()}">
              <span class="status-dot" style="background:${STATUS_COLOR[n.status] || STATUS_COLOR.unknown}"></span>
              <span class="nav-name">${n.name}</span>
              <span class="nav-type">${n.type}</span>
            </li>`).join('')}
        </ul>
      </div>`;
  }).join('');

  container.querySelectorAll('.nav-group-header').forEach(hdr => {
    hdr.addEventListener('click', () => hdr.parentElement.classList.toggle('expanded'));
  });

  container.querySelectorAll('.nav-item').forEach(item => {
    item.addEventListener('click', () => selectResource(item.dataset.id));
  });

  applyFilters();
}

function applyFilters() {
  const q = state.searchQuery.toLowerCase();
  document.querySelectorAll('.nav-item').forEach(item => {
    const matchesSearch = !q || item.dataset.search.includes(q);
    const matchesStatus = !state.statusFilter || item.dataset.status === state.statusFilter;
    item.classList.toggle('dimmed', !(matchesSearch && matchesStatus));
  });
  applyGraphSearch();
}

function selectResource(id) {
  state.selectedId = id;
  document.querySelectorAll('.nav-item').forEach(i => i.classList.toggle('active', i.dataset.id === id));

  const node = state.rawData.nodes.find(n => n.id === id);
  if (node) showDetails(node);

  // If not on a graph view, jump to the last-used one to show it in context
  if (!['force', 'tree', 'radial'].includes(state.currentView)) switchView(state.lastGraphView);

  // Expand ancestors so the node is visible, then highlight it
  let target = null;
  state.hierarchyRoot.each(d => { if (d.data.id === id) target = d; });
  if (target) {
    let p = target.parent;
    while (p) {
      if (p._children) { p.children = p._children; p._children = null; }
      p = p.parent;
    }
    updateGraph();
    highlightSelection();
    updateBreadcrumb(target);
  }
}

function switchView(view) {
  state.currentView = view;
  if (['force', 'tree', 'radial'].includes(view)) state.lastGraphView = view;
  document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.view === view));
  renderCurrentView();
}

// ═══════════════════════════════════════════════════════
// Hierarchy
// ═══════════════════════════════════════════════════════

function buildHierarchy() {
  const rootData = { id: 'root', name: 'Infrastructure', type: 'root', children: [] };
  const map = new Map();
  map.set('root', rootData);
  state.rawData.nodes.forEach(n => map.set(n.id, { ...n, children: [] }));
  state.rawData.nodes.forEach(n => {
    const node = map.get(n.id);
    const parent = map.get(n.parent || 'root');
    if (parent && parent !== node) parent.children.push(node);
    else rootData.children.push(node);
  });
  state.hierarchyRoot = d3.hierarchy(rootData);
}

function setAllCollapsed(collapsed) {
  state.hierarchyRoot.each(d => {
    if (collapsed) {
      if (d.depth >= 1 && d.children) { d._children = d.children; d.children = null; }
    } else {
      if (d._children) { d.children = d._children; d._children = null; }
    }
  });
  updateGraph();
  zoomToFit();
}

// ═══════════════════════════════════════════════════════
// Graph core
// ═══════════════════════════════════════════════════════

function initGraph() {
  svg = d3.select('#graph-svg');

  // arrowhead markers per edge type
  const defs = svg.append('defs');
  Object.entries(EDGE_STYLE).forEach(([type, st]) => {
    defs.append('marker')
      .attr('id', `arrow-${type}`)
      .attr('viewBox', '0 -4 8 8').attr('refX', 7).attr('refY', 0)
      .attr('markerWidth', 6).attr('markerHeight', 6).attr('orient', 'auto')
      .append('path').attr('d', 'M0,-4L8,0L0,4')
      .attr('fill', st.color || '#64748b');
  });

  g = svg.append('g');
  zoom = d3.zoom().scaleExtent([0.08, 4]).on('zoom', e => {
    g.attr('transform', e.transform);
    updateMinimapViewport(e.transform);
  });
  svg.call(zoom).on('dblclick.zoom', null);
}

function visibleNodes() {
  return state.hierarchyRoot.descendants().filter(d => d.data.id !== 'root');
}

function updateGraph() {
  if (!state.hierarchyRoot) return;
  g.selectAll('*').remove();
  if (simulation) simulation.stop();

  const view = state.currentView === 'dashboard' ? state.lastGraphView : state.currentView;
  if (view === 'force') renderForce();
  else if (view === 'tree') renderTree();
  else if (view === 'radial') renderRadial();

  buildLegend(view);
  highlightSelection();
  applyGraphSearch();
  applyAnnotations();
  setTimeout(updateMinimapContent, 120);
}

// severity pulses, time-travel diff rings, cost labels
function applyAnnotations() {
  g.selectAll('.node')
    .attr('class', function(d) {
      let cls = 'node';
      const sev = state.nodeSeverity[d.data.id];
      if (sev) cls += ` f-${sev}`;
      const dv = state.diff?.map[d.data.id];
      if (dv) cls += ` diff-${dv}`;
      if (d3.select(this).classed('selected')) cls += ' selected';
      if (d3.select(this).classed('search-hit')) cls += ' search-hit';
      return cls;
    });

  if (state.costMode) {
    g.selectAll('.node').filter(d => d.data.cost_monthly > 0)
      .append('text')
      .attr('class', 'cost-label')
      .attr('dy', d => -(getRadius(d) + 7))
      .text(d => fmtCost(d.data.cost_monthly));
  }
}

// shared node rendering: colored disc + icon + status ring + collapsed badge + label
function drawNodes(sel, { labelBelow = true } = {}) {
  sel.append('circle')
    .attr('class', 'halo')
    .attr('r', d => getRadius(d) + 4);

  sel.append('circle')
    .attr('class', 'bg-circle')
    .attr('r', d => getRadius(d))
    .attr('fill', d => hexA(typeStyle(d.data.type).color, 0.18))
    .attr('stroke', d => typeStyle(d.data.type).color);

  sel.append('text')
    .attr('class', 'node-icon')
    .style('font-size', d => `${getRadius(d) * 0.95}px`)
    .text(d => typeStyle(d.data.type).icon);

  // status dot bottom-right
  sel.filter(d => d.data.status)
    .append('circle')
    .attr('class', 'status-ring')
    .attr('cx', d => getRadius(d) * 0.72)
    .attr('cy', d => getRadius(d) * 0.72)
    .attr('r', 3.6)
    .attr('fill', d => STATUS_COLOR[d.data.status] || STATUS_COLOR.unknown)
    .attr('stroke', 'var(--node-stroke)')
    .attr('stroke-width', 1.4);

  // collapsed-children badge top-right
  const collapsed = sel.filter(d => d._children && d._children.length);
  collapsed.append('circle')
    .attr('cx', d => getRadius(d) * 0.72)
    .attr('cy', d => -getRadius(d) * 0.72)
    .attr('r', 7)
    .attr('fill', 'var(--accent)')
    .attr('stroke', 'var(--node-stroke)')
    .attr('stroke-width', 1.4);
  collapsed.append('text')
    .attr('class', 'badge-count')
    .attr('x', d => getRadius(d) * 0.72)
    .attr('y', d => -getRadius(d) * 0.72)
    .text(d => d._children.length);

  if (labelBelow) {
    sel.append('text')
      .attr('class', 'node-label')
      .attr('dy', d => getRadius(d) + 13)
      .attr('text-anchor', 'middle')
      .text(d => truncate(d.data.name, 18));
    sel.append('text')
      .attr('class', 'node-sublabel')
      .attr('dy', d => getRadius(d) + 24)
      .attr('text-anchor', 'middle')
      .text(d => d.data.type);
  }
}

function attachNodeEvents(sel) {
  sel.on('click', handleNodeClick)
     .on('dblclick', handleNodeDblClick)
     .on('contextmenu', handleContextMenu)
     .on('mouseover', showNodeTooltip)
     .on('mouseout', hideTooltip);
}

// ─── Force ("Network Map"): hierarchy edges + real relationship edges ───
// remembered positions so expand/collapse doesn't re-scramble the whole map
const posCache = new Map();

function renderForce() {
  const { width, height } = svgSize();
  const nodes = visibleNodes();
  const byId = new Map(nodes.map(d => [d.data.id, d]));

  // seed nodes at their previous position (new nodes start near their parent)
  nodes.forEach(d => {
    const prev = posCache.get(d.data.id);
    if (prev) { d.x = prev.x; d.y = prev.y; }
    else {
      const pp = d.parent && posCache.get(d.parent.data.id);
      if (pp) { d.x = pp.x + (Math.random() - 0.5) * 60; d.y = pp.y + (Math.random() - 0.5) * 60; }
    }
  });
  const anyCached = nodes.some(d => posCache.has(d.data.id));

  // hierarchy containment links between visible nodes
  const links = state.hierarchyRoot.links()
    .filter(l => l.source.data.id !== 'root')
    .map(l => ({ source: l.source, target: l.target, type: 'contains' }));

  // relationship edges from the raw graph (skip contains — hierarchy already covers it)
  state.rawData.edges.forEach(e => {
    if (e.type === 'contains') return;
    const s = byId.get(e.source), t = byId.get(e.target);
    if (s && t) links.push({ source: s, target: t, type: e.type, label: e.label });
  });

  simulation = d3.forceSimulation(nodes)
    .force('link', d3.forceLink(links).id(d => d.data.id)
      .distance(l => l.type === 'contains' ? 70 : 130)
      .strength(l => l.type === 'contains' ? 0.7 : 0.15))
    .force('charge', d3.forceManyBody().strength(-380))
    .force('center', d3.forceCenter(width / 2, height / 2))
    .force('collide', d3.forceCollide(d => getRadius(d) + 22))
    .force('x', d3.forceX(width / 2).strength(0.04))
    .force('y', d3.forceY(height / 2).strength(0.05));

  // gentle settle when we already know the layout — no explosion on each click
  if (anyCached) simulation.alpha(0.25);

  const link = g.append('g').selectAll('path').data(links)
    .enter().append('path')
    .attr('class', d => `link ${d.type === 'contains' ? 'contains' : 'rel'}`)
    .attr('stroke', d => (EDGE_STYLE[d.type] || EDGE_STYLE.default).color || 'var(--edge-color)')
    .attr('stroke-dasharray', d => (EDGE_STYLE[d.type] || EDGE_STYLE.default).dash)
    .attr('marker-end', d => d.type === 'contains' ? null : `url(#arrow-${EDGE_STYLE[d.type] ? d.type : 'default'})`);

  const node = g.append('g').selectAll('.node').data(nodes, d => d.data.id)
    .enter().append('g')
    .attr('class', 'node')
    .call(d3.drag()
      .on('start', (event, d) => { if (!event.active) simulation.alphaTarget(0.25).restart(); d.fx = d.x; d.fy = d.y; })
      .on('drag', (event, d) => { d.fx = event.x; d.fy = event.y; })
      .on('end', (event, d) => { if (!event.active) simulation.alphaTarget(0); d.fx = null; d.fy = null; }));

  drawNodes(node);
  attachNodeEvents(node);

  let fitted = false;
  simulation.on('tick', () => {
    link.attr('d', d => {
      const dx = d.target.x - d.source.x, dy = d.target.y - d.source.y;
      if (d.type === 'contains') return `M${d.source.x},${d.source.y}L${d.target.x},${d.target.y}`;
      // gentle arc for relationship edges, ending at the node edge (for the arrowhead)
      const dist = Math.hypot(dx, dy) || 1;
      const pad = getRadius(d.target) + 6;
      const tx = d.target.x - (dx / dist) * pad;
      const ty = d.target.y - (dy / dist) * pad;
      return `M${d.source.x},${d.source.y}Q${(d.source.x + tx) / 2 - dy * 0.08},${(d.source.y + ty) / 2 + dx * 0.08} ${tx},${ty}`;
    });
    node.attr('transform', d => `translate(${d.x},${d.y})`);
    nodes.forEach(d => posCache.set(d.data.id, { x: d.x, y: d.y }));
    if (simulation.alpha() < 0.06) {
      updateMinimapContent();
      if (!fitted) { fitted = true; if (!anyCached) zoomToFit(400); }
    }
  });
  // only auto-fit on a fresh layout; when navigating we keep the user's viewport
  simulation.on('end', () => { updateMinimapContent(); if (!anyCached) zoomToFit(300); });
}

// ─── Tree ───
function renderTree() {
  d3.tree().nodeSize([52, 230])(state.hierarchyRoot);

  const nodes = visibleNodes();
  const links = state.hierarchyRoot.links().filter(l => l.source.data.id !== 'root');

  g.append('g').selectAll('path').data(links)
    .enter().append('path')
    .attr('class', 'link contains')
    .attr('d', d3.linkHorizontal().x(d => d.y).y(d => d.x));

  const node = g.append('g').selectAll('.node').data(nodes, d => d.data.id)
    .enter().append('g')
    .attr('class', 'node')
    .attr('transform', d => `translate(${d.y},${d.x})`);

  drawNodes(node, { labelBelow: false });
  attachNodeEvents(node);

  node.append('text')
    .attr('class', 'node-label')
    .attr('dy', 4)
    .attr('x', d => (d.children || d._children) ? 0 : getRadius(d) + 8)
    .attr('y', d => (d.children || d._children) ? -(getRadius(d) + 8) : 0)
    .attr('text-anchor', d => (d.children || d._children) ? 'middle' : 'start')
    .text(d => truncate(d.data.name, 26));
}

// ─── Radial ───
function renderRadial() {
  const { width, height } = svgSize();
  const radius = Math.max(Math.min(width, height) / 2 - 100, 200);

  d3.tree().size([2 * Math.PI, radius])
    .separation((a, b) => (a.parent === b.parent ? 1 : 2) / a.depth)(state.hierarchyRoot);

  const nodes = visibleNodes();
  const links = state.hierarchyRoot.links().filter(l => l.source.data.id !== 'root');

  const cx = width / 2, cy = height / 2;
  const inner = g.append('g').attr('transform', `translate(${cx},${cy})`);

  // faint depth rings
  const depths = [...new Set(nodes.map(d => d.y))].sort((a, b) => a - b);
  inner.append('g').selectAll('circle').data(depths).enter().append('circle')
    .attr('r', d => d).attr('fill', 'none')
    .attr('stroke', 'var(--edge-color)').attr('stroke-opacity', 0.25).attr('stroke-dasharray', '2,5');

  inner.append('g').selectAll('path').data(links)
    .enter().append('path')
    .attr('class', 'link contains')
    .attr('d', d3.linkRadial().angle(d => d.x).radius(d => d.y));

  const node = inner.append('g').selectAll('.node').data(nodes, d => d.data.id)
    .enter().append('g')
    .attr('class', 'node')
    .attr('transform', d => `rotate(${d.x * 180 / Math.PI - 90}) translate(${d.y},0)`);

  // keep icons/badges upright
  const up = node.append('g').attr('transform', d => `rotate(${-(d.x * 180 / Math.PI - 90)})`);
  drawNodes(up, { labelBelow: false });
  attachNodeEvents(node);

  node.append('text')
    .attr('class', 'node-label')
    .attr('dy', '0.31em')
    .attr('x', d => d.x < Math.PI === !d.children ? getRadius(d) + 8 : -(getRadius(d) + 8))
    .attr('text-anchor', d => d.x < Math.PI === !d.children ? 'start' : 'end')
    .attr('transform', d => d.x >= Math.PI ? 'rotate(180)' : null)
    .text(d => truncate(d.data.name, 22));
}

// ─── Legend ───
function buildLegend(view) {
  const el = document.getElementById('legend');
  const typesPresent = [...new Set(visibleNodes().map(d => d.data.type))];

  let html = `<div class="legend-title">Resources</div>`;
  html += typesPresent.sort().slice(0, 14).map(t =>
    `<div class="legend-row"><span class="sw" style="background:${typeStyle(t).color}"></span>${t}</div>`
  ).join('');

  if (view === 'force') {
    const edgeTypes = [...new Set(state.rawData.edges.map(e => e.type))];
    if (edgeTypes.length) {
      html += `<div class="legend-sep"></div><div class="legend-title">Connections</div>`;
      html += edgeTypes.map(t => {
        const st = EDGE_STYLE[t] || EDGE_STYLE.default;
        return `<div class="legend-row"><span class="lw ${st.dash ? 'dashed' : ''}" style="border-color:${st.color || 'var(--edge-color)'}"></span>${st.label || t}</div>`;
      }).join('');
    }
  }
  el.innerHTML = html;
}

// ═══════════════════════════════════════════════════════
// Zoom / fit / minimap
// ═══════════════════════════════════════════════════════

function svgSize() {
  const r = svg.node().getBoundingClientRect();
  return { width: r.width || 900, height: r.height || 600 };
}

function zoomToFit(duration = 500) {
  const bbox = g.node().getBBox();
  if (!bbox.width || !bbox.height) return;
  const { width, height } = svgSize();
  const scale = Math.min(0.92 * width / bbox.width, 0.92 * height / bbox.height, 2);
  const tx = width / 2 - scale * (bbox.x + bbox.width / 2);
  const ty = height / 2 - scale * (bbox.y + bbox.height / 2);
  const t = d3.zoomIdentity.translate(tx, ty).scale(scale);
  (duration ? svg.transition().duration(duration) : svg).call(zoom.transform, t);
}

function initMinimap() {
  minimapSvg = d3.select('#minimap-svg');
  minimapViewport = document.getElementById('minimap-viewport');
}

function updateMinimapContent() {
  minimapSvg.selectAll('*').remove();
  if (!g.node()) return;
  const clone = g.node().cloneNode(true);
  clone.removeAttribute('transform');
  minimapSvg.node().appendChild(clone);
  const bbox = g.node().getBBox();
  if (!bbox.width || !bbox.height) return;
  const pad = 40;
  minimapSvg.attr('viewBox', `${bbox.x - pad} ${bbox.y - pad} ${bbox.width + pad * 2} ${bbox.height + pad * 2}`);
  updateMinimapViewport(d3.zoomTransform(svg.node()));
}

function updateMinimapViewport(t) {
  const bbox = g.node().getBBox();
  if (!bbox.width || !bbox.height) return;
  const pad = 40;
  const mw = 190, mh = 136;
  const worldW = bbox.width + pad * 2, worldH = bbox.height + pad * 2;
  const scale = Math.min(mw / worldW, mh / worldH);
  const offX = (mw - worldW * scale) / 2, offY = (mh - worldH * scale) / 2;

  const { width, height } = svgSize();
  const vX = (-t.x / t.k - (bbox.x - pad)) * scale + offX;
  const vY = (-t.y / t.k - (bbox.y - pad)) * scale + offY;
  minimapViewport.style.width = `${(width / t.k) * scale}px`;
  minimapViewport.style.height = `${(height / t.k) * scale}px`;
  minimapViewport.style.left = `${vX}px`;
  minimapViewport.style.top = `${vY}px`;
}

// ═══════════════════════════════════════════════════════
// Search & highlight
// ═══════════════════════════════════════════════════════

function applyGraphSearch() {
  const area = document.getElementById('graph-area');
  const q = state.searchQuery.toLowerCase();
  const active = q.length > 0 || state.statusFilter;
  area.classList.toggle('searching', !!active);
  if (!active) { g.selectAll('.node').classed('search-hit', false); return; }
  g.selectAll('.node').classed('search-hit', d => {
    const matchQ = !q || (d.data.name + ' ' + d.data.type + ' ' + d.data.id).toLowerCase().includes(q);
    const matchS = !state.statusFilter || d.data.status === state.statusFilter;
    return matchQ && matchS;
  });
}

function highlightSelection() {
  g.selectAll('.node').classed('selected', d => d.data.id === state.selectedId);
}

// ═══════════════════════════════════════════════════════
// Interactions
// ═══════════════════════════════════════════════════════

// single click: select + details + breadcrumb. Nothing else — no layout change,
// no auto-focus, so you never lose your place.
function handleNodeClick(event, d) {
  event.stopPropagation();
  const datum = d.data ? d : d3.select(this.parentNode).datum(); // radial nests a <g>
  state.selectedId = datum.data.id;
  showDetails(datum.data);
  document.querySelectorAll('.nav-item').forEach(i => i.classList.toggle('active', i.dataset.id === datum.data.id));
  highlightSelection();
  updateBreadcrumb(datum);
}

// double click: expand/collapse children
function handleNodeDblClick(event, d) {
  event.stopPropagation();
  event.preventDefault();
  const datum = d.data ? d : d3.select(this.parentNode).datum();
  if (datum.children) { datum._children = datum.children; datum.children = null; }
  else if (datum._children) { datum.children = datum._children; datum._children = null; }
  else return; // leaf — nothing to toggle
  updateGraph();
}

// breadcrumb: 🏠 › vpc › subnet › node — click any ancestor to jump back up
function updateBreadcrumb(datum) {
  const el = document.getElementById('breadcrumb');
  if (!datum) { el.innerHTML = ''; return; }
  const path = datum.ancestors().reverse(); // root … selected
  el.innerHTML = path.map((a, i) => {
    const last = i === path.length - 1;
    const label = a.data.id === 'root' ? '🏠' : `${typeStyle(a.data.type).icon} ${truncate(a.data.name, 18)}`;
    return `<span class="crumb ${last ? 'current' : ''}" data-id="${a.data.id}">${label}</span>` +
           (last ? '' : '<span class="crumb-sep">›</span>');
  }).join('');
  el.querySelectorAll('.crumb:not(.current)').forEach(c => {
    c.addEventListener('click', () => {
      if (c.dataset.id === 'root') { clearSelection(); zoomToFit(); }
      else selectResource(c.dataset.id);
    });
  });
}

function clearSelection() {
  state.selectedId = null;
  highlightSelection();
  document.getElementById('detail-panel').classList.add('hidden');
  document.querySelectorAll('.nav-item.active').forEach(i => i.classList.remove('active'));
  updateBreadcrumb(null);
}

// Escape peels ONE layer of state per press — menu → blast → focus → search →
// selection → refit. It never expands/collapses nodes, so the map stays put.
function escapeBack() {
  const menu = document.getElementById('context-menu');
  if (!menu.classList.contains('hidden')) { menu.classList.add('hidden'); return; }
  const area = document.getElementById('graph-area');
  if (area.classList.contains('blast-mode')) { clearBlast(); return; }
  if (area.classList.contains('focus-mode')) { clearFocus(); return; }
  if (state.searchQuery) {
    const s = document.getElementById('search-input');
    s.value = ''; state.searchQuery = ''; applyFilters(); s.blur();
    return;
  }
  if (state.selectedId) { clearSelection(); return; }
  zoomToFit();
}

function handleContextMenu(event, d) {
  event.preventDefault();
  state.contextNode = d.data ? d : d3.select(this.parentNode).datum();
  const menu = document.getElementById('context-menu');
  menu.style.left = `${event.clientX}px`;
  menu.style.top = `${event.clientY}px`;
  menu.classList.remove('hidden');
}

function initContextMenu() {
  const menu = document.getElementById('context-menu');
  document.addEventListener('click', () => menu.classList.add('hidden'));

  document.getElementById('menu-copy-id').addEventListener('click', () => {
    if (state.contextNode) navigator.clipboard.writeText(state.contextNode.data.id);
  });

  document.getElementById('menu-focus').addEventListener('click', () => {
    if (state.contextNode) focusNode(state.contextNode.data.id);
  });

  document.getElementById('menu-blast').addEventListener('click', () => {
    if (state.contextNode) showBlastRadius(state.contextNode.data.id);
  });

  document.getElementById('menu-expand-all').addEventListener('click', () => {
    if (!state.contextNode) return;
    (function expand(n) {
      if (n._children) { n.children = n._children; n._children = null; }
      if (n.children) n.children.forEach(expand);
    })(state.contextNode);
    updateGraph();
  });
}

function focusNode(id) {
  const area = document.getElementById('graph-area');
  area.classList.add('focus-mode');
  g.selectAll('.node').classed('focused', false);
  g.selectAll('.link').classed('focused', false);

  const neighbors = new Set([id]);
  state.rawData.edges.forEach(e => {
    if (e.source === id) neighbors.add(e.target);
    if (e.target === id) neighbors.add(e.source);
  });
  g.selectAll('.node').filter(d => neighbors.has(d.data.id)).classed('focused', true);
  g.selectAll('.link').filter(l => {
    const s = l.source.data ? l.source.data.id : l.source;
    const t = l.target.data ? l.target.data.id : l.target;
    return s === id || t === id;
  }).classed('focused', true);
}

function clearFocus() {
  document.getElementById('graph-area').classList.remove('focus-mode');
  g.selectAll('.focused').classed('focused', false);
}

// ═══════════════════════════════════════════════════════
// Blast radius
// ═══════════════════════════════════════════════════════

function showBlastRadius(originId) {
  // Impact propagation: containment/attachment flows parent→child;
  // dependency edges flow in reverse (whoever connects to a dead resource breaks).
  const adj = {};
  const addAdj = (a, b) => { (adj[a] = adj[a] || []).push(b); };
  state.rawData.edges.forEach(e => {
    if (e.type === 'contains' || e.type === 'attached_to') addAdj(e.source, e.target);
    else addAdj(e.target, e.source); // connects_to, depends_on, routes_to
  });

  const impacted = new Set([originId]);
  let frontier = [originId];
  while (frontier.length) {
    const next = [];
    frontier.forEach(id => (adj[id] || []).forEach(t => {
      if (!impacted.has(t)) { impacted.add(t); next.push(t); }
    }));
    frontier = next;
  }

  const area = document.getElementById('graph-area');
  area.classList.remove('focus-mode', 'searching');
  area.classList.add('blast-mode');
  g.selectAll('.node')
    .classed('blast', d => impacted.has(d.data.id))
    .classed('blast-origin', d => d.data.id === originId);
  g.selectAll('.link').classed('blast', l => {
    const s = l.source.data ? l.source.data.id : l.source;
    const t = l.target.data ? l.target.data.id : l.target;
    return impacted.has(s) && impacted.has(t);
  });

  const origin = state.rawData.nodes.find(n => n.id === originId);
  const toast = document.getElementById('blast-toast');
  toast.classList.remove('hidden');
  toast.innerHTML = `💥 If <b>${origin?.name || originId}</b> fails → <b>${impacted.size - 1}</b> resources impacted
    <button class="btn-tool" id="blast-close">✕</button>`;
  document.getElementById('blast-close').addEventListener('click', clearBlast);
}

function clearBlast() {
  document.getElementById('graph-area').classList.remove('blast-mode');
  document.getElementById('blast-toast').classList.add('hidden');
  g.selectAll('.blast, .blast-origin').classed('blast', false).classed('blast-origin', false);
}

// ═══════════════════════════════════════════════════════
// Time travel
// ═══════════════════════════════════════════════════════

async function loadSnapshotList() {
  try {
    const metas = await (await fetch('/api/snapshots')).json();
    const sel = document.getElementById('history-select');
    sel.innerHTML = '<option value="">Compare with…</option>' +
      (metas || []).slice(1).map(m => // skip [0] — the snapshot just taken now
        `<option value="${m.file}">${new Date(m.time).toLocaleString()} (${m.nodes} nodes)</option>`).join('');
  } catch (_) { /* no snapshots yet */ }
}

async function applyTimeTravel(file) {
  const summary = document.getElementById('history-summary');
  const clearBtn = document.getElementById('history-clear');
  if (!file) {
    state.diff = null;
    summary.classList.add('hidden');
    clearBtn.classList.add('hidden');
    updateGraph();
    return;
  }
  const d = await (await fetch(`/api/snapshots/diff?file=${encodeURIComponent(file)}`)).json();
  const map = {};
  (d.added || []).forEach(n => map[n.id] = 'added');
  (d.changed || []).forEach(c => map[c.node.id] = 'changed');
  state.diff = { map, removed: d.removed || [] };

  summary.innerHTML = `<span class="h-add">+${(d.added || []).length}</span> /
    <span class="h-chg">~${(d.changed || []).length}</span> /
    <span class="h-rem">−${(d.removed || []).length} removed</span>`;
  summary.title = (d.removed || []).map(n => `− ${n.name} (${n.type})`).join('\n') || 'nothing removed';
  summary.classList.remove('hidden');
  clearBtn.classList.remove('hidden');
  updateGraph();
}

// ═══════════════════════════════════════════════════════
// Detail panel
// ═══════════════════════════════════════════════════════

function statusBadge(status) {
  if (!status) return '';
  const cls = OK_STATUSES.has(status) ? 'ok'
    : (status === 'degraded' || status === 'pending') ? 'warn'
    : (status === 'error' || status === 'unhealthy') ? 'err' : 'muted';
  return `<span class="badge ${cls}">${status}</span>`;
}

function showDetails(data) {
  const panel = document.getElementById('detail-panel');
  panel.classList.remove('hidden');
  document.getElementById('detail-title').textContent = data.name;

  const st = typeStyle(data.type);
  let html = `
    <div class="detail-hero">
      <div class="d-icon" style="background:${hexA(st.color, 0.16)}">${st.icon}</div>
      <div>
        <div class="d-name">${data.name}</div>
        <div class="d-type">${data.type}</div>
      </div>
    </div>`;

  html += `<div class="detail-section"><h3>Properties</h3>`;
  if (data.status) html += field('Status', statusBadge(data.status));
  if (data.cost_monthly > 0) html += field('Est. cost', `<span style="color:var(--green)">${fmtCost(data.cost_monthly)}/mo</span>`);
  html += field('ID', `<code>${data.id}</code>`);
  if (data.provider) html += field('Provider', data.provider);
  if (data.region) html += field('Region', data.region);
  if (data.group) html += field('Group', data.group);
  html += `</div>`;

  const nodeFindings = state.findings.filter(f => f.node_id === data.id);
  if (nodeFindings.length) {
    html += `<div class="detail-section"><h3>⚠ Findings (${nodeFindings.length})</h3>`;
    html += nodeFindings.map(f => findingCard(f, false)).join('');
    html += `</div>`;
  }

  if (data.metadata && Object.keys(data.metadata).length) {
    html += `<div class="detail-section"><h3>Metadata</h3>`;
    for (const [k, v] of Object.entries(data.metadata)) html += field(k, v);
    html += `</div>`;
  }

  // Connections from raw edges
  const nodesById = new Map(state.rawData.nodes.map(n => [n.id, n]));
  const outs = state.rawData.edges.filter(e => e.source === data.id && e.type !== 'contains');
  const ins = state.rawData.edges.filter(e => e.target === data.id && e.type !== 'contains');
  if (outs.length || ins.length) {
    html += `<div class="detail-section"><h3>Connections (${outs.length + ins.length})</h3>`;
    const arrowOut = `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="7" y1="17" x2="17" y2="7"/><polyline points="8 7 17 7 17 16"/></svg>`;
    const arrowIn = `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="17" y1="7" x2="7" y2="17"/><polyline points="16 17 7 17 7 8"/></svg>`;
    const item = (other, rel, arrow) => {
      if (!other) return '';
      const os = typeStyle(other.type);
      return `<div class="conn-item" data-id="${other.id}">
        <span class="conn-arrow">${arrow}</span>
        <span style="font-size:14px">${os.icon}</span>
        <div class="conn-body"><div class="conn-name">${other.name}</div><div class="conn-type">${other.type}</div></div>
        <span class="conn-rel">${rel.replace(/_/g, ' ')}</span>
      </div>`;
    };
    html += outs.map(e => item(nodesById.get(e.target), e.label || e.type, arrowOut)).join('');
    html += ins.map(e => item(nodesById.get(e.source), e.label || e.type, arrowIn)).join('');
    html += `</div>`;
  }

  const content = document.getElementById('detail-content');
  content.innerHTML = html;
  content.querySelectorAll('.conn-item').forEach(el => {
    el.addEventListener('click', () => selectResource(el.dataset.id));
  });

  function field(k, v) {
    return `<div class="detail-field"><span class="detail-key">${k}</span><span class="detail-value">${v}</span></div>`;
  }
}

// ═══════════════════════════════════════════════════════
// Tooltip
// ═══════════════════════════════════════════════════════

function showNodeTooltip(event, d) {
  const datum = d.data ? d : d3.select(this.parentNode).datum();
  const tt = document.getElementById('tooltip');
  tt.classList.remove('hidden');
  const st = typeStyle(datum.data.type);
  tt.innerHTML = `
    <div class="tt-title">${st.icon} ${datum.data.name} ${statusBadge(datum.data.status)}</div>
    <div class="tt-type">${datum.data.type}${datum.data.region ? ' · ' + datum.data.region : ''}</div>
    ${datum._children ? `<div class="tt-hint">▸ ${datum._children.length} hidden — double-click to expand</div>` : ''}`;
  tt.style.left = `${Math.min(event.clientX + 16, window.innerWidth - 280)}px`;
  tt.style.top = `${event.clientY - 12}px`;
}
function hideTooltip() { document.getElementById('tooltip').classList.add('hidden'); }

// ═══════════════════════════════════════════════════════
// Global events
// ═══════════════════════════════════════════════════════

function initEventListeners() {
  document.querySelectorAll('.tab').forEach(tab => {
    tab.addEventListener('click', () => switchView(tab.dataset.view));
  });

  const search = document.getElementById('search-input');
  search.addEventListener('input', () => {
    state.searchQuery = search.value.trim();
    applyFilters();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === '/' && document.activeElement !== search) { e.preventDefault(); search.focus(); }
    if (e.key === 'Escape') escapeBack();
  });

  document.getElementById('btn-close-detail').addEventListener('click', () => {
    closeDetails();
    state.selectedId = null;
    highlightSelection();
    clearFocus();
  });

  document.getElementById('btn-home').addEventListener('click', () => {
    const menu = document.getElementById('context-menu');
    if (!menu.classList.contains('hidden')) menu.classList.add('hidden');
    clearBlast();
    closeDetails();
    clearFocus();
    const search = document.getElementById('search-input');
    search.value = ''; state.searchQuery = ''; applyFilters(); search.blur();
    posCache.clear(); // fresh layout on purpose — this is "start over"
    setAllCollapsed(true);
    state.selectedId = null;
    highlightSelection();
    updateBreadcrumb(null);
    zoomToFit();
  });

  document.getElementById('btn-zoom-in').addEventListener('click', () => svg.transition().duration(200).call(zoom.scaleBy, 1.35));
  document.getElementById('btn-zoom-out').addEventListener('click', () => svg.transition().duration(200).call(zoom.scaleBy, 0.72));
  document.getElementById('btn-zoom-fit').addEventListener('click', () => zoomToFit());
  document.getElementById('btn-expand-all').addEventListener('click', () => setAllCollapsed(false));
  document.getElementById('btn-collapse-all').addEventListener('click', () => setAllCollapsed(true));

  const costBtn = document.getElementById('btn-cost-mode');
  costBtn.addEventListener('click', () => {
    state.costMode = !state.costMode;
    costBtn.classList.toggle('active', state.costMode);
    updateGraph();
  });

  document.getElementById('btn-terraform').addEventListener('click', () => {
    window.location.href = '/api/export/terraform';
  });

  document.getElementById('history-select').addEventListener('change', e => applyTimeTravel(e.target.value));
  document.getElementById('history-clear').addEventListener('click', () => {
    document.getElementById('history-select').value = '';
    applyTimeTravel('');
  });

  const btnRefresh = document.getElementById('btn-refresh');
  btnRefresh.addEventListener('click', async () => {
    btnRefresh.classList.add('spinning');
    try { await fetch('/api/discover', { method: 'POST' }); await fetchData(); }
    finally { btnRefresh.classList.remove('spinning'); }
  });

  let resizeTimer;
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => { if (state.currentView !== 'dashboard') updateGraph(); }, 200);
  });
}

function closeDetails() {
  document.getElementById('detail-panel').classList.add('hidden');
  document.querySelectorAll('.nav-item.active').forEach(i => i.classList.remove('active'));
}

// ═══════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════

function typeStyle(type) { return TYPE_STYLE[type] || TYPE_STYLE.default; }
function getRadius(d) {
  if (d.data && (d.children || d._children)) return 19;
  return ['region', 'vpc', 'docker_daemon', 'ecs_cluster'].includes(d.data?.type) ? 19 : 13;
}
function truncate(s, len) { return s && s.length > len ? s.slice(0, len - 1) + '…' : s; }
function fmtCost(c) {
  if (c >= 1000) return `$${(c / 1000).toFixed(1)}k`;
  return c >= 100 ? `$${Math.round(c)}` : `$${c.toFixed(2).replace(/\.00$/, '')}`;
}
function hexA(hex, a) {
  const n = parseInt(hex.slice(1), 16);
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${a})`;
}
