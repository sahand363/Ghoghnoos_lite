let lang = 'en';
let currentTheme = 'cyan';
let currentTab = 'servers';
let selectedServer = 'local';
let selectedMethod = 'wss';
let deployMode = 'ssh';
let enrollToken = '';
let masterIP = '';
let dialPort = '6001';
let currentServers = [];
let currentTunnels = [];
let pingHistory = [];
let sysMetrics = {};
let pingResults = {};
let initialLoaded = false;
let userEditedName = false;
let userEditedSrvName = false;
let userEditedEnrollName = false;
let consoleCollapsed = false;
let historyRange = '24h';
let pollIntervalMs = 10000;
let pollTimer = null;

const el = id => document.getElementById(id);
const setText = (id, val) => { const e = el(id); if (e) e.innerText = val; };
const setHTML = (id, val) => { const e = el(id); if (e) e.innerHTML = val; };

function getNextDefaultServerName() {
  const nextNum = (currentServers && currentServers.length ? currentServers.length : 1) + 1;
  return `Server-${nextNum}`;
}

function syncDefaultServerNames() {
  const nextName = getNextDefaultServerName();
  const srvIn = el('srv-name');
  if (srvIn && !userEditedSrvName) srvIn.placeholder = nextName;
  const enrIn = el('enroll-node-name');
  if (enrIn && !userEditedEnrollName) enrIn.value = nextName;
  updateEnrollCommandUI();
}

function togglePollRate() {
  pollIntervalMs = pollIntervalMs === 10000 ? 300000 : 10000;
  updatePollRateUI();
  restartPolling();
}

function updatePollRateUI() {
  const btn = el('btn-poll-rate');
  if (!btn) return;
  if (pollIntervalMs === 10000) {
    btn.className = 'px-2 py-0.5 rounded text-[10px] font-mono font-bold bg-sky-950 text-sky-400 border border-sky-800/80 hover:scale-105 transition';
    btn.innerText = '⚡ 10s Live';
  } else {
    btn.className = 'px-2 py-0.5 rounded text-[10px] font-mono font-bold bg-amber-950 text-amber-400 border border-amber-800/80 hover:scale-105 transition';
    btn.innerText = '🐢 5m Eco';
  }
}

function restartPolling() {
  if (pollTimer) clearInterval(pollTimer);
  pollTimer = setInterval(() => { if (!document.hidden) fetchState(); }, pollIntervalMs);
}

document.addEventListener('visibilitychange', () => { if (!document.hidden) fetchState(); });

function downloadLogs24h() {
  window.location.href = `/api/logs/download?server_id=${selectedServer}`;
}

function setDeployMode(mode) {
  deployMode = mode;
  const bSsh = el('btn-mode-ssh'), bEnr = el('btn-mode-enroll');
  const boxSsh = el('box-deploy-ssh'), boxEnr = el('box-deploy-enroll');
  if (mode === 'enroll') {
    if (bSsh) bSsh.className = 'px-2.5 py-1 rounded-lg text-xs font-bold text-slate-400 hover:text-white transition';
    if (bEnr) bEnr.className = 'px-2.5 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black transition';
    if (boxSsh) boxSsh.classList.add('hidden');
    if (boxEnr) boxEnr.classList.remove('hidden');
    syncDefaultServerNames();
  } else {
    if (bSsh) bSsh.className = 'px-2.5 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black transition';
    if (bEnr) bEnr.className = 'px-2.5 py-1 rounded-lg text-xs font-bold text-slate-400 hover:text-white transition';
    if (boxSsh) boxSsh.classList.remove('hidden');
    if (boxEnr) boxEnr.classList.add('hidden');
  }
}

function updateEnrollCommandUI() {
  const addrIn = el('enroll-master-addr');
  if (addrIn && !addrIn.value && masterIP) addrIn.value = `${masterIP}:${dialPort || '6001'}`;
  const targetAddr = (addrIn?.value || `${masterIP || window.location.hostname}:${dialPort || '6001'}`).trim();
  const defaultName = getNextDefaultServerName();
  const nodeName = (el('enroll-node-name')?.value || defaultName).trim().replace(/\s+/g, '-');
  const cmd = `curl -Ls https://raw.githubusercontent.com/sahand363/ghoghnoos_light/main/install.sh | bash -s -- --enroll ${targetAddr} ${enrollToken} ${nodeName}`;
  setText('enroll-cmd-box', cmd);
}

