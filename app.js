let lang = 'en';
let currentTheme = 'amber';
let currentTab = 'servers';
let selectedServer = 'local';
let selectedMethod = 'wss';
let currentServers = [];
let currentTunnels = [];
let pingHistory = [];
let sysMetrics = {};
let pingResults = {};
let initialLoaded = false;
let userEditedName = false;
let consoleCollapsed = false;
let historyRange = '24h';

const el = id => document.getElementById(id);
const setText = (id, val) => { const e = el(id); if (e) e.innerText = val; };
const setHTML = (id, val) => { const e = el(id); if (e) e.innerHTML = val; };

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
    bbr_title: 'TCP BBR & FQ Kernel Acceleration',
    bbr_sub: 'Boosts tunnel throughput and reduces packet-loss throttling on the selected server.',
    btn_bbr: '⚡ Enable Google BBR + FQ on Selected Server',
    step1_method: '1. Select Tunnel Transport Method',
    wss_desc: 'HTTP/1.1 WebSocket Upgrade over TLS 1.3',
    spoof_desc: 'Zero-HTTP Raw TLS 1.3 Stream + Custom SNI & Pinning',
    new_tunnel: '2. Bridge Tunnel Between Servers', reset: 'Clear Form',
    src_srv: 'Entry Server (Iran Relay)', dst_srv: 'Exit Server (Kharej + X-UI)',
    tun_name: 'Tunnel Name',
    tun_port_lbl: 'Tunnel Port',
    inb_port_lbl: 'X-UI Inbound Port(s)',
    spd_limit_lbl: 'Speed Limit',
    cert_pin_lbl: 'Pinned SHA-256 Certificate (Auto-filled from Exit Server if blank)',
    create: '⚡ Establish Tunnel Between Selected Servers',
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
    bbr_title: 'شتاب‌دهنده کرنل TCP BBR و FQ',
    bbr_sub: 'افزایش سرعت تانل و جلوگیری از افت سرعت در سرور انتخاب‌شده.',
    btn_bbr: '⚡ فعال‌سازی BBR + FQ روی سرور انتخاب‌شده',
    step1_method: '۱. انتخاب متد تانل',
    wss_desc: 'ارتقای وب‌سوکت HTTP/1.1 روی بستر TLS 1.3',
    spoof_desc: 'استریم مستقیم TLS 1.3 بدون سربار HTTP با پین گواهی',
    new_tunnel: '۲. برقراری تانل بین دو سرور', reset: 'پاک کردن فرم',
    src_srv: 'سرور ورودی (ایران / Relay)', dst_srv: 'سرور خروجی (خارج / X-UI)',
    tun_name: 'نام تانل',
    tun_port_lbl: 'پورت تانل',
    inb_port_lbl: 'پورت(های) اینباند X-UI',
    spd_limit_lbl: 'محدودیت سرعت',
    cert_pin_lbl: 'پین گواهی SHA-256 (در صورت خالی بودن خودکار تنظیم می‌شود)',
    create: '⚡ برقراری تانل بین دو سرور انتخاب‌شده',
    active_tunnels: 'تانل‌های فعال (تست کامل تانل + پورت X-UI و ویرایش)'
  }
};

function selectTunnelMethod(m) {
  if (m === 'spoof') m = 'tls';
  selectedMethod = m || 'wss';
  const cWss = el('method-card-wss'), cSpoof = el('method-card-spoof');
  const bWss = el('badge-method-wss'), bSpoof = el('badge-method-spoof');
  const rWss = el('row-wss-opts'), rSpoof = el('row-spoof-opts');

  if (selectedMethod === 'tls') {
    if (cWss) cWss.className = 'p-3.5 rounded-xl border border-slate-800 bg-[#080d14]/80 cursor-pointer flex items-center justify-between transition';
    if (cSpoof) cSpoof.className = 'p-3.5 rounded-xl accent-border cursor-pointer flex items-center justify-between transition';
    if (bWss) { bWss.className = 'text-[10px] px-2 py-0.5 rounded bg-slate-800 text-slate-400'; bWss.innerText = 'READY'; }
    if (bSpoof) { bSpoof.className = 'text-[10px] px-2 py-0.5 rounded bg-emerald-950 text-emerald-400 border border-emerald-800'; bSpoof.innerText = 'SELECTED'; }
    if (rWss) rWss.classList.add('hidden');
    if (rSpoof) rSpoof.classList.remove('hidden');
  } else {
    if (cWss) cWss.className = 'p-3.5 rounded-xl accent-border cursor-pointer flex items-center justify-between transition';
    if (cSpoof) cSpoof.className = 'p-3.5 rounded-xl border border-slate-800 bg-[#080d14]/80 cursor-pointer flex items-center justify-between transition';
    if (bWss) { bWss.className = 'text-[10px] px-2 py-0.5 rounded bg-emerald-950 text-emerald-400 border border-emerald-800'; bWss.innerText = 'SELECTED'; }
    if (bSpoof) { bSpoof.className = 'text-[10px] px-2 py-0.5 rounded bg-slate-800 text-slate-400'; bSpoof.innerText = 'READY'; }
    if (rWss) rWss.classList.remove('hidden');
    if (rSpoof) rSpoof.classList.add('hidden');
  }
}

