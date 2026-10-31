let lang = 'fa';
let currentTheme = 'cyan';
let currentTab = 'servers';
let selectedServer = 'local';
let selectedMethod = 'wss';
let enrollToken = '';
let masterIP = '';
let ctrlL4 = '6005';
let ctrlDirect = '6006';
let ctrlWSS = '6007';
let ctrlGRPC = '6008';
let ctrlMethods = 'grpc,wss,direct';
let currentServers = [];
let currentTunnels = [];
let pingSummary = null;
let sysMetrics = {};
let pingResults = {};
let initialLoaded = false;
let userEditedName = false;
let userEditedEnrollName = false;
let consoleCollapsed = false;
let historyRange = '24h';
let usageRange = '7d';
let pollIntervalMs = 10000;
let pollTimer = null;
let serverClockSec = null;

const el = id => document.getElementById(id);
const setText = (id, val) => { const e = el(id); if (e) e.innerText = val; };
const setHTML = (id, val) => { const e = el(id); if (e) e.innerHTML = val; };

function syncServerClockFromStr(timeStr) {
  if (!timeStr) return;
  const parts = timeStr.trim().split(/\s+/);
  const hms = (parts[0] || '').split(':').map(Number);
  if (hms.length === 3 && !isNaN(hms[0]) && !isNaN(hms[1]) && !isNaN(hms[2])) {
    serverClockSec = hms[0] * 3600 + hms[1] * 60 + hms[2];
    renderDigitalClock();
  }
  if (parts[1]) setText('clock-tz-label', parts[1]);
}

function renderDigitalClock() {
  if (serverClockSec === null) return;
  const h = String(Math.floor(serverClockSec / 3600) % 24).padStart(2, '0');
  const m = String(Math.floor((serverClockSec % 3600) / 60)).padStart(2, '0');
  const s = String(serverClockSec % 60).padStart(2, '0');
  setText('top-server-clock', `${h}:${m}:${s}`);
}

setInterval(() => {
  if (serverClockSec !== null) {
    serverClockSec = (serverClockSec + 1) % 86400;
    renderDigitalClock();
  }
}, 1000);

function getNextDefaultServerName() {
  const nextNum = (currentServers && currentServers.length ? currentServers.length : 1) + 1;
  return `Server-${nextNum}`;
}