async function copyEnrollCommand() {
  const cmd = el('enroll-cmd-box')?.innerText || '';
  if (!cmd) return;
  try {
    await navigator.clipboard.writeText(cmd);
    const btn = el('btn-copy-enroll');
    if (btn) {
      const old = btn.innerText;
      btn.innerText = lang === 'fa' ? '✅ کپی شد! در ترمینال سرور خارج اجرا کنید' : '✅ Copied! Paste in Remote VPS Terminal';
      setTimeout(() => { btn.innerText = old; }, 2500);
    }
  } catch (e) { prompt('Copy command:', cmd); }
}

async function rotateEnrollToken() {
  const res = await fetch('/api/enroll/token', { method: 'POST' });
  const data = await res.json();
  if (data.enroll_token) { enrollToken = data.enroll_token; updateEnrollCommandUI(); }
}

function stepSpeed(delta) {
  const inp = el('tun-speed');
  if (!inp) return;
  let cur = parseFloat(inp.value) || 0;
  inp.value = Math.max(0, Math.round((cur + delta) * 10) / 10);
  updateSpeedBadge();
}

function updateSpeedBadge() {
  const val = parseFloat(el('tun-speed')?.value) || 0;
  setText('spd-badge', val <= 0 ? (lang === 'fa' ? 'نامحدود' : 'Unlimited') : `${val} MB/s (${(val * 8).toFixed(0)} Mbps)`);
}

function selectTunnelMethod(m) {
  if (m === 'spoof' || m === 'tls') m = 'direct';
  selectedMethod = m || 'wss';
  ['wss', 'direct', 'forward'].forEach(k => {
    const card = el('method-card-' + k), badge = el('badge-method-' + k);
    if (k === selectedMethod) {
      if (card) card.className = 'p-3.5 rounded-xl accent-border cursor-pointer flex flex-col justify-between gap-2 transition';
      if (badge) { badge.className = 'text-[10px] px-2 py-0.5 rounded bg-emerald-950 text-emerald-400 border border-emerald-800'; badge.innerText = 'SELECTED'; }
    } else {
      if (card) card.className = 'p-3.5 rounded-xl border border-slate-800 bg-[#080d14]/80 cursor-pointer flex flex-col justify-between gap-2 transition hover:border-slate-700';
      if (badge) { badge.className = 'text-[10px] px-2 py-0.5 rounded bg-slate-800 text-slate-400'; badge.innerText = 'READY'; }
    }
  });
  const rSniWrap = el('row-tls-sni-wrap'), rWsPath = el('row-ws-path'), rDirect = el('row-direct-opts'), rForward = el('row-forward-opts'), rPin = el('row-cert-pin');
  if (selectedMethod === 'forward') {
    rSniWrap?.classList.add('hidden'); rDirect?.classList.add('hidden'); rForward?.classList.remove('hidden'); rPin?.classList.add('hidden');
  } else if (selectedMethod === 'direct') {
    rSniWrap?.classList.remove('hidden'); rWsPath?.classList.add('hidden'); rDirect?.classList.remove('hidden'); rForward?.classList.add('hidden'); rPin?.classList.remove('hidden');
  } else {
    rSniWrap?.classList.remove('hidden'); rWsPath?.classList.remove('hidden'); rDirect?.classList.add('hidden'); rForward?.classList.add('hidden'); rPin?.classList.remove('hidden');
  }
}

function toggleMobileConsole() {
  consoleCollapsed = !consoleCollapsed;
  el('putty-body')?.classList.toggle('hidden', consoleCollapsed);
  setText('console-toggle-btn', consoleCollapsed ? '▲' : '▼');
}

function setTab(t, persist) {
  currentTab = t;
  ['servers', 'config', 'tunnels', 'logs'].forEach(id => {
    el('view-' + id)?.classList.add('hidden');
    el('tab-' + id)?.classList.remove('active-tab');
  });
  el('view-' + t)?.classList.remove('hidden');
  el('tab-' + t)?.classList.add('active-tab');
  if (persist) savePrefsToServer();
}