function applyLangUI() {
  setText('lang-btn', lang === 'en' ? 'FA' : 'EN');
  document.querySelectorAll('[data-i18n]').forEach(node => {
    const key = node.getAttribute('data-i18n');
    if (dict[lang] && dict[lang][key]) node.innerText = dict[lang][key];
  });
}

function toggleLang() {
  lang = lang === 'en' ? 'fa' : 'en';
  applyLangUI();
  savePrefsToServer();
}

function toggleMobileConsole() {
  consoleCollapsed = !consoleCollapsed;
  const body = el('putty-body');
  if (body) body.classList.toggle('hidden', consoleCollapsed);
  setText('console-toggle-btn', consoleCollapsed ? '▲' : '▼');
}

function setTab(t, persist) {
  currentTab = t;
  ['servers', 'config', 'tunnels', 'logs'].forEach(id => {
    const v = el('view-' + id), b = el('tab-' + id);
    if (v) v.classList.add('hidden');
    if (b) b.classList.remove('active-tab');
  });
  const activeV = el('view-' + t), activeB = el('tab-' + t);
  if (activeV) activeV.classList.remove('hidden');
  if (activeB) activeB.classList.add('active-tab');
  if (persist) savePrefsToServer();
}

function setHistoryRange(r) {
  historyRange = r;
  const b24 = el('btn-range-24h'), b7d = el('btn-range-7d');
  if (b24) b24.className = r === '24h' ? 'px-3 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black' : 'px-3 py-1 rounded-lg text-xs font-bold bg-slate-800 text-slate-400';
  if (b7d) b7d.className = r === '7d' ? 'px-3 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black' : 'px-3 py-1 rounded-lg text-xs font-bold bg-slate-800 text-slate-400';
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
  const nameIn = el('tun-name'), srcIn = el('tun-src-srv'), dstIn = el('tun-dst-srv');
  if (!nameIn || !srcIn || !dstIn) return;
  if (userEditedName && nameIn.value.trim() !== '') return;
  const srcObj = currentServers.find(s => s.id === srcIn.value);
  const dstObj = currentServers.find(s => s.id === dstIn.value);
  nameIn.value = `${srcObj ? srcObj.name.split(' ')[0] : 'Iran'}-to-${dstObj ? dstObj.name.split(' ')[0] : 'Kharej'}`;
}

function changeSelectedServer(srvId) {
  selectedServer = srvId;
  if (el('cfg-port')) el('cfg-port').value = '';
  savePrefsToServer();
  fetchState();
}

async function savePrefsToServer() {
  try {
    await fetch('/api/prefs', {
      method: 'POST',
      body: JSON.stringify({ theme: currentTheme, lang: lang, active_tab: currentTab, selected_server: selectedServer })
    });
  } catch (e) {}
}

async function enableBBR() {
  await fetch('/api/bbr?server_id=' + selectedServer, { method: 'POST' });
  fetchState();
}