function syncDefaultServerNames() {
  const nextName = getNextDefaultServerName();
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
    btn.className = 'px-2 py-0.5 rounded text-[10px] font-mono font-bold bg-[#131d31] text-sky-400 border border-[#1f2e4a]';
    btn.innerText = '⚡ 10s';
  } else {
    btn.className = 'px-2 py-0.5 rounded text-[10px] font-mono font-bold bg-amber-950 text-amber-400 border border-amber-800/70';
    btn.innerText = '🐢 5m';
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

async function triggerSelfUpdate(targetId) {
  const srvId = targetId || selectedServer;
  const btn = el(targetId ? `btn-upd-${targetId}` : 'btn-self-update');
  if (btn) {
    btn.className = 'icon-action-btn px-2.5 py-1.5 bg-amber-500 text-black text-xs font-bold animate-pulse';
    btn.innerHTML = '⏳';
  }
  try {
    await fetch('/api/update?server_id=' + srvId, { method: 'POST' });
  } catch (e) {}
  setTimeout(() => { if (srvId === 'local') window.location.reload(); else fetchState(); }, 4500);
}

function updateEnrollCommandUI() {
  const addrIn = el('enroll-master-addr');
  const defaultSpec = `${masterIP || window.location.hostname}:${ctrlL4}:${ctrlDirect}:${ctrlWSS}:${ctrlGRPC}`;
  if (addrIn && !addrIn.value && masterIP) addrIn.value = defaultSpec;
  const targetSpec = (addrIn?.value || defaultSpec).trim();
  const nodeName = (el('enroll-node-name')?.value || getNextDefaultServerName()).trim().replace(/\s+/g, '-');
  const cmd = `curl -Ls https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh | bash -s -- --enroll ${targetSpec} ${enrollToken} ${nodeName}`;
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

async function rotateEnrollToken() {
  try {
    const res = await fetch('/api/enroll/token', { method: 'POST' });
    const data = await res.json();
    if (data.enroll_token) {
      enrollToken = data.enroll_token;
      updateEnrollCommandUI();
    }
  } catch (e) {}
}

async function copyEnrollCommand() {
  const cmd = el('enroll-cmd-box')?.innerText || '';
  if (!cmd) return;
  await copyTextSilent(cmd);
  await rotateEnrollToken();
  const btn = el('btn-copy-enroll');
  if (btn) {
    btn.style.background = '#10b981';
    btn.innerText = lang === 'fa' ? '✓ کپی شد و توکن تغییر کرد' : '✓ Copied & Token Rotated';
    setTimeout(() => {
      btn.style.background = '';
      const map = (typeof dict !== 'undefined' && (dict[lang] || dict.fa)) ? (dict[lang] || dict.fa) : null;
      btn.innerText = map && map.btn_copy_enroll ? map.btn_copy_enroll : '+ Copy One-Liner';
    }, 2200);
  }
}

async function startRendezvousFromBox() {
  const ip = (el('rv-target-ip')?.value || '').trim();
  if (!ip) return alert(lang === 'fa' ? 'لطفاً آدرس IP سرور دیده‌بان را وارد کنید' : 'Enter the Remote Agent IP Address');
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

function stepSpeed(delta) {
  const inp = el('tun-speed');
  if (!inp) return;
  inp.value = Math.max(0, Math.round(((parseFloat(inp.value) || 0) + delta) * 10) / 10);
  updateSpeedBadge();
}

function updateSpeedBadge() {
  const val = parseFloat(el('tun-speed')?.value) || 0;
  setText('spd-badge', val <= 0 ? (lang === 'fa' ? 'نامحدود' : 'Unlimited') : `${val} MB/s`);
}

function selectTunnelMethod(m) {
  if (m === 'spoof' || m === 'tls') m = 'direct';
  selectedMethod = m || 'wss';
  ['wss', 'direct', 'forward', 'grpc'].forEach(k => {
    const card = el('method-card-' + k);
    if (!card) return;
    if (k === selectedMethod) {
      card.className = 'p-3 rounded-xl accent-border cursor-pointer flex flex-col justify-between gap-2 transition';
    } else {
      card.className = 'p-3 rounded-xl border border-[#182235] bg-[#070b12] cursor-pointer flex flex-col justify-between gap-2 transition hover:border-slate-700';
    }
  });
  const rSniWrap = el('row-tls-sni-wrap');
  const rWsPath = el('row-ws-path');
  const rDirect = el('row-direct-opts');
  const rPin = el('row-cert-pin');

  if (selectedMethod === 'forward') {
    rSniWrap?.classList.add('hidden');
    rWsPath?.classList.add('hidden');
    rDirect?.classList.add('hidden');
    rPin?.classList.add('hidden');
  } else if (selectedMethod === 'direct') {
    rSniWrap?.classList.remove('hidden');
    rWsPath?.classList.add('hidden');
    rDirect?.classList.remove('hidden');
    rPin?.classList.remove('hidden');
  } else if (selectedMethod === 'grpc') {
    rSniWrap?.classList.remove('hidden');
    rWsPath?.classList.add('hidden');
    rDirect?.classList.add('hidden');
    rPin?.classList.remove('hidden');
  } else {
    rSniWrap?.classList.remove('hidden');
    rWsPath?.classList.remove('hidden');
    rDirect?.classList.add('hidden');
    rPin?.classList.remove('hidden');
  }
}

function toggleMobileConsole() {
  consoleCollapsed = !consoleCollapsed;
  el('putty-body')?.classList.toggle('hidden', consoleCollapsed);
  setText('console-toggle-btn', consoleCollapsed ? '▲' : '▼');
}

function setTab(t, persist) {
  if (t === 'config' && currentTab === 'config' && persist) {
    t = 'servers';
  }
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
  if (el('btn-range-24h')) el('btn-range-24h').className = r === '24h' ? 'range-pill active' : 'range-pill';
  if (el('btn-range-7d')) el('btn-range-7d').className = r === '7d' ? 'range-pill active' : 'range-pill';
  fetchState();
}

function setUsageRange(r) {
  usageRange = r;
  if (el('btn-bw-7d')) el('btn-bw-7d').className = r === '7d' ? 'range-pill active' : 'range-pill';
  if (el('btn-bw-30d')) el('btn-bw-30d').className = r === '30d' ? 'range-pill active' : 'range-pill';
  renderPingAnalytics();
}

function autoUpdateTunnelName() {
  const nameIn = el('tun-name'), dstIn = el('tun-dst-srv');
  if (!nameIn || !dstIn || (userEditedName && nameIn.value.trim() !== '')) return;
  const dstObj = currentServers.find(s => s.id === dstIn.value);
  nameIn.value = `Master-to-${dstObj ? dstObj.name.split(' ')[0] : 'Agent'}`;
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

async function deleteServer(id) {
  const msg = lang === 'fa' ? 'آیا این سرور حذف و سرویس دیده‌بان از روی آن پاک شود؟' : 'Remove this server AND uninstall Agent from the remote host?';
  if (!confirm(msg)) return;
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
  pingResults[id] = '...';
  renderTunnels();
  const res = await fetch('/api/ping?id=' + id);
  const data = await res.json();
  pingResults[id] = data.ok ? `✓ ${data.ms}ms` : `✕ ${data.error || 'Err'}`;
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
  if (!cleanInbounds) return alert(lang === 'fa' ? 'لطفاً حداقل یک پورت ورودی وارد کنید' : 'Enter at least one Inbound Port (e.g. 45454)');
  const payload = {
    id: el('tun-id')?.value || '',
    name: (el('tun-name')?.value || '').trim() || 'Bridge',
    method: selectedMethod,
    source_server: 'local',
    target_server: el('tun-dst-srv')?.value || 'local',
    tunnel_port: cleanPortsInput(el('tunnel-port')?.value || '') || '10201',
    inbound_ports: cleanInbounds,
    speed_limit: String(parseFloat(el('tun-speed')?.value) || 0),
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
  const port = (el('cfg-port')?.value || '').trim() || '6001', path = (el('cfg-path')?.value || '').trim();
  const ctrl_l4 = (el('cfg-ctrl-l4')?.value || '').trim() || '6005';
  const ctrl_direct = (el('cfg-ctrl-direct')?.value || '').trim() || '6006';
  const ctrl_wss = (el('cfg-ctrl-wss')?.value || '').trim() || '6007';
  const ctrl_grpc = (el('cfg-ctrl-grpc')?.value || '').trim() || '6008';
  const methods = [];
  if (el('chk-ctrl-grpc')?.checked) methods.push('grpc');
  if (el('chk-ctrl-wss')?.checked) methods.push('wss');
  if (el('chk-ctrl-direct')?.checked) methods.push('direct');
  if (el('chk-ctrl-l4')?.checked) methods.push('forward');
  if (methods.length === 0) return alert('Keep at least one Control Port checked!');
  const ctrl_methods = methods.join(',');

  await fetch('/api/config?server_id=' + selectedServer, {
    method: 'POST',
    body: JSON.stringify({ port, path, ctrl_l4, ctrl_direct, ctrl_wss, ctrl_grpc, ctrl_methods })
  });
  if (selectedServer === 'local') setTimeout(() => { window.location.href = `${window.location.protocol}//${window.location.hostname}:${port}/${path}/`; }, 2000);
  else alert(lang === 'fa' ? 'تنظیمات سرور دیده‌بان ذخیره شد!' : 'Agent configuration updated!');
}

function buildSVGChart(values, unit, strokeColor, gradId) {
  let clean = (values || []).filter(v => typeof v === 'number' && !isNaN(v));
  if (clean.length === 0) clean = [0, 0, 0, 0];
  while (clean.length < 10) clean.unshift(clean[0]);
  const w = 1000, h = 185, padL = 66, padR = 16, padT = 16, padB = 18, plotW = w - padL - padR, plotH = h - padT - padB;
  const maxVal = Math.max(...clean, unit === 'ms' ? 200 : 2);
  const topVal = maxVal.toFixed(unit === 'ms' ? 0 : 1);
  const midVal = (maxVal / 2).toFixed(unit === 'ms' ? 0 : 1);
  const pts = clean.map((v, idx) => `${(padL + (idx / (clean.length - 1)) * plotW).toFixed(1)},${(padT + plotH - ((v / maxVal) * plotH)).toFixed(1)}`).join(' ');
  const area = `${padL},${padT + plotH} ${pts} ${padL + plotW},${padT + plotH}`;
  return `<svg viewBox="0 0 ${w} ${h}" class="w-full h-full" preserveAspectRatio="none">
    <defs><linearGradient id="${gradId}" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="${strokeColor}" stop-opacity="0.38"/><stop offset="100%" stop-color="${strokeColor}" stop-opacity="0.02"/></linearGradient></defs>
    <line x1="${padL}" y1="${padT}" x2="${w - padR}" y2="${padT}" stroke="#162033" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH / 2}" x2="${w - padR}" y2="${padT + plotH / 2}" stroke="#162033" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH}" x2="${w - padR}" y2="${padT + plotH}" stroke="#162033"/>
    <text x="8" y="${padT + 4}" fill="#64748b" font-size="10" font-family="monospace">${topVal} ${unit}</text>
    <text x="8" y="${padT + plotH / 2 + 4}" fill="#64748b" font-size="10" font-family="monospace">${midVal} ${unit}</text>
    <text x="8" y="${padT + plotH + 3}" fill="#64748b" font-size="10" font-family="monospace">0 ${unit}</text>
    <polygon points="${area}" fill="url(#${gradId})"/><polyline fill="none" stroke="${strokeColor}" stroke-width="2.2" points="${pts}"/></svg>`;
}

function fmtAxisTime(ts, is7d) {
  if (!ts) return '--:--';
  const d = new Date(ts * 1000);
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  if (is7d) {
    const mo = String(d.getMonth() + 1).padStart(2, '0');
    const da = String(d.getDate()).padStart(2, '0');
    return `${mo}-${da} ${hh}:${mm}`;
  }
  return `${hh}:${mm}`;
}

function buildHistorySVGChart(points) {
  const is7d = historyRange === '7d';
  const spanSec = is7d ? 7 * 86400 : 86400;
  const nowSec = Math.floor(Date.now() / 1000);
  const startSec = nowSec - spanSec;

  let list = (points && points.length) ? points.slice() : [];
  if (list.length === 0) {
    for (let i = 0; i <= 12; i++) {
      list.push({ ts: startSec + Math.floor((i / 12) * spanSec), ms: 48, ok: true });
    }
  } else if (list[0].ts > startSec + spanSec * 0.1) {
    // Pad the start of the window so the X-axis always spans the full 24h or 7d range
    const firstMs = list[0].ms || 48;
    list.unshift({ ts: startSec, ms: firstMs, ok: true });
  }

  const w = 1000, h = 185, padL = 66, padR = 18, padT = 16, padB = 24, plotW = w - padL - padR, plotH = h - padT - padB;
  const vals = list.map(p => (p.ok && p.ms > 0) ? p.ms : 0);
  const maxVal = Math.max(...vals, 200), midVal = Math.round(maxVal / 2), topVal = Math.round(maxVal);

  const xForTs = (ts) => {
    const clamped = Math.max(startSec, Math.min(nowSec, ts || nowSec));
    return padL + ((clamped - startSec) / spanSec) * plotW;
  };

  let redBars = '';
  list.forEach((p) => {
    if (!p.ok) {
      const cx = xForTs(p.ts), bw = 8, bx = Math.max(padL, cx - bw / 2);
      redBars += `<rect x="${bx.toFixed(1)}" y="${padT}" width="${bw}" height="${plotH}" fill="rgba(244,63,94,0.26)"/>`;
      redBars += `<line x1="${cx.toFixed(1)}" y1="${padT}" x2="${cx.toFixed(1)}" y2="${padT + plotH}" stroke="#f43f5e" stroke-width="1.3" stroke-opacity="0.75"/>`;
    }
  });

  const pts = list.map((p, idx) => {
    const v = vals[idx];
    const x = xForTs(p.ts);
    const y = padT + plotH - ((v / maxVal) * plotH);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(' ');
  const firstX = xForTs(list[0].ts).toFixed(1);
  const lastX = xForTs(list[list.length - 1].ts).toFixed(1);
  const area = `${firstX},${padT + plotH} ${pts} ${lastX},${padT + plotH}`;

  const tStart = fmtAxisTime(startSec, is7d);
  const tMid = fmtAxisTime(startSec + Math.floor(spanSec / 2), is7d);
  const tEnd = fmtAxisTime(nowSec, is7d);

  return `<svg viewBox="0 0 ${w} ${h}" class="w-full h-full" preserveAspectRatio="none">
    <defs><linearGradient id="gradHist" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="#38bdf8" stop-opacity="0.38"/><stop offset="100%" stop-color="#38bdf8" stop-opacity="0.02"/></linearGradient></defs>
    <line x1="${padL}" y1="${padT}" x2="${w - padR}" y2="${padT}" stroke="#162033" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH / 2}" x2="${w - padR}" y2="${padT + plotH / 2}" stroke="#162033" stroke-dasharray="3,3"/>
    <line x1="${padL}" y1="${padT + plotH}" x2="${w - padR}" y2="${padT + plotH}" stroke="#162033"/>
    ${redBars}
    <text x="8" y="${padT + 4}" fill="#64748b" font-size="10" font-family="monospace">${topVal} ms</text>
    <text x="8" y="${padT + plotH / 2 + 4}" fill="#64748b" font-size="10" font-family="monospace">${midVal} ms</text>
    <text x="8" y="${padT + plotH + 3}" fill="#f43f5e" font-size="10" font-weight="bold" font-family="monospace">0 (OFF)</text>
    <polygon points="${area}" fill="url(#gradHist)"/><polyline fill="none" stroke="#38bdf8" stroke-width="2.2" points="${pts}"/>
    <text x="${padL}" y="${h - 5}" fill="#64748b" font-size="10" font-family="monospace">${tStart}</text>
    <text x="${padL + plotW / 2 - 22}" y="${h - 5}" fill="#64748b" font-size="10" font-family="monospace">${tMid}</text>
    <text x="${w - padR - (is7d ? 68 : 34)}" y="${h - 5}" fill="#38bdf8" font-size="10" font-family="monospace">${tEnd}</text></svg>`;
}

function buildDailyBWChart(dailyUsage) {
  const numDays = usageRange === '7d' ? 7 : 30;
  const map = {};
  (dailyUsage || []).forEach(d => { map[d.date] = d; });

  const days = [];
  let totRxMB = 0, totTxMB = 0;
  const now = new Date();
  for (let i = numDays - 1; i >= 0; i--) {
    const dt = new Date(now.getTime() - i * 86400000);
    const iso = dt.toISOString().slice(0, 10);
    const rec = map[iso] || { rx_mb: 0, tx_mb: 0 };
    totRxMB += (rec.rx_mb || 0);
    totTxMB += (rec.tx_mb || 0);
    days.push({ label: iso.slice(5, 10), rxGB: (rec.rx_mb || 0) / 1024, txGB: (rec.tx_mb || 0) / 1024 });
  }

  setText('pill-month-rx', `${(totRxMB / 1024).toFixed(1)} GB`);
  setText('pill-month-tx', `${(totTxMB / 1024).toFixed(1)} GB`);

  let maxGB = 1.0;
  days.forEach(d => { if (d.rxGB > maxGB) maxGB = d.rxGB; if (d.txGB > maxGB) maxGB = d.txGB; });
  maxGB = Math.ceil(maxGB * 1.15 * 10) / 10;

  const w = 1000, h = 205, padL = 66, padR = 20, padT = 16, padB = 28, plotW = w - padL - padR, plotH = h - padT - padB;
  const colW = plotW / numDays;
  const barW = numDays === 7 ? 20 : Math.max((colW - 8) / 2, 4);
  const gap = numDays === 7 ? 4 : 1.5;

  let gridLines = '';
  for (let g = 0; g <= 4; g++) {
    const y = padT + (g / 4) * plotH;
    const val = ((4 - g) / 4) * maxGB;
    const lbl = g === 4 ? '0 B' : `${val.toFixed(1)} GB`;
    gridLines += `<line x1="${padL}" y1="${y.toFixed(1)}" x2="${w - padR}" y2="${y.toFixed(1)}" stroke="#151f30" stroke-dasharray="3,3"/>`;
    gridLines += `<text x="${padL - 10}" y="${(y + 3.5).toFixed(1)}" fill="#64748b" font-size="10" text-anchor="end" font-family="monospace">${lbl}</text>`;
  }

  let bars = '';
  days.forEach((d, idx) => {
    const center = padL + idx * colW + colW / 2;
    const xRx = center - barW - gap / 2;
    const xTx = center + gap / 2;
    const hRx = Math.max((d.rxGB / maxGB) * plotH, 4);
    const hTx = Math.max((d.txGB / maxGB) * plotH, 4);
    const yRx = padT + plotH - hRx;
    const yTx = padT + plotH - hTx;

    bars += `<rect x="${xRx.toFixed(1)}" y="${yRx.toFixed(1)}" width="${barW.toFixed(1)}" height="${hRx.toFixed(1)}" rx="3" fill="#2dd4bf"/>`;
    bars += `<rect x="${xTx.toFixed(1)}" y="${yTx.toFixed(1)}" width="${barW.toFixed(1)}" height="${hTx.toFixed(1)}" rx="3" fill="#60a5fa"/>`;
    if (numDays === 7 || idx % 3 === 0 || idx === numDays - 1) {
      bars += `<text x="${center.toFixed(1)}" y="${h - 7}" fill="#64748b" font-size="10" text-anchor="middle" font-family="monospace">${d.label}</text>`;
    }
  });

  return `<svg viewBox="0 0 ${w} ${h}" class="w-full h-full" preserveAspectRatio="none">${gridLines}${bars}</svg>`;
}

function formatMethodLabel(m) {
  if (!m) return 'Control Ready';
  const up = m.toUpperCase();
  if (up.includes('GRPC')) return 'Method 01 (gRPC 90%)';
  if (up.includes('WSS')) return 'Method 02 (WSS 85%)';
  if (up.includes('DIRECT')) return 'Method 03 (Direct-TLS 75%)';
  if (up.includes('FORWARD') || up.includes('L4')) return 'Method 04 (L4 Raw 20%)';
  return up;
}

function renderServers() {
  const topSel = el('top-server-select'), dstSel = el('tun-dst-srv');
  if (topSel) {
    const allOpts = currentServers.map(s => `<option value="${s.id}">${s.name} (${s.ip})</option>`).join('');
    if (topSel.innerHTML !== allOpts) topSel.innerHTML = allOpts;
    topSel.value = selectedServer;
  }
  if (dstSel) {
    const prevDst = dstSel.value;
    const remoteServers = currentServers.filter(s => !s.is_local);
    const targetList = remoteServers.length > 0 ? remoteServers : currentServers;
    const dstOpts = targetList.map(s => `<option value="${s.id}">${s.name} (${s.ip})</option>`).join('');
    if (dstSel.innerHTML !== dstOpts) dstSel.innerHTML = dstOpts;
    if (prevDst && targetList.some(s => s.id === prevDst)) dstSel.value = prevDst;
    else if (targetList.length > 0) dstSel.value = targetList[targetList.length - 1].id;
  }
  setText('agents-count-badge', String(currentServers.length || 1));
  syncDefaultServerNames();
  if (el('tun-name') && !el('tun-name').value) autoUpdateTunnelName();

  const activeSrv = currentServers.find(s => s.id === selectedServer) || currentServers[0];
  if (activeSrv) {
    setText('putty-title', `root@${activeSrv.name} (${activeSrv.ip})${activeSrv.via_method ? ` [${activeSrv.via_method}]` : ''}`);
    setText('cfg-target-badge', activeSrv.name);
    setText('ping-target-srv-badge', `${activeSrv.name} (${activeSrv.ip})`);
    setText('tp-target-srv-badge', `${activeSrv.name} (${activeSrv.ip})`);
  }

  const cc = sysMetrics.bbr_status || 'cubic', qd = sysMetrics.qdisc || 'fq_codel';
  setText('bbr-cc-val', cc.toUpperCase()); setText('bbr-qdisc-val', qd.toUpperCase());
  const bbrBadge = el('bbr-status-badge');
  if (bbrBadge) {
    bbrBadge.className = cc === 'bbr' ? 'px-2.5 py-1 rounded-md text-[11px] font-mono font-bold bg-emerald-950/80 text-emerald-400 border border-emerald-800/60' : 'px-2.5 py-1 rounded-md text-[11px] font-mono font-bold bg-amber-950/80 text-amber-400 border border-amber-800/60';
    bbrBadge.innerText = cc === 'bbr' ? '● BBR Active' : `● ${cc.toUpperCase()}`;
  }

  setHTML('server-cards-list', currentServers.map(s => {
    const isOnline = s.status === 'Online';
    const statusText = isOnline ? 'CONNECTED' : s.status.toUpperCase();
    const statusColor = isOnline ? 'text-emerald-400' : 'text-amber-400';
    const dotColor = isOnline ? 'bg-emerald-400' : 'bg-amber-400';
    const verBadge = `<span class="text-[10px] px-1.5 py-0.5 rounded bg-[#152033] text-slate-300 border border-[#22324d] font-mono font-semibold">${s.version || 'v2.7.0'}</span>`;
    const roleInfo = s.is_local
      ? `Master Node • Control Ports :${ctrlGRPC}/:${ctrlWSS}/:${ctrlDirect}`
      : `${formatMethodLabel(s.via_method)} • Port ${s.panel_port || '6001'} • Agent ➔ Master`;

    const probeBtn = !s.is_local
      ? (s.probing
          ? `<button onclick="toggleServerProbe('${s.id}', true)" class="icon-action-btn px-2.5 py-1.5 text-[11px] font-semibold text-rose-400 border-rose-900/60 animate-pulse" title="Pause Probe">⏸</button>`
          : `<button onclick="toggleServerProbe('${s.id}', false)" class="icon-action-btn px-3 py-1.5 text-[11px] font-semibold text-slate-200 gap-1" title="Probe Next Method"><span>⏭</span><span>Probe</span></button>`)
      : '';

    return `<div class="card p-4 flex flex-col gap-3 ${s.id === selectedServer ? 'accent-border' : ''}" dir="ltr">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex items-center gap-3 min-w-0">
          <div class="w-10 h-10 rounded-xl bg-[#0f1726] border border-[#1c283d] flex items-center justify-center text-sky-400 shrink-0">
            <svg class="w-5 h-5" fill="none" stroke="currentColor" stroke-width="1.8" viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="6" rx="1.5"/><rect x="3" y="14" width="18" height="6" rx="1.5"/><circle cx="7" cy="7" r="1" fill="currentColor"/><circle cx="7" cy="17" r="1" fill="currentColor"/></svg>
          </div>
          <div class="min-w-0">
            <div class="flex items-center gap-2">
              <h4 class="font-bold text-sm text-white uppercase tracking-wide truncate">${s.name}</h4>
              ${verBadge}
              ${s.id === selectedServer ? `<span class="text-[10px] font-mono px-1.5 py-0.5 rounded bg-sky-950 text-sky-400 border border-sky-800/70">ACTIVE</span>` : ''}
            </div>
            <div class="text-xs font-mono text-slate-400 mt-0.5 truncate">
              <span>${s.ip}</span>
              <span class="text-emerald-400 font-medium"> • ${roleInfo}</span>
            </div>
          </div>
        </div>

        <div class="flex items-center gap-2 shrink-0 ml-auto">
          <div class="flex items-center gap-1.5 mr-1">
            <span class="w-2 h-2 rounded-full ${dotColor}"></span>
            <span class="text-[11px] font-mono font-bold tracking-wider ${statusColor}">${statusText}</span>
          </div>
          <button onclick="changeSelectedServer('${s.id}')" class="icon-action-btn px-2.5 py-1.5 text-xs font-semibold ${s.id === selectedServer ? 'text-sky-400 border-sky-800/80' : ''}" title="Select Server">${s.id === selectedServer ? '✓' : 'Select'}</button>
          ${probeBtn}
          <button onclick="copyTextSilent('${s.cert_pin || s.ip}')" class="icon-action-btn p-2 text-xs" title="Copy SHA-256 Pin">📋</button>
          <button id="btn-upd-${s.id}" onclick="triggerSelfUpdate('${s.id}')" class="icon-action-btn p-2 text-xs" title="Update Binary">↻</button>
          <a href="http://${s.ip}:${s.panel_port || '6001'}/sentinel-info" target="_blank" class="icon-action-btn p-2 text-xs" title="Sentinel Status Page">🌐</a>
          ${!s.is_local ? `<button onclick="deleteServer('${s.id}')" class="icon-action-btn p-2 text-xs text-rose-400 hover:text-rose-300" title="Uninstall & Delete">🗑️</button>` : ''}
        </div>
      </div>

      <div class="pt-2.5 border-t border-[#141d2e] flex items-center justify-between text-[11px] font-mono text-slate-500">
        <span class="truncate">SHA-256 Pin: <b class="text-slate-400 font-normal">${s.cert_pin ? s.cert_pin.slice(0, 36) + '...' : 'Pending Sync...'}</b></span>
        <span class="shrink-0 text-slate-400">${s.is_local ? 'Master Vault Active' : 'Token Ready'}</span>
      </div>
    </div>`;
  }).join(''));
}

function renderTunnels() {
  setHTML('tunnel-list', currentTunnels.map(t => {
    const dstSrv = currentServers.find(s => s.id === t.target_server);
    let mBadge = 'WSS 85%';
    if (t.method === 'direct' || t.method === 'spoof') mBadge = 'DIRECT-TLS 75%';
    if (t.method === 'forward') mBadge = 'L4 RAW 20%';
    if (t.method === 'grpc') mBadge = 'gRPC (H2) 90%';
    const spdTxt = (t.speed_limit && parseFloat(t.speed_limit) > 0) ? `${t.speed_limit} MB/s` : 'Unlimited';
    return `<div class="card p-4 flex flex-col gap-2.5" dir="ltr">
      <div class="flex flex-wrap justify-between items-center gap-2">
        <div class="flex items-center gap-2 min-w-0">
          <span class="px-2.5 py-0.5 rounded-md text-[11px] font-bold font-mono bg-[#131d31] text-sky-400 border border-[#1f2e4a] shrink-0">${mBadge}</span>
          <span class="font-bold text-sm text-white truncate">${t.name}</span>
          <span class="text-xs text-emerald-400 font-mono">• Master ➔ ${dstSrv ? dstSrv.name : 'Agent'}</span>
        </div>
        <div class="flex items-center gap-1.5 shrink-0">
          <button onclick="pingTunnel('${t.id}')" class="icon-action-btn px-3 py-1 text-xs font-mono font-semibold text-emerald-400 border-emerald-900/60">${pingResults[t.id] || '⚡ Ping'}</button>
          <button onclick="editTunnel('${t.id}')" class="icon-action-btn p-1.5 text-xs">✏️</button>
          <button onclick="deleteTunnel('${t.id}')" class="icon-action-btn p-1.5 text-xs text-rose-400">🗑️</button>
        </div>
      </div>
      <div class="pt-2 border-t border-[#141d2e] flex flex-wrap items-center justify-between gap-2 text-xs font-mono text-slate-400">
        <span>Exit: <b class="text-slate-200">${t.remote_addr || (':' + t.tunnel_port)}</b></span>
        <span>Inbounds: <b class="text-emerald-400">[${t.inbound_ports}]</b></span>
        <span>Limit: <b class="text-amber-400">${spdTxt}</b></span>
        <span>SNI: <b class="text-sky-300">${t.method === 'forward' ? 'Raw L4' : t.sni}</b></span>
      </div>
    </div>`;
  }).join(''));
}

function renderPingAnalytics() {
  if (!el('kpi-cpu') || currentTab !== 'logs') return;
  const cpu = sysMetrics.cpu_pct || 0, ramUsed = sysMetrics.ram_used_mb || 0, ramTot = sysMetrics.ram_total_mb || 1024, ramPct = sysMetrics.ram_pct || 0;
  const rxMbps = sysMetrics.live_rx_mbps || 0, txMbps = sysMetrics.live_tx_mbps || 0;
  setText('kpi-cpu', `${cpu.toFixed(1)}%`);
  setText('kpi-ram', `${ramPct}% (${ramUsed}/${ramTot}M)`);

  const totMbps = rxMbps + txMbps;
  setText('recent-tp-val', `↓ ${rxMbps.toFixed(2)} Mbps | ↑ ${txMbps.toFixed(2)} Mbps (${(totMbps / 8).toFixed(2)} MB/s)`);
  setHTML('svg-recent-tp', buildSVGChart(sysMetrics.tp_history || [0.1, 0.2, 0.15, 0.3], 'Mbps', '#818cf8', 'gradTp'));
  setHTML('svg-daily-bw', buildDailyBWChart(sysMetrics.daily_usage || []));

  if (!pingSummary) return;
  setText('kpi-reach', pingSummary.reach_str || '20 / 20');
  const fails = pingSummary.fails || 0;
  setText('pill-uptime', `● Uptime: ${pingSummary.uptime_pct || '100.0'}%`);
  setText('pill-down', `● Down: ${fails * 10}m`);
  setText('pill-avg', `⚡ Avg: ${pingSummary.avg_ms || 50} ms`);
  const wins = pingSummary.windows || [];
  setText('pill-outage-windows', wins.length ? `Outages: ${wins.join(' | ')}` : `Outages: None — 100% Continuous Uptime`);
  setHTML('svg-history-ping', buildHistorySVGChart(pingSummary.chart_points || []));
}

async function fetchState() {
  try {
    const res = await fetch(`/api/state?server_id=${selectedServer}&tab=${currentTab}&range=${historyRange}`);
    if (res.status === 401) { window.location.href = 'login'; return; }
    const data = await res.json();
    if (data.version) setText('app-version-badge', data.version);
    const updBtn = el('btn-self-update');
    if (updBtn && !updBtn.innerText.includes('⏳')) {
      if (data.update_available) {
        updBtn.className = 'icon-action-btn px-2.5 py-1.5 bg-amber-500 text-black text-xs font-bold animate-pulse';
        updBtn.innerHTML = '🔥';
      } else {
        updBtn.className = 'icon-action-btn px-2.5 py-1.5 text-xs font-mono text-emerald-400';
        updBtn.innerHTML = '⬆️';
      }
    }
    if (data.server_time) syncServerClockFromStr(data.server_time);
    if (data.ctrl_l4_port) ctrlL4 = data.ctrl_l4_port;
    if (data.ctrl_direct_port) ctrlDirect = data.ctrl_direct_port;
    if (data.ctrl_wss_port) ctrlWSS = data.ctrl_wss_port;
    if (data.ctrl_grpc_port) ctrlGRPC = data.ctrl_grpc_port;
    if (data.ctrl_methods) ctrlMethods = data.ctrl_methods;
    if (data.enroll_token && data.enroll_token !== enrollToken) {
      enrollToken = data.enroll_token;
      masterIP = data.master_ip || window.location.hostname;
      updateEnrollCommandUI();
    }
    if (!initialLoaded && data.prefs) {
      initialLoaded = true;
      lang = data.prefs.lang || 'fa';
      selectedServer = data.prefs.selected_server || 'local';
      applyLangUI();
      applyTheme(data.prefs.theme || 'cyan', false);
      setTab(data.prefs.active_tab || 'servers', false);
    }
    currentServers = data.servers || [];
    currentTunnels = data.tunnels || [];
    if (data.ping_summary) pingSummary = data.ping_summary;
    sysMetrics = data.metrics || {};
    renderServers();
    renderTunnels();
    renderPingAnalytics();

    const puttyEl = el('putty-box');
    if (puttyEl) {
      const wasAtBottom = puttyEl.scrollHeight - puttyEl.clientHeight <= puttyEl.scrollTop + 40;
      puttyEl.innerText = (data.logs || []).join('\n');
      if (wasAtBottom) puttyEl.scrollTop = puttyEl.scrollHeight;
    }
    if (el('cfg-port') && !el('cfg-port').value) {
      el('cfg-port').value = data.panel_port || '6001';
      if (el('cfg-path')) el('cfg-path').value = data.panel_path || '';
      if (el('cfg-ctrl-l4')) el('cfg-ctrl-l4').value = ctrlL4;
      if (el('cfg-ctrl-direct')) el('cfg-ctrl-direct').value = ctrlDirect;
      if (el('cfg-ctrl-wss')) el('cfg-ctrl-wss').value = ctrlWSS;
      if (el('cfg-ctrl-grpc')) el('cfg-ctrl-grpc').value = ctrlGRPC;
      const mSet = (ctrlMethods || 'grpc,wss,direct').split(',');
      if (el('chk-ctrl-grpc')) el('chk-ctrl-grpc').checked = mSet.includes('grpc');
      if (el('chk-ctrl-wss')) el('chk-ctrl-wss').checked = mSet.includes('wss');
      if (el('chk-ctrl-direct')) el('chk-ctrl-direct').checked = mSet.includes('direct');
      if (el('chk-ctrl-l4')) el('chk-ctrl-l4').checked = mSet.includes('forward');
    }
  } catch (err) { console.error('State sync error:', err); }
}

applyLangUI();
updatePollRateUI();
fetchState();
restartPolling();
