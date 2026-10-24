let lang = 'en';
let currentTheme = 'amber';
let currentTab = 'servers';
let selectedServer = 'local';
let currentServers = [];
let currentTunnels = [];
let pingHistory = [];
let recentPingSamples = [];
let sysMetrics = {};
let pingResults = {};
let initialLoaded = false;
let userEditedName = false;
let consoleCollapsed = false;
let historyRange = '24h';

const dict = {
  en: {
    servers: 'Servers', config: 'Config', tunnels: 'Tunnels', logs: 'Log',
    add_srv_title: 'Connect Remote Server & Deploy Sentinel',
    add_srv_sub: 'Enter SSH credentials to automatically install and link the Sentinel agent.',
    srv_name: 'Server Name', srv_ip: 'Server IP Address', srv_port: 'SSH Port',
    srv_user: 'SSH Username', srv_pass: 'SSH Password',
    btn_deploy: '🚀 Connect via SSH & Deploy Sentinel (0% ➔ 100%)',
    network_settings: 'Server Port & URL Configuration', lbl_panel_port: 'Panel / Sentinel Port',
    lbl_panel_path: 'Secret URL Path', save_restart: 'Save & Restart Selected Server',
    step1_method: '1. Select Tunnel Transport Method',
    wss_desc: 'WebSocket over TLS 1.3 + Keep-Alive',
    new_tunnel: '2. Bridge Tunnel Between Servers', reset: 'Clear Form',
    src_srv: 'Entry Server (Iran Relay)', dst_srv: 'Exit Server (Kharej + X-UI)',
    tun_name: 'Tunnel Name (Auto-filled)',
    tun_port_lbl: 'Tunnel Port (WSS Highway)',
    inb_port_lbl: 'X-UI Inbound Port(s)',
    cert_pin_lbl: 'Pinned SHA-256 Certificate (Auto-filled from Exit Server if blank)',
    create: '⚡ Establish WSS Tunnel Between Selected Servers',
    active_tunnels: 'Active Tunnels (End-to-End X-UI Ping & Edit)'
  },
  fa: {
    servers: 'سرورها', config: 'تنظیمات', tunnels: 'تانل‌ها', logs: 'لاگ',
    add_srv_title: 'اتصال به سرور جدید و نصب خودکار سنتینل',
    add_srv_sub: 'مشخصات SSH را وارد کنید تا سنتینل با نمایش درصد پیشرفت نصب شود.',
    srv_name: 'نام سرور', srv_ip: 'آدرس IP سرور', srv_port: 'پورت SSH',
    srv_user: 'نام کاربری SSH', srv_pass: 'رمز عبور SSH',
    btn_deploy: '🚀 اتصال با SSH و نصب سنتینل (۰٪ تا ۱۰۰٪)',
    network_settings: 'تنظیمات پورت و آدرس سرور انتخاب‌شده', lbl_panel_port: 'پورت پنل / سنتینل',
    lbl_panel_path: 'آدرس مخفی پنل', save_restart: 'ذخیره و ری‌استارت سرور انتخاب‌شده',
    step1_method: '۱. انتخاب متد تانل',
    wss_desc: 'وب‌سوکت روی TLS 1.3 با اتصال پایدار ضد قطعی',
    new_tunnel: '۲. برقراری تانل بین دو سرور', reset: 'پاک کردن فرم',
    src_srv: 'سرور ورودی (ایران / Relay)', dst_srv: 'سرور خروجی (خارج / X-UI)',
    tun_name: 'نام تانل (پیشنهاد خودکار)',
    tun_port_lbl: 'پورت تانل (پل ارتباطی WSS)',
    inb_port_lbl: 'پورت(های) اینباند X-UI (با کاما)',
    cert_pin_lbl: 'پین گواهی SHA-256 (در صورت خالی بودن خودکار تنظیم می‌شود)',
    create: '⚡ برقراری تانل WSS بین دو سرور انتخاب‌شده',
    active_tunnels: 'تانل‌های فعال (تست کامل تانل + پورت X-UI و ویرایش)'
  }
};

function applyLangUI() {
  document.getElementById('lang-btn').innerText = lang === 'en' ? 'FA' : 'EN';
  document.querySelectorAll('[data-i18n]').forEach(el => {
    const key = el.getAttribute('data-i18n');
    if (dict[lang][key]) el.innerText = dict[lang][key];
  });
}