function setHistoryRange(r) {
  historyRange = r;
  if (el('btn-range-24h')) el('btn-range-24h').className = r === '24h' ? 'px-3 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black' : 'px-3 py-1 rounded-lg text-xs font-bold bg-slate-800 text-slate-400';
  if (el('btn-range-7d')) el('btn-range-7d').className = r === '7d' ? 'px-3 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black' : 'px-3 py-1 rounded-lg text-xs font-bold bg-slate-800 text-slate-400';
  renderPingAnalytics();
}

function autoUpdateTunnelName() {
  const nameIn = el('tun-name'), srcIn = el('tun-src-srv'), dstIn = el('tun-dst-srv');
  if (!nameIn || !srcIn || !dstIn || (userEditedName && nameIn.value.trim() !== '')) return;
  const srcObj = currentServers.find(s => s.id === srcIn.value), dstObj = currentServers.find(s => s.id === dstIn.value);
  nameIn.value = `${srcObj ? srcObj.name.split(' ')[0] : 'Relay'}-to-${dstObj ? dstObj.name.split(' ')[0] : 'Exit'}`;
}

function changeSelectedServer(srvId) {
  selectedServer = srvId;
  if (el('cfg-port')) el('cfg-port').value = '';
  savePrefsToServer();
  fetchState();
}

async function savePrefsToServer() {
  try { await fetch('/api/prefs', { method: 'POST', body: JSON.stringify({ theme: currentTheme, lang, active_tab: currentTab, selected_server: selectedServer }) }); } catch (e) {}
}

async function enableBBR() { await fetch('/api/bbr?server_id=' + selectedServer, { method: 'POST' }); fetchState(); }

async function addAndDeployServer() {
  const payload = {
    name: (el('srv-name')?.value || '').trim() || getNextDefaultServerName(),
    ip: (el('srv-ip')?.value || '').trim(),
    ssh_port: (el('srv-port')?.value || '').trim() || '22',
    ssh_user: (el('srv-user')?.value || '').trim() || 'root',
    ssh_pass: el('srv-pass')?.value || ''
  };
  if (!payload.ip) return alert('Please enter Server IP');
  userEditedSrvName = false;
  if (el('srv-name')) el('srv-name').value = '';
  if (el('srv-ip')) el('srv-ip').value = '';
  if (el('srv-pass')) el('srv-pass').value = '';
  await fetch('/api/servers', { method: 'POST', body: JSON.stringify(payload) });
  fetchState();
}

async function redeployServer(id) { await fetch('/api/servers/deploy?id=' + id, { method: 'POST' }); fetchState(); }

async function deleteServer(id) {
  if (!confirm('Remove this server AND completely uninstall Sentinel from the remote host?')) return;
  await fetch('/api/servers?id=' + id, { method: 'DELETE' });
  fetchState();
}

async function sendCmd(cmd) {
  await fetch('/api/cmd', { method: 'POST', body: JSON.stringify({ cmd, server_id: selectedServer }) });
  setTimeout(fetchState, 350);
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
  updateSpeedBadge();
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
  updateSpeedBadge();
  if (el('tun-sni')) el('tun-sni').value = t.sni || 'www.speedtest.net';
  if (el('tun-fp')) el('tun-fp').value = t.fingerprint || 'chrome';
  if (el('tun-frag')) el('tun-frag').value = t.frag_size || '0';
  if (el('tun-path')) el('tun-path').value = t.ws_path || '/ws';
  if (el('tun-pin')) el('tun-pin').value = t.cert_pin || '';
}

async function deleteTunnel(id) { await fetch('/api/tunnel?id=' + id, { method: 'DELETE' }); fetchState(); }

async function pingTunnel(id) {
  pingResults[id] = 'Testing...';
  renderTunnels();
  const res = await fetch('/api/ping?id=' + id);
  const data = await res.json();
  pingResults[id] = data.ok ? `✅ ${data.ms} ms (OK)` : `❌ ${data.error || 'Failed'}`;
  renderTunnels();
  fetchState();
}

function cleanPortsInput(raw) {
  return (raw || '').split(',').map(p => { const tr = p.trim(); return tr.includes(':') ? tr.split(':').pop() : tr; }).filter(Boolean).join(', ');
}

