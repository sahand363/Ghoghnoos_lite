const dict = {
  en: {
    servers: 'Servers', tunnels: 'Tunnels', logs: 'Log',
    enroll_title: '1. Enroll Agent Server',
    enroll_node_lbl: 'Agent Name',
    enroll_addr_lbl: 'Master Ports',
    btn_rotate_tok: 'Rotate',
    btn_copy_enroll: '📋 Copy One-Liner (Auto-Rotates Token)',
    rv_title: '2. Forward Rendezvous Probe',
    btn_start_rv: 'Probe',
    network_settings: 'Panel & Control Ports',
    lbl_panel_port: 'Panel Port', lbl_panel_path: 'Secret Path',
    ctrl_ports_title: 'Control & Rendezvous Ports',
    lbl_ctrl_grpc: 'gRPC (90%)', lbl_ctrl_wss: 'WSS (85%)', lbl_ctrl_direct: 'Direct (75%)', lbl_ctrl_l4: 'L4 Raw (20%)',
    save_restart: 'Save & Restart',
    bbr_title: 'Google BBR + FQ Acceleration',
    btn_bbr: '⚡ Enable BBR + FQ',
    step1_method: '1. Transport Engine',
    stealth_lbl: 'Stealth',
    slot_soon_title: 'Reserved',
    new_tunnel: '2. Bridge Master to Agent', reset: 'Reset',
    dst_srv: 'Target Agent Server',
    tun_name: 'Tunnel Name', tun_port_lbl: 'Tunnel Port',
    inb_port_lbl: 'Inbound Port(s)', spd_limit_lbl: 'Speed Limit',
    sni_lbl: 'TLS SNI Hostname', wspath_lbl: 'WS Path',
    fp_lbl: 'TLS Profile', seg_lbl: 'ClientHello Frag',
    cert_pin_lbl: 'Exit SHA-256 Pin',
    create: '⚡ Establish Tunnel',
    active_tunnels: 'Active Tunnels'
  },
  fa: {
    servers: 'سرورها', tunnels: 'تانل‌ها', logs: 'گزارش',
    enroll_title: '۱. افزودن سرور دیده‌بان',
    enroll_node_lbl: 'نام دیده‌بان',
    enroll_addr_lbl: 'پورت‌های مستر',
    btn_rotate_tok: 'تغییر توکن',
    btn_copy_enroll: '📋 کپی دستور تک‌خطی (تغییر خودکار توکن)',
    rv_title: '۲. کاوشگر مستقیم مستر',
    btn_start_rv: 'کاوش',
    network_settings: 'تنظیمات پنل و پورت‌های کنترل',
    lbl_panel_port: 'پورت پنل', lbl_panel_path: 'مسیر مخفی',
    ctrl_ports_title: 'پورت‌های کنترل و کاوش (تیک = فعال)',
    lbl_ctrl_grpc: 'gRPC (٪۹۰)', lbl_ctrl_wss: 'WSS (٪۸۵)', lbl_ctrl_direct: 'Direct (٪۷۵)', lbl_ctrl_l4: 'L4 خام (٪۲۰)',
    save_restart: 'ذخیره و ری‌استارت',
    bbr_title: 'شتاب‌دهنده هسته (BBR + FQ)',
    btn_bbr: '⚡ فعال‌سازی BBR + FQ',
    step1_method: '۱. انتخاب موتور تانل',
    stealth_lbl: 'مخفی‌سازی',
    slot_soon_title: 'رزرو آینده',
    new_tunnel: '۲. اتصال مستر به دیده‌بان', reset: 'پاکسازی',
    dst_srv: 'سرور مقصد (دیده‌بان)',
    tun_name: 'نام تانل', tun_port_lbl: 'پورت تانل',
    inb_port_lbl: 'پورت(های) ورودی', spd_limit_lbl: 'محدودیت سرعت',
    sni_lbl: 'دامنه TLS SNI', wspath_lbl: 'مسیر وب‌سوکت',
    fp_lbl: 'پروفایل TLS', seg_lbl: 'قطعه‌بندی بسته',
    cert_pin_lbl: 'پین گواهی SHA-256',
    create: '⚡ برقراری تانل',
    active_tunnels: 'تانل‌های فعال'
  }
};