function toggleLang() {
  lang = lang === 'en' ? 'fa' : 'en';
  applyLangUI();
  savePrefsToServer();
}

function toggleMobileConsole() {
  consoleCollapsed = !consoleCollapsed;
  document.getElementById('putty-body').classList.toggle('hidden', consoleCollapsed);
  document.getElementById('console-toggle-btn').innerText = consoleCollapsed ? '▲' : '▼';
}

function setTab(t, persist) {
  currentTab = t;
  ['servers', 'config', 'tunnels', 'logs'].forEach(id => {
    document.getElementById('view-' + id).classList.add('hidden');
    document.getElementById('tab-' + id).classList.remove('active-tab');
  });
  document.getElementById('view-' + t).classList.remove('hidden');
  document.getElementById('tab-' + t).classList.add('active-tab');
  if (persist) savePrefsToServer();
}

function setHistoryRange(r) {
  historyRange = r;
  const b24 = document.getElementById('btn-range-24h');
  const b7d = document.getElementById('btn-range-7d');
  if (b24 && b7d) {
    b24.className = r === '24h' ? 'px-3 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black' : 'px-3 py-1 rounded-lg text-xs font-bold bg-slate-800 text-slate-400';
    b7d.className = r === '7d' ? 'px-3 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black' : 'px-3 py-1 rounded-lg text-xs font-bold bg-slate-800 text-slate-400';
  }
  renderPingAnalytics();
}

function applyTheme(name, persist) {
  currentTheme = name;
  const themes = {
    amber:   { bg: '#0b111a', card: '#111a27', border: '#1e293b', accent: '#facc15', glow: 'rgba(250, 204, 21, 0.12)' },
    cyan:    { bg: '#0c131f', card: '#131e2f', border: '#1e2e46', accent: '#38bdf8', glow: 'rgba(56, 189, 248, 0.12)' },
    emerald: { bg: '#0b1312', card: '#12201e', border: '#1a332f', accent: '#10b981', glow: 'rgba(16, 185, 129, 0.12)' },
    indigo:  { bg: '#0d1117', card: '#161b22', border: '#30363d', accent: '#818cf8', glow: 'rgba(129, 140, 248, 0.12)' },
    rose:    { bg: '#120c10', card: '#1c1318', border: '#33202a', accent: '#fb7185', glow: 'rgba(251, 113, 133, 0.12)' }
  };
  const t = themes[name] || themes.amber;
  document.documentElement.style.setProperty('--bg', t.bg);
  document.documentElement.style.setProperty('--card', t.card);
  document.documentElement.style.setProperty('--border', t.border);
  document.documentElement.style.setProperty('--accent', t.accent);
  document.documentElement.style.setProperty('--accent-glow', t.glow);
  if (persist) savePrefsToServer();
}

function autoUpdateTunnelName() {
  if (userEditedName && document.getElementById('tun-name').value.trim() !== '') return;
  const srcObj = currentServers.find(s => s.id === document.getElementById('tun-src-srv').value);
  const dstObj = currentServers.find(s => s.id === document.getElementById('tun-dst-srv').value);
  const sName = srcObj ? srcObj.name.split(' ')[0] : 'Iran';
  const dName = dstObj ? dstObj.name.split(' ')[0] : 'Kharej';
  document.getElementById('tun-name').value = `${sName}-to-${dName}`;
}

function changeSelectedServer(srvId) {
  selectedServer = srvId;
  document.getElementById('cfg-port').value = '';
  savePrefsToServer();
  fetchState();
}

async function savePrefsToServer() {
  await fetch('/api/prefs', {
    method: 'POST',
    body: JSON.stringify({ theme: currentTheme, lang: lang, active_tab: currentTab, selected_server: selectedServer })
  });
}

async function addAndDeployServer() {
  const payload = {
    name: document.getElementById('srv-name').value.trim() || 'Remote-Node',
    ip: document.getElementById('srv-ip').value.trim(),
    ssh_port: document.getElementById('srv-port').value.trim() || '22',
    ssh_user: document.getElementById('srv-user').value.trim() || 'root',
    ssh_pass: document.getElementById('srv-pass').value
  };
  if (!payload.ip) return alert('Please enter Server IP');
  document.getElementById('srv-name').value = '';
  document.getElementById('srv-ip').value = '';
  document.getElementById('srv-pass').value = '';
  await fetch('/api/servers', { method: 'POST', body: JSON.stringify(payload) });
  fetchState();
}