async function saveTunnel() {
  let pathVal = (el('tun-path')?.value || '').trim() || '/ws';
  if (!pathVal.startsWith('/')) pathVal = '/' + pathVal;
  const cleanInbounds = cleanPortsInput(el('inbound-ports')?.value || '');
  if (!cleanInbounds) return alert('Please enter at least one Inbound Port (e.g. 45454)');
  const payload = {
    id: el('tun-id')?.value || '', name: (el('tun-name')?.value || '').trim() || 'Bridge', method: selectedMethod,
    source_server: el('tun-src-srv')?.value || 'local', target_server: el('tun-dst-srv')?.value || 'local',
    tunnel_port: cleanPortsInput(el('tunnel-port')?.value || '') || '10201', inbound_ports: cleanInbounds,
    speed_limit: String(parseFloat(el('tun-speed')?.value) || 0), sni: (el('tun-sni')?.value || '').trim() || 'www.speedtest.net',
    fingerprint: el('tun-fp')?.value || 'chrome', frag_size: (el('tun-frag')?.value || '').trim() || '0',
    ws_path: pathVal, cert_pin: (el('tun-pin')?.value || '').trim()
  };
  await fetch('/api/tunnel', { method: 'POST', body: JSON.stringify(payload) });
  resetForm();
  fetchState();
}

async function saveConfig() {
  const port = (el('cfg-port')?.value || '').trim() || '6001', path = (el('cfg-path')?.value || '').trim();
  const user = (el('cfg-user')?.value || '').trim(), pass = (el('cfg-pass')?.value || '').trim();
  await fetch('/api/config?server_id=' + selectedServer, { method: 'POST', body: JSON.stringify({ port, path, user, pass }) });
  if (selectedServer === 'local') setTimeout(() => { window.location.href = `http://${window.location.hostname}:${port}/${path}/`; }, 2000);
  else alert('Remote Sentinel configuration updated!');
}

function buildSVGChart(values, unit, strokeColor, gradId) {
  let clean = (values || []).filter(v => typeof v === 'number' && !isNaN(v));
  if (clean.length === 0) clean = [0, 0, 0, 0];
  while (clean.length < 6) clean.unshift(clean[0]);
  const w = 600, h = 135, padL = 54, padR = 10, padT = 14, padB = 14, plotW = w - padL - padR, plotH = h - padT - padB;
  const maxVal = Math.max(...clean, unit === 'ms' ? 220 : 2), midVal = (maxVal / 2).toFixed(unit === 'ms' ? 0 : 1), topVal = maxVal.toFixed(unit === 'ms' ? 0 : 1);
  const pts = clean.map((v, idx) => `${(padL + (idx / (clean.length - 1)) * plotW).toFixed(1)},${(padT + plotH - ((v / maxVal) * plotH)).toFixed(1)}`).join(' ');
  const area = `${padL},${padT + plotH} ${pts} ${padL + plotW},${padT + plotH}`;
  return `<svg viewBox="0 0 ${w} ${h}" class="w-full h-full" preserveAspectRatio="none">
    <defs><linearGradient id="${gradId}" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="${strokeColor}" stop-opacity="0.42"/><stop offset="100%" stop-color="${strokeColor}" stop-opacity="0.02"/></linearGradient></defs>
    <line x1="${padL}" y1="${padT}" x2="${w - padR}" y2="${padT}" stroke="#1e293b" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH / 2}" x2="${w - padR}" y2="${padT + plotH / 2}" stroke="#1e293b" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH}" x2="${w - padR}" y2="${padT + plotH}" stroke="#1e293b"/>
    <text x="6" y="${padT + 4}" fill="#64748b" font-size="10" font-family="monospace">${topVal} ${unit}</text>
    <text x="6" y="${padT + plotH / 2 + 4}" fill="#64748b" font-size="10" font-family="monospace">${midVal} ${unit}</text>
    <text x="6" y="${padT + plotH + 3}" fill="#64748b" font-size="10" font-family="monospace">0 ${unit}</text>
    <polygon points="${area}" fill="url(#${gradId})"/><polyline fill="none" stroke="${strokeColor}" stroke-width="2.4" points="${pts}"/></svg>`;
}