const helpTexts = {
  en: {
    enroll: ['Step 1: One-Liner Enrollment', 'Run the copied command on your remote VPS terminal.\n\n• Choose [1] Connect & Listen (Bidirectional) or [2] Listen Only (Passive).\n• Every time you click Copy, the security token automatically rotates for the next server while keeping your copied command valid.'],
    node_name: ['Agent Server Name', 'A friendly name for your remote Sentinel node (auto-numbered as Server-2, Server-3, etc.).'],
    master_addr: ['Master Control Target (IP:L4:Direct:WSS:gRPC)', 'Format: MasterIP:6005:6006:6007:6008\nSpecifies the Master IP and the 4 control ports the remote Agent will use to link back to Master.'],
    rendezvous: ['Step 2: Master Forward Rendezvous Prober', 'If outbound connections from the remote server are blocked (or you chose [2] Listen Only during setup), enter the remote server IP here and press Probe (▶).\n\nMaster will actively hunt across enabled ports (gRPC ➔ WSS ➔ Direct-TLS ➔ L4) every 120–150s until linked.'],
    ctrlports: ['Dedicated Control & Rendezvous Ports', 'Separate from your Web Panel port (:6001) so DPI scanners never touch your UI:\n• gRPC (:6008): 90% Stealth (HTTP/2 ALPN h2)\n• WSS (:6007): 85% Stealth (WebSocket TLS 1.3)\n• Direct-TLS (:6006): 75% Stealth (Raw TLS 1.3)\n• L4 (:6005): 20% Raw HTTP API\n\nUncheck any method to disable it.'],
    port: ['Web Panel Port', 'The TCP port used to open this web dashboard (Default: 6001).'],
    path: ['Secret URL Path', 'Secret folder path required to access the panel URL.'],
    bbr: ['TCP BBR & FQ Acceleration', 'Google BBR congestion control and Fair Queueing (FQ) packet pacing maximize tunnel speed and prevent packet-loss throttling.'],
    stealth: ['Transport Engines & Stealth Guide', '• gRPC (90%): Multiplexes all streams over a single persistent HTTP/2 (ALPN h2) TLS 1.3 connection with 16MB windows.\n• WSS (85%): HTTP/1.1 WebSocket Upgrade over TLS 1.3.\n• Direct TLS (75%): Zero-HTTP raw TLS 1.3 stream with browser cipher fingerprinting and ClientHello segmentation.\n• Port Forward (20%): Raw L4 TCP byte relay without outer TLS wrapping.'],
    sni: ['TLS SNI Hostname', 'The ServerName presented during the TLS 1.3 handshake (e.g. www.speedtest.net).'],
    seg: ['TCP ClientHello Fragmentation', 'Splits the initial TLS ClientHello packet into smaller TCP segments (e.g. 64 bytes) to bypass DPI SNI inspection (0 = Off).'],
    pin: ['SHA-256 Certificate Pinning', 'Automatically synced from your target Agent server. Verifies the exact cryptographic fingerprint of the Exit node.']
  },
  fa: {
    enroll: ['گام ۱: نصب تک‌خطی دیده‌بان', 'دستور کپی‌شده را در ترمینال سرور خارج اجرا کنید.\n\n• امکان انتخاب بین حالت [1] اتصال دوطرفه یا [2] فقط شنود (Listen Only).\n• با هر بار کپی، توکن امنیتی به صورت خودکار برای سرور بعدی تغییر می‌کند اما دستور کپی‌شده شما معتبر می‌ماند.'],
    node_name: ['نام سرور دیده‌بان', 'نام دلخواه برای سرور خارج که به صورت خودکار شماره‌گذاری می‌شود (مثلاً Server-2).'],
    master_addr: ['آدرس و پورت‌های کنترل مستر', 'فرمت: IP:L4:Direct:WSS:gRPC\nآدرس IP سرور مستر و ۴ پورت کنترلی که دیده‌بان برای اتصال به مستر استفاده می‌کند.'],
    rendezvous: ['گام ۲: کاوشگر مستقیم مستر به دیده‌بان', 'اگر اتصال معکوس از سرور خارج به مستر مسدود است (یا دیده‌بان را روی حالت فقط شنود نصب کرده‌اید)، IP سرور خارج را وارد کرده و دکمه کاوش (▶) را بزنید تا مستر به آن متصل شود.'],
    ctrlports: ['پورت‌های اختصاصی کنترل و کاوش', 'جدا از پورت وب‌پنل (:6001) برای امنیت کامل:\n• پورت ۶۰۰۸ (gRPC): ۹۰٪ مخفی‌سازی (HTTP/2)\n• پورت ۶۰۰۷ (WSS): ۸۵٪ مخفی‌سازی (WebSocket)\n• پورت ۶۰۰۶ (Direct-TLS): ۷۵٪ مخفی‌سازی\n• پورت ۶۰۰۵ (L4): ارتباط خام\n\nبرداشتن تیک هر روش آن را غیرفعال می‌کند.'],
    port: ['پورت وب پنل', 'پورتی که برای باز کردن این پنل در مرورگر استفاده می‌شود (پیش‌فرض: 6001).'],
    path: ['مسیر مخفی پنل', 'عبارت امنیتی در آدرس پنل برای جلوگیری از شناسایی توسط اسکنرها.'],
    bbr: ['شتاب‌دهنده هسته لینوکس (BBR + FQ)', 'فعال‌سازی الگوریتم Google BBR و زمان‌بند FQ برای حداکثر سرعت و رفع افت سرعت ناشی از پکت‌لاس.'],
    stealth: ['راهنمای موتورهای انتقال و مخفی‌سازی', '• روش gRPC (٪۹۰): مالتی‌پلکس تمامی اتصالات روی یک استریم پایدار HTTP/2 (ALPN h2) با بستر TLS 1.3.\n• روش WSS (٪۸۵): شبیه‌سازی ترافیک وب‌سوکت روی TLS 1.3.\n• روش Direct TLS (٪۷۵): استریم مستقیم TLS 1.3 بدون سربار HTTP.\n• روش Port Forward (٪۲۰): انتقال مستقیم لایه ۴ بدون رمزنگاری اضافه.'],
    sni: ['دامنه TLS SNI', 'نام دامنه‌ای که در هندشیک TLS 1.3 نمایش داده می‌شود.'],
    seg: ['قطعه‌بندی ClientHello', 'بسته اولیه TLS را به قطعات کوچک‌تر تقسیم می‌کند تا فیلترینگ SNI دور زده شود (0 = غیرفعال).'],
    pin: ['پین گواهی SHA-256', 'به صورت خودکار از سرور دیده‌بان دریافت می‌شود و امنیت گواهی TLS را تضمین می‌کند.']
  }
};