async function redeployServer(id) {
  await fetch('/api/servers/deploy?id=' + id, { method: 'POST' });
  fetchState();
}

async function deleteServer(id) {
  if (!confirm('Remove this server from panel?')) return;
  await fetch('/api/servers?id=' + id, { method: 'DELETE' });
  fetchState();
}

async function sendCmd(cmd) {
  await fetch('/api/cmd', { method: 'POST', body: JSON.stringify({ cmd, server_id: selectedServer }) });
  fetchState();
}

function runConsoleInput() {
  const box = document.getElementById('putty-input');
  if (!box.value.trim()) return;
  sendCmd(box.value.trim());
  box.value = '';
}

function resetForm() {
  userEditedName = false;
  document.getElementById('tun-id').value = '';
  document.getElementById('tunnel-port').value = '10201';
  document.getElementById('inbound-ports').value = '';
  document.getElementById('tun-sni').value = 'www.speedtest.net';
  document.getElementById('tun-path').value = '/ws';
  document.getElementById('tun-pin').value = '';
  autoUpdateTunnelName();
}

function editTunnel(id) {
  const t = currentTunnels.find(x => x.id === id);
  if (!t) return;
  userEditedName = true;
  document.getElementById('tun-id').value = t.id;
  document.getElementById('tun-name').value = t.name || '';
  if (t.source_server) document.getElementById('tun-src-srv').value = t.source_server;
  if (t.target_server) document.getElementById('tun-dst-srv').value = t.target_server;
  document.getElementById('tunnel-port').value = t.tunnel_port || '10201';
  document.getElementById('inbound-ports').value = t.inbound_ports || '';
  document.getElementById('tun-sni').value = t.sni || 'www.speedtest.net';
  document.getElementById('tun-path').value = t.ws_path || '/ws';
  document.getElementById('tun-pin').value = t.cert_pin || '';
}

async function deleteTunnel(id) {
  await fetch('/api/tunnel?id=' + id, { method: 'DELETE' });
  fetchState();
}

async function pingTunnel(id) {
  pingResults[id] = 'Testing...';
  renderTunnels();
  const res = await fetch('/api/ping?id=' + id);
  const data = await res.json();
  pingResults[id] = data.ok ? `✅ ${data.ms} ms (X-UI OK)` : `❌ ${data.error || 'Failed'}`;
  renderTunnels();
  fetchState();
}

function cleanPortsInput(raw) {
  return raw.split(',').map(p => {
    const trimmed = p.trim();
    return trimmed.includes(':') ? trimmed.split(':').pop() : trimmed;
  }).filter(Boolean).join(', ');
}

async function saveTunnel() {
  let pathVal = document.getElementById('tun-path').value.trim() || '/ws';
  if (!pathVal.startsWith('/')) pathVal = '/' + pathVal;

  const cleanInbounds = cleanPortsInput(document.getElementById('inbound-ports').value);
  const cleanTunPort = cleanPortsInput(document.getElementById('tunnel-port').value) || '10201';
  if (!cleanInbounds) return alert('Please enter at least one X-UI Inbound Port (e.g. 45454)');

  const payload = {
    id: document.getElementById('tun-id').value,
    name: document.getElementById('tun-name').value.trim() || 'WSS-Bridge',
    method: 'wss',
    source_server: document.getElementById('tun-src-srv').value,
    target_server: document.getElementById('tun-dst-srv').value,
    tunnel_port: cleanTunPort,
    inbound_ports: cleanInbounds,
    sni: document.getElementById('tun-sni').value.trim() || 'www.speedtest.net',
    ws_path: pathVal,
    cert_pin: document.getElementById('tun-pin').value.trim()
  };

  await fetch('/api/tunnel', { method: 'POST', body: JSON.stringify(payload) });
  resetForm();
  fetchState();
}