function renderServers() {
  const topSel = el('top-server-select'), srcSel = el('tun-src-srv'), dstSel = el('tun-dst-srv');
  if (topSel && srcSel && dstSel) {
    const prevSrc = srcSel.value, prevDst = dstSel.value;
    const optsHTML = currentServers.map(s => `<option value="${s.id}">${s.name} (${s.ip})</option>`).join('');
    if (topSel.innerHTML !== optsHTML) { topSel.innerHTML = optsHTML; srcSel.innerHTML = optsHTML; dstSel.innerHTML = optsHTML; }
    topSel.value = selectedServer;
    if (prevSrc) srcSel.value = prevSrc;
    if (prevDst) dstSel.value = prevDst;
    else if (currentServers.length > 1) dstSel.value = currentServers[currentServers.length - 1].id;
  }
  syncDefaultServerNames();
  if (el('tun-name') && !el('tun-name').value) autoUpdateTunnelName();
  const activeSrv = currentServers.find(s => s.id === selectedServer) || currentServers[0];
  if (activeSrv) {
    setText('putty-title', `root@${activeSrv.name} (${activeSrv.ip})${activeSrv.via_method ? ` [via ${activeSrv.via_method}]` : ''}`);
    setText('cfg-target-badge', `${activeSrv.name} (${activeSrv.ip})`);
  }
  const cc = sysMetrics.bbr_status || 'cubic', qd = sysMetrics.qdisc || 'fq_codel';
  setText('bbr-cc-val', cc.toUpperCase()); setText('bbr-qdisc-val', qd.toUpperCase());
  const bbrBadge = el('bbr-status-badge');
  if (bbrBadge) {
    bbrBadge.className = cc === 'bbr' ? 'px-3 py-1 rounded-full text-xs font-mono font-bold bg-emerald-950 text-emerald-400 border border-emerald-800' : 'px-3 py-1 rounded-full text-xs font-mono font-bold bg-amber-950 text-amber-400 border border-amber-800';
    bbrBadge.innerText = cc === 'bbr' ? '● BBR Active (Optimized)' : `● ${cc.toUpperCase()} (Not BBR)`;
  }
  setHTML('server-cards-list', currentServers.map(s => {
    const isOnline = s.status === 'Online', badgeClass = isOnline ? 'bg-emerald-950 text-emerald-400' : 'bg-amber-950 text-amber-400';
    const viaBadge = (!s.is_local && s.via_method) ? `<span class="text-[10px] px-2 py-0.5 rounded bg-sky-950 text-sky-400 border border-sky-800/70 font-mono">🔗 VIA ${s.via_method}</span>` : '';
    return `<div class="card rounded-2xl p-4 sm:p-5 flex flex-col justify-between gap-3 ${s.id === selectedServer ? 'accent-border' : ''}">
      <div class="flex justify-between items-start"><div><div class="flex flex-wrap items-center gap-1.5"><h4 class="font-bold text-sm sm:text-base">${s.name}</h4>
      ${s.is_local ? `<span class="text-[10px] px-2 py-0.5 rounded font-bold" style="background: var(--accent-glow); color: var(--accent);">Master</span>` : `<span class="text-[10px] px-2 py-0.5 rounded bg-slate-800 text-slate-300">Sentinel</span>`}${viaBadge}</div>
      <div class="text-xs font-mono text-slate-400 mt-1" dir="ltr">IP: <b class="text-slate-200">${s.ip}</b> | Port: <b>${s.panel_port || '6001'}</b></div></div>
      <span class="px-2.5 py-1 rounded-full text-xs ${badgeClass}">● ${s.status}</span></div>
      <div class="text-[11px] font-mono text-slate-400 bg-[#080d14] p-2.5 rounded-xl border border-slate-800/80 truncate" dir="ltr">Pin: <span class="text-emerald-400">${s.cert_pin ? s.cert_pin.slice(0, 26) + '...' : 'Pending...'}</span></div>
      <div class="flex flex-wrap justify-between items-center gap-2 pt-2 border-t border-slate-800/60"><div class="flex flex-wrap items-center gap-1.5">
      <button onclick="changeSelectedServer('${s.id}')" class="px-2.5 py-1 rounded-lg bg-slate-800 hover:bg-slate-700 text-xs font-semibold" style="color: var(--accent)">${s.id === selectedServer ? '✓ Active' : 'Select'}</button>
      <button onclick="redeployServer('${s.id}')" class="px-2.5 py-1 rounded-lg bg-sky-950 text-sky-400 border border-sky-800/70 hover:bg-sky-900 text-xs font-semibold">🔄 Deploy</button>
      <a href="http://${s.ip}:${s.panel_port || '6001'}/sentinel-info" target="_blank" class="px-2.5 py-1 rounded-lg bg-emerald-950 text-emerald-400 border border-emerald-800/70 hover:bg-emerald-900 text-xs font-semibold">🌐 Info</a></div>
      ${!s.is_local ? `<button onclick="deleteServer('${s.id}')" title="Uninstall & Remove" class="text-xs text-rose-400 hover:text-rose-300">🗑️</button>` : ''}</div></div>`;
  }).join(''));
}