function applyLangUI() {
  document.documentElement.dir = lang === 'fa' ? 'rtl' : 'ltr';
  document.documentElement.lang = lang;
  const lb = document.getElementById('lang-btn');
  if (lb) lb.innerText = lang === 'fa' ? 'EN' : 'FA';
  const map = dict[lang] || dict.en;
  document.querySelectorAll('[data-i18n]').forEach(el => {
    const k = el.getAttribute('data-i18n');
    if (map[k]) el.innerText = map[k];
  });
}

function toggleLang() {
  lang = lang === 'en' ? 'fa' : 'en';
  applyLangUI();
  if (typeof updateSpeedBadge === 'function') updateSpeedBadge();
  if (typeof renderServers === 'function') renderServers();
  if (typeof savePrefsToServer === 'function') savePrefsToServer();
}

function applyTheme(t, persist) {
  currentTheme = t;
  const palettes = {
    cyan: ['#38bdf8', 'rgba(56, 189, 248, 0.12)'],
    emerald: ['#10b981', 'rgba(16, 185, 129, 0.12)'],
    amber: ['#f59e0b', 'rgba(245, 158, 11, 0.12)'],
    indigo: ['#818cf8', 'rgba(129, 140, 248, 0.12)'],
    rose: ['#f43f5e', 'rgba(244, 63, 94, 0.12)']
  };
  const p = palettes[t] || palettes.cyan;
  document.documentElement.style.setProperty('--accent', p[0]);
  document.documentElement.style.setProperty('--accent-glow', p[1]);
  if (persist && typeof savePrefsToServer === 'function') savePrefsToServer();
}

function showHelpModal(key) {
  const item = (helpTexts[lang] || helpTexts.en)[key];
  if (!item) return;
  document.getElementById('help-modal-title').innerText = item[0];
  document.getElementById('help-modal-body').innerText = item[1];
  document.getElementById('help-modal-btn').innerText = lang === 'fa' ? 'متوجه شدم' : 'Got it';
  document.getElementById('help-modal-card').dir = lang === 'fa' ? 'rtl' : 'ltr';
  document.getElementById('help-modal').classList.remove('hidden');
}

function closeHelpModal() {
  document.getElementById('help-modal').classList.add('hidden');
}
