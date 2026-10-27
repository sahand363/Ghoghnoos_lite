let lang = 'en';
let currentTheme = 'cyan';
let currentTab = 'servers';
let selectedServer = 'local';
let selectedMethod = 'wss';
let deployMode = 'ssh';
let enrollToken = '';
let masterIP = '';
let ctrlL4 = '6005';
let ctrlDirect = '6006';
let ctrlWSS = '6007';
let ctrlMethods = 'wss,direct';
let currentServers = [];
let currentTunnels = [];
let pingSummary = null;
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

async function triggerSelfUpdate() {
  const btn = el('btn-self-update');
  if (btn) {
    btn.className = 'px-2.5 py-1 rounded-lg bg-amber-500 text-black text-xs font-bold animate-pulse';
    btn.innerHTML = '⏳ Updating...';
  }
  try {
    await fetch('/api/update?server_id=' + selectedServer, { method: 'POST' });
  } catch (e) {}
  setTimeout(() => { window.location.reload(); }, 4500);
}

function setDeployMode(mode) {
  deployMode = mode;
  const bSsh = el('btn-mode-ssh'), bEnr = el('btn-mode-enroll');
  const boxSsh = el('box-deploy-ssh'), boxEnr = el('box-deploy-enroll');
  if (mode === 'enroll') {
    if (bSsh) bSsh.className = 'px-2.5 py-1 rounded-lg text-xs font-bold text-slate-400 hover:text-white transition';
    if (bEnr) bEnr.className = 'px-2.5 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black transition';
    boxSsh?.classList.add('hidden');
    boxEnr?.classList.remove('hidden');
    syncDefaultServerNames();
  } else {
    if (bSsh) bSsh.className = 'px-2.5 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black transition';
    if (bEnr) bEnr.className = 'px-2.5 py-1 rounded-lg text-xs font-bold text-slate-400 hover:text-white transition';
    boxSsh?.classList.remove('hidden');
    boxEnr?.classList.add('hidden');
  }
}

function updateEnrollCommandUI() {
  const addrIn = el('enroll-master-addr');
  const defaultSpec = `${masterIP || window.location.hostname}:${ctrlL4}:${ctrlDirect}:${ctrlWSS}`;
  if (addrIn && !addrIn.value && masterIP) addrIn.value = defaultSpec;
  const targetSpec = (addrIn?.value || defaultSpec).trim();
  const nodeName = (el('enroll-node-name')?.value || getNextDefaultServerName()).trim().replace(/\s+/g, '-');
  const cmd = `curl -Ls https://raw.githubusercontent.com/sahand363/ghoghnoos_light/main/install.sh | bash -s -- --enroll ${targetSpec} ${enrollToken} ${nodeName}`;
  setText('enroll-cmd-box', cmd);
}

function copyTextSilent(text) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }
  return new Promise((resolve) => {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    ta.style.top = '0';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    try { document.execCommand('copy'); } catch (e) {}
    document.body.removeChild(ta);
    resolve();
  });
}

async function copyEnrollCommand() {
  const cmd = el('enroll-cmd-box')?.innerText || '';
  if (!cmd) return;
  await copyTextSilent(cmd);
  const btn = el('btn-copy-enroll');
  if (btn) {
    const oldText = btn.innerText;
    const oldBg = btn.style.background;
    btn.style.background = '#10b981';
    btn.innerText = lang === 'fa' ? '✅ کپی شد! در ترمینال سرور خارج پیست کنید' : '✅ Copied to Clipboard!';
    setTimeout(() => {
      btn.style.background = oldBg || 'var(--accent)';
      btn.innerText = oldText;
    }, 2500);
  }
}

async function rotateEnrollToken() {
  const res = await fetch('/api/enroll/token', { method: 'POST' });
  const data = await res.json();
  if (data.enroll_token) { enrollToken = data.enroll_token; updateEnrollCommandUI(); }
}