async function saveConfig() {
  const port = document.getElementById('cfg-port').value.trim();
  const path = document.getElementById('cfg-path').value.trim();
  await fetch('/api/config?server_id=' + selectedServer, { method: 'POST', body: JSON.stringify({ port, path }) });
  if (selectedServer === 'local') {
    setTimeout(() => { window.location.href = `http://${window.location.hostname}:${port}/${path}/`; }, 2000);
  } else {
    alert('Remote Sentinel configuration updated!');
  }
}

function buildSVGChart(values, maxVal, strokeColor, gradId) {
  if (!values || values.length === 0) values = [0, 0];
  if (values.length === 1) values = [values[0], values[0]];
  const w = 600, h = 130, pad = 8;
  const max = Math.max(maxVal || 10, ...values, 1);
  const pts = values.map((v, idx) => {
    const x = (idx / (values.length - 1)) * w;
    const y = h - pad - ((v / max) * (h - pad * 2));
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  const line = pts.join(' ');
  const area = `0,${h} ${line} ${w},${h}`;
  return `<svg viewBox="0 0 ${w} ${h}" class="w-full h-32 preserve-3d" preserveAspectRatio="none">
    <defs><linearGradient id="${gradId}" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="${strokeColor}" stop-opacity="0.38"/>
      <stop offset="100%" stop-color="${strokeColor}" stop-opacity="0.0"/>
    </linearGradient></defs>
    <polygon points="${area}" fill="url(#${gradId})"/>
    <polyline fill="none" stroke="${strokeColor}" stroke-width="2.2" points="${line}"/>
  </svg>`;
}

function renderServers() {
  const topSel = document.getElementById('top-server-select');
  const srcSel = document.getElementById('tun-src-srv');
  const dstSel = document.getElementById('tun-dst-srv');
  const prevSrc = srcSel.value, prevDst = dstSel.value;

  const optsHTML = currentServers.map(s => `<option value="${s.id}">${s.name} (${s.ip})</option>`).join('');
  if (topSel.innerHTML !== optsHTML) {
    topSel.innerHTML = optsHTML; srcSel.innerHTML = optsHTML; dstSel.innerHTML = optsHTML;
  }
  topSel.value = selectedServer;
  if (prevSrc) srcSel.value = prevSrc;
  if (prevDst) dstSel.value = prevDst;
  else if (currentServers.length > 1) dstSel.value = currentServers[currentServers.length - 1].id;

  if (!document.getElementById('tun-name').value) autoUpdateTunnelName();

  const activeSrv = currentServers.find(s => s.id === selectedServer) || currentServers[0];
  if (activeSrv) {
    document.getElementById('putty-title').innerText = `root@${activeSrv.name} (${activeSrv.ip})`;
    document.getElementById('cfg-target-badge').innerText = `${activeSrv.name} (${activeSrv.ip})`;
  }

  document.getElementById('server-cards-list').innerHTML = currentServers.map(s => {
    const isOnline = s.status === 'Online';
    const badgeClass = isOnline ? 'bg-emerald-950 text-emerald-400' : 'bg-amber-950 text-amber-400';
    const infoUrl = `http://${s.ip}:${s.panel_port || '8880'}/sentinel-info`;
    return `
      <div class="card rounded-2xl p-4 sm:p-5 flex flex-col justify-between gap-3 ${s.id === selectedServer ? 'accent-border' : ''}">
        <div class="flex justify-between items-start">
          <div>
            <div class="flex items-center gap-2">
              <h4 class="font-bold text-sm sm:text-base">${s.name}</h4>
              ${s.is_local ? `<span class="text-[10px] px-2 py-0.5 rounded font-bold" style="background: var(--accent-glow); color: var(--accent);">Master</span>` : `<span class="text-[10px] px-2 py-0.5 rounded bg-slate-800 text-slate-300">Sentinel</span>`}
            </div>
            <div class="text-xs font-mono text-slate-400 mt-1" dir="ltr">IP: <b class="text-slate-200">${s.ip}</b> | Port: <b>${s.panel_port}</b></div>
          </div>
          <span class="px-2.5 py-1 rounded-full text-xs ${badgeClass}">● ${s.status}</span>
        </div>
        <div class="text-[11px] font-mono text-slate-400 bg-[#080d14] p-2.5 rounded-xl border border-slate-800/80 truncate" dir="ltr">
          Pin: <span class="text-emerald-400">${s.cert_pin ? s.cert_pin.slice(0, 26) + '...' : 'Pending...'}</span>
        </div>
        <div class="flex flex-wrap justify-between items-center gap-2 pt-2 border-t border-slate-800/60">
          <div class="flex flex-wrap items-center gap-1.5">
            <button onclick="changeSelectedServer('${s.id}')" class="px-2.5 py-1 rounded-lg bg-slate-800 hover:bg-slate-700 text-xs font-semibold" style="color: var(--accent)">${s.id === selectedServer ? '✓ Active' : 'Select'}</button>
            <button onclick="redeployServer('${s.id}')" class="px-2.5 py-1 rounded-lg bg-sky-950 text-sky-400 border border-sky-800/70 hover:bg-sky-900 text-xs font-semibold">🔄 Deploy</button>
            <a href="${infoUrl}" target="_blank" class="px-2.5 py-1 rounded-lg bg-emerald-950 text-emerald-400 border border-emerald-800/70 hover:bg-emerald-900 text-xs font-semibold">🌐 Info</a>
          </div>
          ${!s.is_local ? `<button onclick="deleteServer('${s.id}')" class="text-xs text-rose-400 hover:text-rose-300">🗑️</button>` : ''}
        </div>
      </div>`;
  }).join('');
}

function renderTunnels() {
  document.getElementById('tunnel-list').innerHTML = currentTunnels.map(t => {
    const srcSrv = currentServers.find(s => s.id === t.source_server);
    const dstSrv = currentServers.find(s => s.id === t.target_server);
    const pingTxt = pingResults[t.id] || '⚡ Ping / Test';
    return `
      <div class="card p-4 sm:p-5 rounded-2xl flex flex-col gap-3" dir="ltr">
        <div class="flex flex-wrap justify-between items-center gap-2">
          <div class="flex flex-wrap items-center gap-2">
            <span class="px-2 py-0.5 rounded-lg text-xs font-bold" style="background: var(--accent-glow); color: var(--accent);">WSS</span>
            <span class="font-bold text-sm sm:text-base text-slate-100">${t.name}</span>
            <span class="text-xs text-slate-400 font-mono bg-[#080d14] px-2 py-0.5 rounded border border-slate-800">${srcSrv ? srcSrv.name : 'Entry'} ➔ ${dstSrv ? dstSrv.name : 'Exit'}</span>
          </div>
          <div class="flex items-center gap-1.5">
            <button onclick="pingTunnel('${t.id}')" class="px-2.5 py-1 rounded-lg bg-emerald-950/90 text-emerald-400 border border-emerald-800/80 hover:bg-emerald-900 text-xs font-mono font-semibold">${pingTxt}</button>
            <button onclick="editTunnel('${t.id}')" class="px-2 py-1 rounded-lg bg-slate-800 hover:bg-slate-700 text-xs">✏️</button>
            <button onclick="deleteTunnel('${t.id}')" class="px-2 py-1 rounded-lg bg-rose-950 text-rose-400 hover:bg-rose-900 text-xs">🗑️</button>
          </div>
        </div>
        <div class="grid grid-cols-2 sm:grid-cols-3 gap-2 text-xs text-slate-400 pt-2 border-t border-slate-800/70 font-mono">
          <div>Tunnel: <b class="text-sky-300">${t.remote_addr || (':' + t.tunnel_port)}</b></div>
          <div>Inbounds: <b class="text-emerald-400">[${t.inbound_ports}]</b></div>
          <div>SNI: <b class="text-slate-200">${t.sni}</b></div>
        </div>
      </div>`;
  }).join('');
}

function renderPingAnalytics() {
  const el = id => document.getElementById(id);
  if (!el('kpi-cpu')) return;

  const cpu = sysMetrics.cpu_pct || 0;
  const ramUsed = sysMetrics.ram_used_mb || 0, ramTot = sysMetrics.ram_total_mb || 1024, ramPct = sysMetrics.ram_pct || 0;
  const rxMbps = sysMetrics.live_rx_mbps || 0, txMbps = sysMetrics.live_tx_mbps || 0;
  const appMB = sysMetrics.app_total_mb || 0, srvGB = sysMetrics.srv_total_gb || 0;

  el('kpi-cpu').innerText = `${cpu.toFixed(1)}%`;
  el('kpi-ram').innerText = `${ramPct}% (${ramUsed}/${ramTot} MB)`;
  el('kpi-tp').innerHTML = `↓ ${rxMbps.toFixed(2)} Mbps | ↑ ${txMbps.toFixed(2)} Mbps`;

  const last20 = pingHistory.slice(-20);
  const ok20 = last20.filter(x => x.ok).length;
  el('kpi-reach').innerText = last20.length ? `${ok20} / ${last20.length}` : `${currentServers.length} / ${currentServers.length}`;

  el('bw-app').innerText = `24h: ${appMB} MB | 7d: ${appMB} MB | 30d: ${appMB} MB`;
  el('bw-srv').innerText = `24h: ${srvGB} GB | 7d: ${srvGB} GB | 30d: ${srvGB} GB`;

  const nowSec = Math.floor(Date.now() / 1000);
  const rangeSec = historyRange === '24h' ? 86400 : 7 * 86400;
  const filtered = pingHistory.filter(p => p.ts >= nowSec - rangeSec);
  const okList = filtered.filter(p => p.ok && p.ms > 0);
  const fails = filtered.filter(p => !p.ok).length;
  const avgMs = okList.length ? Math.round(okList.reduce((a, b) => a + b.ms, 0) / okList.length) : 185;
  const uptimePct = filtered.length ? ((okList.length / filtered.length) * 100).toFixed(1) : '100.0';
  const latestMs = okList.length ? okList[okList.length - 1].ms : avgMs;

  el('recent-ping-val').innerText = `${latestMs} ms`;
  const totMbps = rxMbps + txMbps;
  el('recent-tp-val').innerText = `${totMbps.toFixed(2)} Mbps | ${(totMbps / 8).toFixed(2)} MB/s`;

  const recentPings = okList.slice(-30).map(x => x.ms);
  el('svg-recent-ping').innerHTML = buildSVGChart(recentPings.length ? recentPings : [avgMs, avgMs], 350, '#38bdf8', 'gradPing');
  el('svg-recent-tp').innerHTML = buildSVGChart(sysMetrics.tp_history || [0.2, 0.4], 15, '#818cf8', 'gradTp');

  el('pill-uptime').innerText = `● Uptime: ${uptimePct}%`;
  el('pill-avg').innerText = `⚡ Avg Latency: ${avgMs} ms`;
  el('pill-down').innerText = `● Downtime: ${fails}m`;
  el('pill-inc').innerText = `🕒 Incidents: ${fails} Incidents`;

  const histSeries = filtered.slice(-80).map(x => x.ok ? x.ms : 0);
  el('svg-history-ping').innerHTML = buildSVGChart(histSeries.length ? histSeries : [avgMs, avgMs], 600, '#38bdf8', 'gradHist');
}

async function fetchState() {
  const res = await fetch('/api/state?server_id=' + selectedServer);
  const data = await res.json();

  if (data.server_time) document.getElementById('top-server-clock').innerText = data.server_time;

  if (!initialLoaded && data.prefs) {
    initialLoaded = true;
    lang = data.prefs.lang || 'en';
    selectedServer = data.prefs.selected_server || 'local';
    applyLangUI();
    applyTheme(data.prefs.theme || 'amber', false);
    setTab(data.prefs.active_tab || 'servers', false);
  }

  currentServers = data.servers || [];
  currentTunnels = data.tunnels || [];
  pingHistory = data.ping_history || [];
  sysMetrics = data.metrics || {};

  renderServers();
  renderTunnels();
  renderPingAnalytics();

  const logText = (data.logs || []).join('\n');
  const puttyEl = document.getElementById('putty-box');
  const wasAtBottom = puttyEl.scrollHeight - puttyEl.clientHeight <= puttyEl.scrollTop + 40;
  puttyEl.innerText = logText;
  if (wasAtBottom) puttyEl.scrollTop = puttyEl.scrollHeight;

  if (!document.getElementById('cfg-port').value) {
    document.getElementById('cfg-port').value = data.panel_port || '8880';
    document.getElementById('cfg-path').value = data.panel_path || '';
  }
}

setInterval(fetchState, 2000);
fetchState();