function renderTunnels() {
  setHTML('tunnel-list', currentTunnels.map(t => {
    const srcSrv = currentServers.find(s => s.id === t.source_server), dstSrv = currentServers.find(s => s.id === t.target_server);
    let mBadge = '🌐 WSS', stealthBadge = '🛡️ 85%';
    if (t.method === 'direct' || t.method === 'spoof') { mBadge = '⚡ DIRECT-TLS'; stealthBadge = '🛡️ 75%'; }
    if (t.method === 'forward') { mBadge = '🔄 FORWARD'; stealthBadge = '🛡️ 20%'; }
    const spdTxt = (t.speed_limit && parseFloat(t.speed_limit) > 0) ? `${t.speed_limit} MB/s` : 'Unlimited';
    return `<div class="card p-4 sm:p-5 rounded-2xl flex flex-col gap-3" dir="ltr"><div class="flex flex-wrap justify-between items-center gap-2"><div class="flex flex-wrap items-center gap-2">
      <span class="px-2 py-0.5 rounded-lg text-xs font-bold" style="background: var(--accent-glow); color: var(--accent);">${mBadge}</span>
      <span class="px-2 py-0.5 rounded-lg text-[11px] font-mono bg-slate-800/90 text-slate-300 border border-slate-700">${stealthBadge}</span>
      <span class="font-bold text-sm sm:text-base text-slate-100">${t.name}</span>
      <span class="text-xs text-slate-400 font-mono bg-[#080d14] px-2 py-0.5 rounded border border-slate-800">${srcSrv ? srcSrv.name : 'Relay'} ➔ ${dstSrv ? dstSrv.name : 'Exit'}</span></div>
      <div class="flex items-center gap-1.5"><button onclick="pingTunnel('${t.id}')" class="px-2.5 py-1 rounded-lg bg-emerald-950/90 text-emerald-400 border border-emerald-800/80 hover:bg-emerald-900 text-xs font-mono font-semibold">${pingResults[t.id] || '⚡ Ping / Test'}</button>
      <button onclick="editTunnel('${t.id}')" class="px-2.5 py-1 rounded-lg bg-slate-800 hover:bg-slate-700 text-xs">✏️</button>
      <button onclick="deleteTunnel('${t.id}')" class="px-2.5 py-1 rounded-lg bg-rose-950 text-rose-400 hover:bg-rose-900 text-xs">🗑️</button></div></div>
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs text-slate-400 pt-2 border-t border-slate-800/70 font-mono">
      <div>Tunnel: <b class="text-sky-300">${t.remote_addr || (':' + t.tunnel_port)}</b></div><div>Inbounds: <b class="text-emerald-400">[${t.inbound_ports}]</b></div>
      <div>Speed: <b class="text-amber-400">${spdTxt}</b></div><div>SNI: <b class="text-slate-200">${t.method === 'forward' ? 'L4 Pass' : t.sni}</b></div></div></div>`;
  }).join(''));
}