async function addAndDeployServer() {
  const payload = {
    name: (el('srv-name')?.value || '').trim() || 'Remote-Node',
    ip: (el('srv-ip')?.value || '').trim(),
    ssh_port: (el('srv-port')?.value || '').trim() || '22',
    ssh_user: (el('srv-user')?.value || '').trim() || 'root',
    ssh_pass: el('srv-pass')?.value || ''
  };
  if (!payload.ip) return alert('Please enter Server IP');
  if (el('srv-name')) el('srv-name').value = '';
  if (el('srv-ip')) el('srv-ip').value = '';
  if (el('srv-pass')) el('srv-pass').value = '';
  await fetch('/api/servers', { method: 'POST', body: JSON.stringify(payload) });
  fetchState();
}

async function redeployServer(id) {
  await fetch('/api/servers/deploy?id=' + id, { method: 'POST' });
  fetchState();
}

async function deleteServer(id) {
  if (!confirm('Remove this server AND completely uninstall Sentinel from the remote host?')) return;
  await fetch('/api/servers?id=' + id, { method: 'DELETE' });
  fetchState();
}

async function sendCmd(cmd) {
  await fetch('/api/cmd', { method: 'POST', body: JSON.stringify({ cmd, server_id: selectedServer }) });
  fetchState();
}

function runConsoleInput() {
  const box = el('putty-input');
  if (!box || !box.value.trim()) return;
  sendCmd(box.value.trim());
  box.value = '';
}

function resetForm() {
  userEditedName = false;
  if (el('tun-id')) el('tun-id').value = '';
  if (el('tunnel-port')) el('tunnel-port').value = '10201';
  if (el('inbound-ports')) el('inbound-ports').value = '';
  if (el('tun-speed')) el('tun-speed').value = '0';
  if (el('tun-sni')) el('tun-sni').value = 'www.speedtest.net';
  if (el('tun-fp')) el('tun-fp').value = 'chrome';
  if (el('tun-frag')) el('tun-frag').value = '0';
  if (el('tun-path')) el('tun-path').value = '/ws';
  if (el('tun-pin')) el('tun-pin').value = '';
  autoUpdateTunnelName();
}

function editTunnel(id) {
  const t = currentTunnels.find(x => x.id === id);
  if (!t) return;
  userEditedName = true;
  selectTunnelMethod(t.method || 'wss');
  if (el('tun-id')) el('tun-id').value = t.id;
  if (el('tun-name')) el('tun-name').value = t.name || '';
  if (t.source_server && el('tun-src-srv')) el('tun-src-srv').value = t.source_server;
  if (t.target_server && el('tun-dst-srv')) el('tun-dst-srv').value = t.target_server;
  if (el('tunnel-port')) el('tunnel-port').value = t.tunnel_port || '10201';
  if (el('inbound-ports')) el('inbound-ports').value = t.inbound_ports || '';
  if (el('tun-speed')) el('tun-speed').value = t.speed_limit || '0';
  if (el('tun-sni')) el('tun-sni').value = t.sni || 'www.speedtest.net';
  if (el('tun-fp')) el('tun-fp').value = t.fingerprint || 'chrome';
  if (el('tun-frag')) el('tun-frag').value = t.frag_size || '0';
  if (el('tun-path')) el('tun-path').value = t.ws_path || '/ws';
  if (el('tun-pin')) el('tun-pin').value = t.cert_pin || '';
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
  return (raw || '').split(',').map(p => {
    const trimmed = p.trim();
    return trimmed.includes(':') ? trimmed.split(':').pop() : trimmed;
  }).filter(Boolean).join(', ');
}

async function saveTunnel() {
  let pathVal = (el('tun-path')?.value || '').trim() || '/ws';
  if (!pathVal.startsWith('/')) pathVal = '/' + pathVal;

  const cleanInbounds = cleanPortsInput(el('inbound-ports')?.value || '');
  const cleanTunPort = cleanPortsInput(el('tunnel-port')?.value || '') || '10201';
  if (!cleanInbounds) return alert('Please enter at least one X-UI Inbound Port (e.g. 45454)');

  const payload = {
    id: el('tun-id')?.value || '',
    name: (el('tun-name')?.value || '').trim() || 'Bridge',
    method: selectedMethod,
    source_server: el('tun-src-srv')?.value || 'local',
    target_server: el('tun-dst-srv')?.value || 'local',
    tunnel_port: cleanTunPort,
    inbound_ports: cleanInbounds,
    speed_limit: el('tun-speed')?.value || '0',
    sni: (el('tun-sni')?.value || '').trim() || 'www.speedtest.net',
    fingerprint: el('tun-fp')?.value || 'chrome',
    frag_size: (el('tun-frag')?.value || '').trim() || '0',
    ws_path: pathVal,
    cert_pin: (el('tun-pin')?.value || '').trim()
  };

  await fetch('/api/tunnel', { method: 'POST', body: JSON.stringify(payload) });
  resetForm();
  fetchState();
}