async function startRendezvousFromBox() {
  const ip = (el('rv-target-ip')?.value || '').trim();
  if (!ip) return alert('Please enter the Remote Sentinel IP Address');
  const name = (el('enroll-node-name')?.value || getNextDefaultServerName()).trim();
  if (el('rv-target-ip')) el('rv-target-ip').value = '';
  userEditedEnrollName = false;
  await fetch('/api/servers/probe', { method: 'POST', body: JSON.stringify({ ip, name, stop: false }) });
  fetchState();
}

async function toggleServerProbe(id, stop) {
  await fetch('/api/servers/probe', { method: 'POST', body: JSON.stringify({ id, stop }) });
  fetchState();
}

async function setServerReverseMode(id, action) {
  await fetch('/api/servers/probe', { method: 'POST', body: JSON.stringify({ id, action }) });
  fetchState();
}

function stepSpeed(delta) {
  const inp = el('tun-speed');
  if (!inp) return;
  inp.value = Math.max(0, Math.round(((parseFloat(inp.value) || 0) + delta) * 10) / 10);
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
  if (persist) {
    savePrefsToServer();
    if (t === 'logs') fetchState();
  }
}

function setHistoryRange(r) {
  historyRange = r;
  if (el('btn-range-24h')) el('btn-range-24h').className = r === '24h' ? 'px-3 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black' : 'px-3 py-1 rounded-lg text-xs font-bold bg-slate-800 text-slate-400';
  if (el('btn-range-7d')) el('btn-range-7d').className = r === '7d' ? 'px-3 py-1 rounded-lg text-xs font-bold bg-sky-500 text-black' : 'px-3 py-1 rounded-lg text-xs font-bold bg-slate-800 text-slate-400';
  fetchState();
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
  const ctrl_l4 = (el('cfg-ctrl-l4')?.value || '').trim() || '6005';
  const ctrl_direct = (el('cfg-ctrl-direct')?.value || '').trim() || '6006';
  const ctrl_wss = (el('cfg-ctrl-wss')?.value || '').trim() || '6007';
  const methods = [];
  if (el('chk-ctrl-wss')?.checked) methods.push('wss');
  if (el('chk-ctrl-direct')?.checked) methods.push('direct');
  if (el('chk-ctrl-l4')?.checked) methods.push('forward');
  if (methods.length === 0) return alert('Please keep at least one Control Transport method checked!');
  const ctrl_methods = methods.join(',');

  await fetch('/api/config?server_id=' + selectedServer, {
    method: 'POST',
    body: JSON.stringify({ port, path, user, pass, ctrl_l4, ctrl_direct, ctrl_wss, ctrl_methods })
  });
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

function fmtShortTime(ts) {
  if (!ts) return '--:--';
  const d = new Date(ts * 1000);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

function buildHistorySVGChart(points) {
  let list = (points && points.length) ? points.slice(-90) : [];
  const nowSec = Math.floor(Date.now() / 1000);
  while (list.length < 6) list.unshift({ ts: nowSec - (6 - list.length) * 600, ms: 50, ok: true });
  const w = 680, h = 155, padL = 62, padR = 14, padT = 14, padB = 22, plotW = w - padL - padR, plotH = h - padT - padB;
  const vals = list.map(p => (p.ok && p.ms > 0) ? p.ms : 0);
  const maxVal = Math.max(...vals, 220), midVal = Math.round(maxVal / 2), topVal = Math.round(maxVal);
  const stepW = plotW / Math.max(list.length - 1, 1);

  let redBars = '';
  list.forEach((p, idx) => {
    if (!p.ok) {
      const cx = padL + idx * stepW, bw = Math.max(stepW * 1.15, 7), bx = Math.max(padL, cx - bw / 2);
      redBars += `<rect x="${bx.toFixed(1)}" y="${padT}" width="${bw.toFixed(1)}" height="${plotH}" fill="rgba(244,63,94,0.28)"/>`;
      redBars += `<line x1="${cx.toFixed(1)}" y1="${padT}" x2="${cx.toFixed(1)}" y2="${padT + plotH}" stroke="#f43f5e" stroke-width="1.3" stroke-opacity="0.75"/>`;
    }
  });

  const pts = vals.map((v, idx) => `${(padL + idx * stepW).toFixed(1)},${(padT + plotH - ((v / maxVal) * plotH)).toFixed(1)}`).join(' ');
  const area = `${padL},${padT + plotH} ${pts} ${padL + plotW},${padT + plotH}`;
  const tStart = fmtShortTime(list[0].ts), tMid = fmtShortTime(list[Math.floor(list.length / 2)].ts), tEnd = fmtShortTime(list[list.length - 1].ts);

  return `<svg viewBox="0 0 ${w} ${h}" class="w-full h-full" preserveAspectRatio="none">
    <defs><linearGradient id="gradHist" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="#38bdf8" stop-opacity="0.40"/><stop offset="100%" stop-color="#38bdf8" stop-opacity="0.02"/></linearGradient></defs>
    <line x1="${padL}" y1="${padT}" x2="${w - padR}" y2="${padT}" stroke="#1e293b" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH / 2}" x2="${w - padR}" y2="${padT + plotH / 2}" stroke="#1e293b" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH}" x2="${w - padR}" y2="${padT + plotH}" stroke="#1e293b"/>
    ${redBars}
    <text x="6" y="${padT + 4}" fill="#94a3b8" font-size="10" font-family="monospace">${topVal} ms</text>
    <text x="6" y="${padT + plotH / 2 + 4}" fill="#94a3b8" font-size="10" font-family="monospace">${midVal} ms</text>
    <text x="6" y="${padT + plotH + 3}" fill="#f43f5e" font-size="10" font-weight="bold" font-family="monospace">0 (OFF)</text>
    <polygon points="${area}" fill="url(#gradHist)"/><polyline fill="none" stroke="#38bdf8" stroke-width="2.2" points="${pts}"/>
    <text x="${padL}" y="${h - 4}" fill="#94a3b8" font-size="10" font-family="monospace">${tStart}</text>
    <text x="${padL + plotW / 2 - 14}" y="${h - 4}" fill="#94a3b8" font-size="10" font-family="monospace">${tMid}</text>
    <text x="${w - padR - 32}" y="${h - 4}" fill="#94a3b8" font-size="10" font-family="monospace">${tEnd}</text></svg>`;
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
    const isOnline = s.status === 'Online', badgeClass = isOnline ? 'bg-emerald-950 text-emerald-400' : 'bg-rose-950 text-rose-400';
    const viaBadge = (!s.is_local && s.via_method) ? `<span class="text-[10px] px-2 py-0.5 rounded bg-sky-950 text-sky-400 border border-sky-800/70 font-mono">🔗 VIA ${s.via_method}</span>` : '';
    const probeBtn = !s.is_local
      ? (s.probing
          ? `<button onclick="toggleServerProbe('${s.id}', true)" class="px-2.5 py-1 rounded-lg bg-amber-950 text-amber-400 border border-amber-800/70 hover:bg-amber-900 text-xs font-semibold animate-pulse">⏸ Probing...</button>`
          : `<button onclick="toggleServerProbe('${s.id}', false)" class="px-2.5 py-1 rounded-lg bg-emerald-950 text-emerald-400 border border-emerald-800/70 hover:bg-emerald-900 text-xs font-semibold" title="Start Master Forward Rendezvous Probe">▶ Probe</button>`)
      : '';
    const revBtn = !s.is_local
      ? (s.reverse_paused
          ? `<button onclick="setServerReverseMode('${s.id}', 'resume')" class="px-2.5 py-1 rounded-lg bg-indigo-950 text-indigo-300 border border-indigo-800/70 hover:bg-indigo-900 text-xs font-semibold" title="Resume Sentinel Reverse Dial-In">🔗 Resume Rev</button>`
          : `<button onclick="setServerReverseMode('${s.id}', 'disconnect')" class="px-2.5 py-1 rounded-lg bg-rose-950 text-rose-300 border border-rose-800/70 hover:bg-rose-900 text-xs font-semibold" title="Disconnect & Pause Sentinel Reverse Dial-In (Keep Listening)">🔌 Disconnect</button>`)
      : '';
    return `<div class="card rounded-2xl p-4 sm:p-5 flex flex-col justify-between gap-3 ${s.id === selectedServer ? 'accent-border' : ''}">
      <div class="flex justify-between items-start"><div><div class="flex flex-wrap items-center gap-1.5"><h4 class="font-bold text-sm sm:text-base">${s.name}</h4>
      ${s.is_local ? `<span class="text-[10px] px-2 py-0.5 rounded font-bold" style="background: var(--accent-glow); color: var(--accent);">Master</span>` : `<span class="text-[10px] px-2 py-0.5 rounded bg-slate-800 text-slate-300">Sentinel</span>`}${viaBadge}</div>
      <div class="text-xs font-mono text-slate-400 mt-1" dir="ltr">IP: <b class="text-slate-200">${s.ip}</b> | Port: <b>${s.panel_port || '6001'}</b></div></div>
      <span class="px-2.5 py-1 rounded-full text-xs ${badgeClass}">● ${s.status}</span></div>
      <div class="text-[11px] font-mono text-slate-400 bg-[#080d14] p-2.5 rounded-xl border border-slate-800/80 truncate" dir="ltr">Pin: <span class="text-emerald-400">${s.cert_pin ? s.cert_pin.slice(0, 26) + '...' : 'Pending...'}</span></div>
      <div class="flex flex-wrap justify-between items-center gap-2 pt-2 border-t border-slate-800/60"><div class="flex flex-wrap items-center gap-1.5">
      <button onclick="changeSelectedServer('${s.id}')" class="px-2.5 py-1 rounded-lg bg-slate-800 hover:bg-slate-700 text-xs font-semibold" style="color: var(--accent)">${s.id === selectedServer ? '✓ Active' : 'Select'}</button>
      ${probeBtn}
      ${revBtn}
      <button onclick="redeployServer('${s.id}')" class="px-2.5 py-1 rounded-lg bg-sky-950 text-sky-400 border border-sky-800/70 hover:bg-sky-900 text-xs font-semibold">🔄 SSH</button>
      <a href="http://${s.ip}:${s.panel_port || '6001'}/sentinel-info" target="_blank" class="px-2.5 py-1 rounded-lg bg-slate-800 text-slate-300 hover:bg-slate-700 text-xs font-semibold">🌐 Info</a></div>
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
  if (!el('kpi-cpu') || currentTab !== 'logs') return;
  const cpu = sysMetrics.cpu_pct || 0, ramUsed = sysMetrics.ram_used_mb || 0, ramTot = sysMetrics.ram_total_mb || 1024, ramPct = sysMetrics.ram_pct || 0;
  const rxMbps = sysMetrics.live_rx_mbps || 0, txMbps = sysMetrics.live_tx_mbps || 0, appMB = sysMetrics.app_total_mb || 0, srvGB = sysMetrics.srv_total_gb || 0;
  setText('kpi-cpu', `${cpu.toFixed(1)}%`); setText('kpi-ram', `${ramPct}% (${ramUsed}/${ramTot} MB)`); setHTML('kpi-tp', `↓ ${rxMbps.toFixed(2)} Mbps | ↑ ${txMbps.toFixed(2)} Mbps`);
  setText('bw-app', `24h: ${appMB} MB | 7d: ${appMB} MB | 30d: ${appMB} MB`); setText('bw-srv', `24h: ${srvGB} GB | 7d: ${srvGB} GB | 30d: ${srvGB} GB`);
  const totMbps = rxMbps + txMbps;
  setText('recent-tp-val', `${totMbps.toFixed(2)} Mbps | ${(totMbps / 8).toFixed(2)} MB/s`);
  setHTML('svg-recent-tp', buildSVGChart(sysMetrics.tp_history || [0.1, 0.2, 0.15, 0.3], 'Mbps', '#818cf8', 'gradTp'));

  if (!pingSummary) return;
  setText('kpi-reach', pingSummary.reach_str || '20 / 20');
  setText('recent-ping-val', `${pingSummary.latest_ms || 50} ms`);
  const recentPings = pingSummary.recent_pings || [];
  setHTML('svg-recent-ping', buildSVGChart(recentPings.length ? recentPings : [52, 50, 48, 51, 50], 'ms', '#38bdf8', 'gradPing'));
  const fails = pingSummary.fails || 0;
  setText('pill-uptime', `● Online Uptime: ${pingSummary.uptime_pct || '100.0'}%`);
  setText('pill-down', `● Total Offline Time: ${fails * 10}m (${fails} checks)`);
  setText('pill-avg', `⚡ Avg Ping: ${pingSummary.avg_ms || 50} ms`);
  const wins = pingSummary.windows || [];
  setText('pill-outage-windows', wins.length ? `🕒 Outage Windows: ${wins.join(' | ')}` : `🕒 Outage Windows: None — 100% Continuous Uptime`);
  setHTML('svg-history-ping', buildHistorySVGChart(pingSummary.chart_points || []));
}

async function fetchState() {
  try {
    const res = await fetch(`/api/state?server_id=${selectedServer}&tab=${currentTab}&range=${historyRange}`);
    if (res.status === 401) { window.location.href = 'login'; return; }
    const data = await res.json();
    if (data.version) setText('app-version-badge', data.version);
    const updBtn = el('btn-self-update');
    if (updBtn && !updBtn.innerText.includes('Updating')) {
      if (data.update_available) {
        updBtn.className = 'px-2.5 py-1 rounded-lg bg-amber-500 text-black text-xs font-bold animate-pulse flex items-center gap-1';
        updBtn.innerHTML = '<span>🔥</span><span class="hidden sm:inline">Update Ready!</span>';
      } else {
        updBtn.className = 'px-2.5 py-1 rounded-lg bg-emerald-950 text-emerald-400 border border-emerald-800/80 text-xs font-bold hover:bg-emerald-900 transition flex items-center gap-1';
        updBtn.innerHTML = '<span>⬆️</span><span class="hidden sm:inline">Update</span>';
      }
    }
    if (data.server_time) setText('top-server-clock', data.server_time);
    if (data.ctrl_l4_port) ctrlL4 = data.ctrl_l4_port;
    if (data.ctrl_direct_port) ctrlDirect = data.ctrl_direct_port;
    if (data.ctrl_wss_port) ctrlWSS = data.ctrl_wss_port;
    if (data.ctrl_methods) ctrlMethods = data.ctrl_methods;
    if (data.enroll_token && data.enroll_token !== enrollToken) {
      enrollToken = data.enroll_token;
      masterIP = data.master_ip || window.location.hostname;
      updateEnrollCommandUI();
    }
    if (!initialLoaded && data.prefs) {
      initialLoaded = true; lang = data.prefs.lang || 'en'; selectedServer = data.prefs.selected_server || 'local';
      applyLangUI(); applyTheme(data.prefs.theme || 'cyan', false); setTab(data.prefs.active_tab || 'servers', false);
    }
    currentServers = data.servers || []; currentTunnels = data.tunnels || [];
    if (data.ping_summary) pingSummary = data.ping_summary;
    sysMetrics = data.metrics || {};
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
      if (el('cfg-ctrl-l4')) el('cfg-ctrl-l4').value = ctrlL4;
      if (el('cfg-ctrl-direct')) el('cfg-ctrl-direct').value = ctrlDirect;
      if (el('cfg-ctrl-wss')) el('cfg-ctrl-wss').value = ctrlWSS;
      const mSet = (ctrlMethods || 'wss,direct').split(',');
      if (el('chk-ctrl-wss')) el('chk-ctrl-wss').checked = mSet.includes('wss');
      if (el('chk-ctrl-direct')) el('chk-ctrl-direct').checked = mSet.includes('direct');
      if (el('chk-ctrl-l4')) el('chk-ctrl-l4').checked = mSet.includes('forward');
    }
  } catch (err) { console.error('State sync error:', err); }
}

updatePollRateUI();
fetchState();
restartPolling();