function renderPingAnalytics() {
  if (!el('kpi-cpu')) return;
  const cpu = sysMetrics.cpu_pct || 0, ramUsed = sysMetrics.ram_used_mb || 0, ramTot = sysMetrics.ram_total_mb || 1024, ramPct = sysMetrics.ram_pct || 0;
  const rxMbps = sysMetrics.live_rx_mbps || 0, txMbps = sysMetrics.live_tx_mbps || 0, appMB = sysMetrics.app_total_mb || 0, srvGB = sysMetrics.srv_total_gb || 0;
  setText('kpi-cpu', `${cpu.toFixed(1)}%`); setText('kpi-ram', `${ramPct}% (${ramUsed}/${ramTot} MB)`); setHTML('kpi-tp', `↓ ${rxMbps.toFixed(2)} Mbps | ↑ ${txMbps.toFixed(2)} Mbps`);
  const srvHistory = (selectedServer && selectedServer !== 'local') ? pingHistory.filter(p => !p.sid || p.sid === selectedServer) : pingHistory;
  const last20 = srvHistory.slice(-20);
  setText('kpi-reach', last20.length ? `${last20.filter(x => x.ok).length} / ${last20.length}` : `20 / 20`);
  setText('bw-app', `24h: ${appMB} MB | 7d: ${appMB} MB | 30d: ${appMB} MB`); setText('bw-srv', `24h: ${srvGB} GB | 7d: ${srvGB} GB | 30d: ${srvGB} GB`);
  const nowSec = Math.floor(Date.now() / 1000), rangeSec = historyRange === '24h' ? 86400 : 7 * 86400;
  const filtered = srvHistory.filter(p => p.ts >= nowSec - rangeSec), okList = filtered.filter(p => p.ok && p.ms > 0), fails = filtered.filter(p => !p.ok).length;
  const avgMs = okList.length ? Math.round(okList.reduce((a, b) => a + b.ms, 0) / okList.length) : 50;
  const uptimePct = filtered.length ? ((okList.length / filtered.length) * 100).toFixed(1) : '100.0', latestMs = okList.length ? okList[okList.length - 1].ms : avgMs;
  setText('recent-ping-val', `${latestMs} ms`);
  const totMbps = rxMbps + txMbps;
  setText('recent-tp-val', `${totMbps.toFixed(2)} Mbps | ${(totMbps / 8).toFixed(2)} MB/s`);
  const recentPings = okList.slice(-25).map(x => x.ms);
  setHTML('svg-recent-ping', buildSVGChart(recentPings.length ? recentPings : [52, 50, 48, 51, 50], 'ms', '#38bdf8', 'gradPing'));
  setHTML('svg-recent-tp', buildSVGChart(sysMetrics.tp_history || [0.1, 0.2, 0.15, 0.3], 'Mbps', '#818cf8', 'gradTp'));
  setText('pill-uptime', `● Uptime: ${uptimePct}%`); setText('pill-avg', `⚡ Avg Latency: ${avgMs} ms`);
  setText('pill-down', `● Downtime: ${fails * 5}m`); setText('pill-inc', `🕒 Incidents: ${fails} Incidents`);
  const histSeries = filtered.slice(-80).map(x => x.ok ? x.ms : 0);
  setHTML('svg-history-ping', buildSVGChart(histSeries.length ? histSeries : [50, 52, 49, 53, 51, 50], 'ms', '#38bdf8', 'gradHist'));
}

async function fetchState() {
  try {
    const res = await fetch('/api/state?server_id=' + selectedServer);
    const data = await res.json();
    if (data.server_time) setText('top-server-clock', data.server_time);
    if (data.enroll_token && data.enroll_token !== enrollToken) {
      enrollToken = data.enroll_token; masterIP = data.master_ip || window.location.hostname;
      dialPort = data.dial_port || data.panel_port || '6001'; updateEnrollCommandUI();
    }
    if (!initialLoaded && data.prefs) {
      initialLoaded = true; lang = data.prefs.lang || 'en'; selectedServer = data.prefs.selected_server || 'local';
      applyLangUI(); applyTheme(data.prefs.theme || 'cyan', false); setTab(data.prefs.active_tab || 'servers', false);
    }
    currentServers = data.servers || []; currentTunnels = data.tunnels || [];
    pingHistory = data.ping_history || []; sysMetrics = data.metrics || {};
    renderServers(); renderTunnels(); renderPingAnalytics();
    const puttyEl = el('putty-box');
    if (puttyEl) {
      const wasAtBottom = puttyEl.scrollHeight - puttyEl.clientHeight <= puttyEl.scrollTop + 40;
      puttyEl.innerText = (data.logs || []).join('\n');
      if (wasAtBottom) puttyEl.scrollTop = puttyEl.scrollHeight;
    }
    if (el('cfg-port') && !el('cfg-port').value) {
      el('cfg-port').value = data.panel_port || '6001';
      if (el('cfg-path')) el('cfg-path').value = data.panel_path || '';
      if (el('cfg-user') && data.panel_user) el('cfg-user').value = data.panel_user;
    }
  } catch (err) { console.error('State sync error:', err); }
}

updatePollRateUI();
fetchState();
restartPolling();