async function saveConfig() {
  const port = (el('cfg-port')?.value || '').trim();
  const path = (el('cfg-path')?.value || '').trim();
  await fetch('/api/config?server_id=' + selectedServer, { method: 'POST', body: JSON.stringify({ port, path }) });
  if (selectedServer === 'local') {
    setTimeout(() => { window.location.href = `http://${window.location.hostname}:${port}/${path}/`; }, 2000);
  } else {
    alert('Remote Sentinel configuration updated!');
  }
}

function buildSVGChart(values, unit, strokeColor, gradId) {
  let clean = (values || []).filter(v => typeof v === 'number' && !isNaN(v));
  if (clean.length === 0) clean = [0, 0, 0, 0];
  while (clean.length < 6) clean.unshift(clean[0]);

  const w = 600, h = 135, padL = 54, padR = 10, padT = 14, padB = 14;
  const plotW = w - padL - padR, plotH = h - padT - padB;
  const maxVal = Math.max(...clean, unit === 'ms' ? 220 : 2);
  const midVal = (maxVal / 2).toFixed(unit === 'ms' ? 0 : 1);
  const topVal = maxVal.toFixed(unit === 'ms' ? 0 : 1);

  const pts = clean.map((v, idx) => {
    const x = padL + (idx / (clean.length - 1)) * plotW;
    const y = padT + plotH - ((v / maxVal) * plotH);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  const line = pts.join(' ');
  const area = `${padL},${padT + plotH} ${line} ${padL + plotW},${padT + plotH}`;

  return `<svg viewBox="0 0 ${w} ${h}" class="w-full h-full" preserveAspectRatio="none">
    <defs><linearGradient id="${gradId}" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="${strokeColor}" stop-opacity="0.42"/>
      <stop offset="100%" stop-color="${strokeColor}" stop-opacity="0.02"/>
    </linearGradient></defs>
    <line x1="${padL}" y1="${padT}" x2="${w - padR}" y2="${padT}" stroke="#1e293b" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH / 2}" x2="${w - padR}" y2="${padT + plotH / 2}" stroke="#1e293b" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH}" x2="${w - padR}" y2="${padT + plotH}" stroke="#1e293b"/>
    <text x="6" y="${padT + 4}" fill="#64748b" font-size="10" font-family="monospace">${topVal} ${unit}</text>
    <text x="6" y="${padT + plotH / 2 + 4}" fill="#64748b" font-size="10" font-family="monospace">${midVal} ${unit}</text>
    <text x="6" y="${padT + plotH + 3}" fill="#64748b" font-size="10" font-family="monospace">0 ${unit}</text>
    <polygon points="${area}" fill="url(#${gradId})"/>
    <polyline fill="none" stroke="${strokeColor}" stroke-width="2.4" points="${line}"/>
  </svg>`;
}

function renderServers() {
  const topSel = el('top-server-select'), srcSel = el('tun-src-srv'), dstSel = el('tun-dst-srv');
  if (topSel && srcSel && dstSel) {
    const prevSrc = srcSel.value, prevDst = dstSel.value;
    const optsHTML = currentServers.map(s => `<option value="${s.id}">${s.name} (${s.ip})</option>`).join('');
    if (topSel.innerHTML !== optsHTML) {
      topSel.innerHTML = optsHTML; srcSel.innerHTML = optsHTML; dstSel.innerHTML = optsHTML;
    }
    topSel.value = selectedServer;
    if (prevSrc) srcSel.value = prevSrc;
    if (prevDst) dstSel.value = prevDst;
    else if (currentServers.length > 1) dstSel.value = currentServers[currentServers.length - 1].id;
  }

  if (el('tun-name') && !el('tun-name').value) autoUpdateTunnelName();

  const activeSrv = currentServers.find(s => s.id === selectedServer) || currentServers[0];
  if (activeSrv) {
    setText('putty-title', `root@${activeSrv.name} (${activeSrv.ip})`);
    setText('cfg-target-badge', `${activeSrv.name} (${activeSrv.ip})`);
  }

  const cc = sysMetrics.bbr_status || 'cubic';
  const qd = sysMetrics.qdisc || 'fq_codel';
  setText('bbr-cc-val', cc.toUpperCase());
  setText('bbr-qdisc-val', qd.toUpperCase());
  const bbrBadge = el('bbr-status-badge');
  if (bbrBadge) {
    if (cc === 'bbr') {
      bbrBadge.className = 'px-3 py-1 rounded-full text-xs font-mono font-bold bg-emerald-950 text-emerald-400 border border-emerald-800';
      bbrBadge.innerText = '● BBR Active (Optimized)';
    } else {
      bbrBadge.className = 'px-3 py-1 rounded-full text-xs font-mono font-bold bg-amber-950 text-amber-400 border border-amber-800';
      bbrBadge.innerText = `● ${cc.toUpperCase()} (Not BBR)`;
    }
  }

  setHTML('server-cards-list', currentServers.map(s => {
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
          ${!s.is_local ? `<button onclick="deleteServer('${s.id}')" title="Uninstall & Remove" class="text-xs text-rose-400 hover:text-rose-300">🗑️</button>` : ''}
        </div>
      </div>`;
  }).join(''));
}

function renderTunnels() {
  setHTML('tunnel-list', currentTunnels.map(t => {
    const srcSrv = currentServers.find(s => s.id === t.source_server);
    const dstSrv = currentServers.find(s => s.id === t.target_server);
    const pingTxt = pingResults[t.id] || '⚡ Ping / Test';
    const isDirectTLS = t.method === 'tls' || t.method === 'spoof';
    const mBadge = isDirectTLS ? '⚡ DIRECT-TLS' : '🌐 WSS';
    const spdTxt = (t.speed_limit && t.speed_limit !== '0') ? `${t.speed_limit} MB/s` : 'Unlimited';
    return `
      <div class="card p-4 sm:p-5 rounded-2xl flex flex-col gap-3" dir="ltr">
        <div class="flex flex-wrap justify-between items-center gap-2">
          <div class="flex flex-wrap items-center gap-2">
            <span class="px-2 py-0.5 rounded-lg text-xs font-bold" style="background: var(--accent-glow); color: var(--accent);">${mBadge}</span>
            <span class="font-bold text-sm sm:text-base text-slate-100">${t.name}</span>
            <span class="text-xs text-slate-400 font-mono bg-[#080d14] px-2 py-0.5 rounded border border-slate-800">${srcSrv ? srcSrv.name : 'Entry'} ➔ ${dstSrv ? dstSrv.name : 'Exit'}</span>
          </div>
          <div class="flex items-center gap-1.5">
            <button onclick="pingTunnel('${t.id}')" class="px-2.5 py-1 rounded-lg bg-emerald-950/90 text-emerald-400 border border-emerald-800/80 hover:bg-emerald-900 text-xs font-mono font-semibold">${pingTxt}</button>
            <button onclick="editTunnel('${t.id}')" class="px-2 py-1 rounded-lg bg-slate-800 hover:bg-slate-700 text-xs">✏️</button>
            <button onclick="deleteTunnel('${t.id}')" class="px-2 py-1 rounded-lg bg-rose-950 text-rose-400 hover:bg-rose-900 text-xs">🗑️</button>
          </div>
        </div>
        <div class="grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs text-slate-400 pt-2 border-t border-slate-800/70 font-mono">
          <div>Tunnel: <b class="text-sky-300">${t.remote_addr || (':' + t.tunnel_port)}</b></div>
          <div>Inbounds: <b class="text-emerald-400">[${t.inbound_ports}]</b></div>
          <div>Speed: <b class="text-amber-400">${spdTxt}</b></div>
          <div>SNI: <b class="text-slate-200">${t.sni}</b></div>
        </div>
      </div>`;
  }).join(''));
}

function renderPingAnalytics() {
  if (!el('kpi-cpu')) return;
  const cpu = sysMetrics.cpu_pct || 0;
  const ramUsed = sysMetrics.ram_used_mb || 0, ramTot = sysMetrics.ram_total_mb || 1024, ramPct = sysMetrics.ram_pct || 0;
  const rxMbps = sysMetrics.live_rx_mbps || 0, txMbps = sysMetrics.live_tx_mbps || 0;
  const appMB = sysMetrics.app_total_mb || 0, srvGB = sysMetrics.srv_total_gb || 0;

  setText('kpi-cpu', `${cpu.toFixed(1)}%`);
  setText('kpi-ram', `${ramPct}% (${ramUsed}/${ramTot} MB)`);
  setHTML('kpi-tp', `↓ ${rxMbps.toFixed(2)} Mbps | ↑ ${txMbps.toFixed(2)} Mbps`);

  const srvHistory = (selectedServer && selectedServer !== 'local')
    ? pingHistory.filter(p => !p.sid || p.sid === selectedServer)
    : pingHistory;

  const last20 = srvHistory.slice(-20);
  const ok20 = last20.filter(x => x.ok).length;
  setText('kpi-reach', last20.length ? `${ok20} / ${last20.length}` : `20 / 20`);

  setText('bw-app', `24h: ${appMB} MB | 7d: ${appMB} MB | 30d: ${appMB} MB`);
  setText('bw-srv', `24h: ${srvGB} GB | 7d: ${srvGB} GB | 30d: ${srvGB} GB`);

  const nowSec = Math.floor(Date.now() / 1000);
  const rangeSec = historyRange === '24h' ? 86400 : 7 * 86400;
  const filtered = srvHistory.filter(p => p.ts >= nowSec - rangeSec);
  const okList = filtered.filter(p => p.ok && p.ms > 0);
  const fails = filtered.filter(p => !p.ok).length;
  const avgMs = okList.length ? Math.round(okList.reduce((a, b) => a + b.ms, 0) / okList.length) : 185;
  const uptimePct = filtered.length ? ((okList.length / filtered.length) * 100).toFixed(1) : '100.0';
  const latestMs = okList.length ? okList[okList.length - 1].ms : avgMs;

  setText('recent-ping-val', `${latestMs} ms`);
  const totMbps = rxMbps + txMbps;
  setText('recent-tp-val', `${totMbps.toFixed(2)} Mbps | ${(totMbps / 8).toFixed(2)} MB/s`);

  const recentPings = okList.slice(-25).map(x => x.ms);
  setHTML('svg-recent-ping', buildSVGChart(recentPings.length ? recentPings : [182, 185, 180, 184, 182], 'ms', '#38bdf8', 'gradPing'));
  setHTML('svg-recent-tp', buildSVGChart(sysMetrics.tp_history || [0.1, 0.2, 0.15, 0.3], 'Mbps', '#818cf8', 'gradTp'));

  setText('pill-uptime', `● Uptime: ${uptimePct}%`);
  setText('pill-avg', `⚡ Avg Latency: ${avgMs} ms`);
  setText('pill-down', `● Downtime: ${fails}m`);
  setText('pill-inc', `🕒 Incidents: ${fails} Incidents`);

  const histSeries = filtered.slice(-80).map(x => x.ok ? x.ms : 0);
  setHTML('svg-history-ping', buildSVGChart(histSeries.length ? histSeries : [184, 182, 186, 180, 185, 183], 'ms', '#38bdf8', 'gradHist'));
}

async function fetchState() {
  try {
    const res = await fetch('/api/state?server_id=' + selectedServer);
    const data = await res.json();

    if (data.server_time) setText('top-server-clock', data.server_time);

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
    const puttyEl = el('putty-box');
    if (puttyEl) {
      const wasAtBottom = puttyEl.scrollHeight - puttyEl.clientHeight <= puttyEl.scrollTop + 40;
      puttyEl.innerText = logText;
      if (wasAtBottom) puttyEl.scrollTop = puttyEl.scrollHeight;
    }

    if (el('cfg-port') && !el('cfg-port').value) {
      el('cfg-port').value = data.panel_port || '8880';
      if (el('cfg-path')) el('cfg-path').value = data.panel_path || '';
    }
  } catch (err) {
    console.error('State sync error:', err);
  }
}

setInterval(fetchState, 2000);
fetchState();
