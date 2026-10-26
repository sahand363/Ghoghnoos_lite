const helpTexts = {
  en: {
    enroll: {
      title: '🔑 Stealth-Prioritized Reverse Dial-In & Dual Listeners',
      body: 'When SSH is blocked, run this one-liner on the remote server.\n\n1. Stealth Priority: Tests WSS (85% on :6007) ➔ Direct-TLS (75% on :6006) ➔ L4 (20% on :6005) without 20s delays.\n2. Never Quits: Even if blocked initially, it saves config and enters a 120–150s Survival Loop.\n3. Dual Listeners: Also listens on :6005/:6006/:6007 so Master can probe it directly!'
    },
    rendezvous: {
      title: '▶ Master Forward Rendezvous Prober',
      body: 'If your remote server cannot dial back into Master (reverse connection blocked), enter the remote server IP here and press Play (▶).\n\nMaster will periodically test WSS (:6007) ➔ Direct-TLS (:6006) ➔ L4 (:6005) every 120–150s while Sentinel does the same in reverse. As soon as either direction succeeds, they lock in and sync!'
    },
    ctrlports: {
      title: '🔗 Dedicated Control Ports (Default: 6005, 6006, 6007)',
      body: 'Completely separated from your Web Panel port (:6001). Both Master and Sentinel listen AND probe on these 3 ports:\n\n• WSS Port (Default 6007 — 85% Stealth)\n• Direct-TLS Port (Default 6006 — 75% Stealth)\n• L4 API Port (Default 6005 — 20% Stealth)\n\nChanging them here automatically updates the enrollment one-liner.'
    },
    stealth: {
      title: '🛡️ Method Stealth Percentage Explained',
      body: '• WSS (85% Stealth): Wraps traffic in TLS 1.3 + HTTP/1.1 WebSocket headers and serves a decoy 200 OK HTML webpage if probed.\n\n• Direct TLS (75% Stealth): Pure TLS 1.3 encrypted stream with browser cipher-suite profiles and TCP ClientHello segmentation, without HTTP header overhead.\n\n• Port Forward (20% Stealth): Raw L4 TCP forwarding (like socat). Best used when your inbound service already uses REALITY or TLS.'
    },
    sni: {
      title: 'TLS SNI Hostname (ServerName)',
      body: 'The hostname sent inside the TLS ClientHello handshake when connecting from the Entry server to the Exit server.'
    },
    seg: {
      title: 'TCP ClientHello Segment Size',
      body: 'Splits the initial TLS handshake packet into smaller TCP segments (0 = Off, or 64/128 bytes) so simple DPI matchers cannot parse the header.'
    },
    fq: {
      title: 'FQ (Fair Queueing Scheduler)',
      body: 'A Linux kernel packet scheduler (sch_fq) that paces network flows and eliminates bufferbloat for Google BBR.'
    },
    bbr: {
      title: 'Google BBR Congestion Control',
      body: 'Replaces legacy CUBIC with model-based congestion control to keep cross-border speeds high even under packet loss.'
    },
    pin: {
      title: 'SHA-256 Certificate Pinning',
      body: 'Verifies the exact cryptographic SHA-256 fingerprint of your Exit server certificate to block MITM interception.'
    },
    forward: {
      title: 'Raw Port Forwarding (L4 TCP Relay)',
      body: 'Pipes raw TCP bytes directly from the Entry port to the Exit service port with speed limiting and latency monitoring.'
    },
    port: {
      title: 'Web Panel Port (Default: 6001)',
      body: 'The TCP port used for opening this web dashboard in your browser.'
    },
    path: {
      title: 'Secret URL Path',
      body: 'Protects your web panel behind a secret URL slug so automated scanners get a 404 Not Found.'
    }
  },
  fa: {
    enroll: {
      title: '🔑 اتصال معکوس سنتینل و شنونده دوطرفه',
      body: 'زمانی که SSH مسدود است، این دستور تک‌خطی را در ترمینال سرور خارج اجرا کنید.\n\n۱. اولویت مخفی‌سازی: به ترتیب WSS (۸۵٪ روی 6007)، سپس Direct-TLS (۷۵٪ روی 6006) و L4 (۲۰٪ روی 6005) را بدون وقفه تست می‌کند.\n۲. حلقه دائمی ۱۲۰ تا ۱۵۰ ثانیه: حتی اگر در لحظه اول متصل نشود، متوقف نمی‌شود و در پس‌زمینه به تلاش ادامه می‌دهد.\n۳. شنونده همزمان: روی همین پورت‌ها گوش می‌دهد تا سرور مستر هم بتواند به آن متصل شود!'
    },
    rendezvous: {
      title: '▶ کاوشگر مستقیم مستر (Rendezvous Prober)',
      body: 'اگر اتصال معکوس از سرور خارج به ایران مسدود است، آی‌پی سرور دوم را وارد کرده و دکمه Play (▶) را بزنید.\n\nسرور مستر هر ۱۲۰ تا ۱۵۰ ثانیه متدهای WSS، سپس Direct-TLS و L4 را به سمت سرور دوم تست می‌کند و همزمان سنتینل هم تلاش می‌کند تا به محض باز شدن مسیر از هر طرف، متصل شوند!'
    },
    ctrlports: {
      title: '🔗 پورت‌های اختصاصی کنترل (پیش‌فرض: 6005، 6006، 6007)',
      body: 'کاملاً مجزا از پورت وب پنل (6001). هر دو سرور مستر و سنتینل روی این ۳ پورت هم گوش می‌دهند و هم کاوش می‌کنند:\n\n• پورت WSS (پیش‌فرض 6007 — ۸۵٪ مخفی‌سازی)\n• پورت Direct-TLS (پیش‌فرض 6006 — ۷۵٪ مخفی‌سازی)\n• پورت L4 (پیش‌فرض 6005 — ۲۰٪ مخفی‌سازی)'
    },
    stealth: {
      title: '🛡️ راهنمای درصد مخفی‌سازی (Stealth) متدها',
      body: '• متد WSS (۸۵٪ مخفی‌سازی): ترافیک را داخل TLS 1.3 و هدرهای وب‌سوکت قرار می‌دهد.\n• متد Direct TLS (۷۵٪ مخفی‌سازی): استریم مستقیم TLS 1.3 بدون سربار HTTP.\n• متد Port Forward (۲۰٪ مخفی‌سازی): انتقال مستقیم لایه ۴.'
    },
    sni: { title: 'دامنه TLS SNI', body: 'نام دامنه‌ای که در بسته اولیه TLS ClientHello ارسال می‌شود.' },
    seg: { title: 'قطعه‌بندی TCP ClientHello', body: 'بسته اولیه هندشیک TLS را به قطعات کوچک‌تر تقسیم می‌کند.' },
    fq: { title: 'زمان‌بند پکت FQ', body: 'صف‌بندی هوشمند کرنل لینوکس برای حذف تاخیر بافر.' },
    bbr: { title: 'کنترل ازدحام Google BBR', body: 'حفظ حداکثر سرعت در شرایط پکت‌لاس.' },
    pin: { title: 'پین گواهی SHA-256', body: 'بررسی اثرانگشت دقیق گواهی TLS سرور خارج.' },
    forward: { title: 'پورت فورواردینگ مستقیم', body: 'انتقال مستقیم بایت‌های TCP بین پورت‌های ورودی و خروجی.' },
    port: { title: 'پورت وب پنل (پیش‌فرض: 6001)', body: 'پورت باز کردن داشبورد مدیریت در مرورگر.' },
    path: { title: 'آدرس مخفی پنل', body: 'قرار دادن پنل در مسیر مخفی جهت جلوگیری از اسکن.' }
  }
};

const dict = {
  en: {
    servers: 'Servers', config: 'Config', tunnels: 'Tunnels', logs: 'Log',
    add_srv_title: 'Connect Remote Server & Deploy Sentinel',
    add_srv_sub: 'Choose Direct SSH Push or Bidirectional Rendezvous (when SSH is filtered).',
    mode_ssh_btn: '🚀 Method A: SSH Push', mode_enroll_btn: '🔑 Method B: Reverse / Rendezvous',
    srv_name: 'Server Name', srv_ip: 'Server IP Address', srv_port: 'SSH Port',
    srv_user: 'SSH Username', srv_pass: 'SSH Password',
    btn_deploy: '🚀 Connect via SSH & Deploy Sentinel (0% ➔ 100%)',
    enroll_title: '🔑 Step 1: Run Sentinel One-Liner (Reverse Loop + Listeners)',
    enroll_sub: 'Run this command on your remote VPS. It tests WSS ➔ Direct-TLS ➔ L4 every 120–150s AND opens local control listeners so Master can also probe it directly.',
    enroll_node_lbl: 'New Sentinel Name (Auto-Numbered)',
    enroll_addr_lbl: 'Master Control Target (IP:L4:Direct:WSS)',
    enroll_cmd_lbl: 'Paste this One-Liner on your Remote Server Terminal:',
    btn_rotate_tok: '🔄 Rotate Token',
    btn_copy_enroll: '📋 Copy Reverse Enrollment One-Liner Command',
    rv_title: '▶ Step 2 (Optional): Master Forward Rendezvous Prober',
    rv_sub: 'If reverse dial-in from the remote server is blocked, enter the remote server IP below and press Play (▶). Master will periodically probe WSS ➔ Direct-TLS ➔ L4 until one side breaks through!',
    btn_start_rv: 'Start Forward Probe',
    network_settings: 'Server Port, Control Ports & Login Configuration', lbl_panel_port: 'Web Panel Port',
    lbl_panel_path: 'Secret URL Path',
    ctrl_ports_title: '🔗 Dedicated Control & Rendezvous Ports (Listening + Probing)',
    lbl_ctrl_l4: 'L4 Control Port (20%)', lbl_ctrl_direct: 'Direct-TLS Port (75%)', lbl_ctrl_wss: 'WSS Control Port (85%)',
    lbl_panel_user: 'Panel Username', lbl_panel_pass: 'New Panel Password (blank = keep current)', save_restart: 'Save & Restart Selected Server',
    bbr_title: 'TCP BBR & FQ Kernel Acceleration', bbr_sub: 'Boosts tunnel throughput and eliminates packet-loss throttling on the selected server.',
    btn_bbr: '⚡ Enable Google BBR + FQ on Selected Server',
    step1_method: '1. Select Tunnel Transport Method', wss_desc: 'HTTP/1.1 WebSocket Upgrade over TLS 1.3',
    direct_desc: 'Zero-HTTP Raw TLS 1.3 Stream Relay', forward_desc: 'Raw L4 TCP Relay (Native socat stream)', stealth_lbl: 'Stealth',
    new_tunnel: '2. Bridge Tunnel Between Servers', reset: 'Clear Form',
    src_srv: 'Entry Server (Relay)', dst_srv: 'Exit Server (Target Node)',
    tun_name: 'Tunnel Name', tun_port_lbl: 'Tunnel Port', inb_port_lbl: 'Forwarding Inbound Port(s)', spd_limit_lbl: 'Speed (MB/s)',
    sni_lbl: 'TLS SNI Hostname (ServerName)', wspath_lbl: 'WebSocket Path (WSS Mode)',
    fp_lbl: 'TLS Cipher Suite Profile', seg_lbl: 'TCP ClientHello Segment Size',
    forward_note: 'Direct byte relay from Entry port straight to Exit port without outer TLS wrapping.',
    cert_pin_lbl: 'Pinned SHA-256 Certificate (Auto-filled from Exit Server)',
    create: '⚡ Establish Tunnel Between Selected Servers', active_tunnels: 'Active Tunnels (End-to-End Latency Ping & Edit)'
  },
  fa: {
    servers: 'سرورها', config: 'تنظیمات', tunnels: 'تانل‌ها', logs: 'لاگ',
    add_srv_title: 'اتصال به سرور جدید و نصب سنتینل',
    add_srv_sub: 'نصب مستقیم با SSH یا کاوش دوطرفه هوشمند (زمانی که SSH مسدود است).',
    mode_ssh_btn: '🚀 روش اول: نصب با SSH', mode_enroll_btn: '🔑 روش دوم: اتصال معکوس / دوطرفه',
    srv_name: 'نام سرور', srv_ip: 'آدرس IP سرور', srv_port: 'پورت SSH',
    srv_user: 'نام کاربری SSH', srv_pass: 'رمز عبور SSH',
    btn_deploy: '🚀 اتصال با SSH و نصب سنتینل (۰٪ تا ۱۰۰٪)',
    enroll_title: '🔑 گام ۱: اجرای دستور سنتینل (حلقه معکوس + شنونده)',
    enroll_sub: 'این دستور را در سرور خارج اجرا کنید. هر ۱۲۰ تا ۱۵۰ ثانیه متدهای WSS، سپس Direct-TLS و L4 را تست کرده و همزمان روی پورت‌های کنترل گوش می‌دهد.',
    enroll_node_lbl: 'نام سرور سنتینل (شماره‌گذاری خودکار)',
    enroll_addr_lbl: 'آدرس و پورت‌های کنترل مستر (IP:L4:Direct:WSS)',
    enroll_cmd_lbl: 'این دستور تک‌خطی را در ترمینال سرور خارج اجرا کنید:',
    btn_rotate_tok: '🔄 تغییر توکن',
    btn_copy_enroll: '📋 کپی دستور تک‌خطی اتصال معکوس سنتینل',
    rv_title: '▶ گام ۲ (اختیاری): کاوشگر مستقیم از طرف مستر (Rendezvous)',
    rv_sub: 'اگر اتصال معکوس از سرور خارج مسدود است، آی‌پی سرور دوم را در کادر زیر وارد کرده و دکمه Play (▶) را بزنید تا مستر هم به صورت دوره‌ای تمام متدها را تست کند!',
    btn_start_rv: 'شروع کاوش مستقیم',
    network_settings: 'تنظیمات پورت پنل، پورت‌های کنترل و رمز ورود', lbl_panel_port: 'پورت وب پنل',
    lbl_panel_path: 'آدرس مخفی پنل',
    ctrl_ports_title: '🔗 پورت‌های اختصاصی کنترل و کاوش دوطرفه (6005 / 6006 / 6007)',
    lbl_ctrl_l4: 'پورت کنترل L4 (۲۰٪)', lbl_ctrl_direct: 'پورت Direct-TLS (۷۵٪)', lbl_ctrl_wss: 'پورت WSS (۸۵٪)',
    lbl_panel_user: 'نام کاربری پنل', lbl_panel_pass: 'رمز عبور جدید پنل (خالی = بدون تغییر)', save_restart: 'ذخیره و ری‌استارت سرور انتخاب‌شده',
    bbr_title: 'شتاب‌دهنده کرنل TCP BBR و FQ', bbr_sub: 'افزایش سرعت تانل و جلوگیری از افت سرعت در سرور انتخاب‌شده.',
    btn_bbr: '⚡ فعال‌سازی BBR + FQ روی سرور انتخاب‌شده',
    step1_method: '۱. انتخاب متد تانل', wss_desc: 'ارتقای وب‌سوکت HTTP/1.1 روی بستر TLS 1.3',
    direct_desc: 'استریم مستقیم TLS 1.3 بدون سربار HTTP', forward_desc: 'انتقال مستقیم لایه ۴ (مشابه socat)', stealth_lbl: 'مخفی‌سازی',
    new_tunnel: '۲. برقراری تانل بین دو سرور', reset: 'پاک کردن فرم',
    src_srv: 'سرور ورودی (Relay)', dst_srv: 'سرور خروجی (خارج)',
    tun_name: 'نام تانل', tun_port_lbl: 'پورت تانل', inb_port_lbl: 'پورت(های) فورواردینگ داخلی', spd_limit_lbl: 'سرعت (MB/s)',
    sni_lbl: 'دامنه TLS SNI (قابل تغییر)', wspath_lbl: 'مسیر وب‌سوکت (مخصوص WSS)',
    fp_lbl: 'پروفایل رمزنگاری مرورگر', seg_lbl: 'اندازه قطعه‌بندی ClientHello',
    forward_note: 'انتقال مستقیم بایت‌های TCP بدون رمزنگاری بیرونی.',
    cert_pin_lbl: 'پین گواهی SHA-256 (خودکار تنظیم می‌شود)',
    create: '⚡ برقراری تانل بین دو سرور انتخاب‌شده', active_tunnels: 'تانل‌های فعال (تست تاخیر شبکه و ویرایش)'
  }
};

function showHelpModal(key) {
  const langSet = helpTexts[lang] || helpTexts.en;
  const item = langSet[key] || helpTexts.en[key];
  if (!item) return;
  const card = document.getElementById('help-modal-card');
  if (card) card.setAttribute('dir', lang === 'fa' ? 'rtl' : 'ltr');
  document.getElementById('help-modal-title').innerText = item.title;
  document.getElementById('help-modal-body').innerText = item.body;
  document.getElementById('help-modal-btn').innerText = lang === 'fa' ? 'متوجه شدم' : 'Understood';
  document.getElementById('help-modal').classList.remove('hidden');
}

function closeHelpModal() {
  document.getElementById('help-modal').classList.add('hidden');
}

function applyLangUI() {
  const lb = document.getElementById('lang-btn');
  if (lb) lb.innerText = lang === 'en' ? 'FA' : 'EN';
  document.querySelectorAll('[data-i18n]').forEach(node => {
    const key = node.getAttribute('data-i18n');
    if (dict[lang] && dict[lang][key]) node.innerText = dict[lang][key];
  });
  if (typeof updateSpeedBadge === 'function') updateSpeedBadge();
}

function toggleLang() {
  lang = lang === 'en' ? 'fa' : 'en';
  applyLangUI();
  if (typeof savePrefsToServer === 'function') savePrefsToServer();
}

function applyTheme(name, persist) {
  currentTheme = name;
  const themes = {
    cyan:    { bg: '#0c131f', card: '#131e2f', border: '#1e2e46', accent: '#38bdf8', glow: 'rgba(56, 189, 248, 0.14)' },
    emerald: { bg: '#0b1312', card: '#12201e', border: '#1a332f', accent: '#10b981', glow: 'rgba(16, 185, 129, 0.14)' },
    amber:   { bg: '#0b111a', card: '#111a27', border: '#1e293b', accent: '#facc15', glow: 'rgba(250, 204, 21, 0.14)' },
    indigo:  { bg: '#0d1117', card: '#161b22', border: '#30363d', accent: '#818cf8', glow: 'rgba(129, 140, 248, 0.14)' },
    rose:    { bg: '#120c10', card: '#1c1318', border: '#33202a', accent: '#fb7185', glow: 'rgba(251, 113, 133, 0.14)' }
  };
  const t = themes[name] || themes.cyan;
  document.documentElement.style.setProperty('--bg', t.bg);
  document.documentElement.style.setProperty('--card', t.card);
  document.documentElement.style.setProperty('--border', t.border);
  document.documentElement.style.setProperty('--accent', t.accent);
  document.documentElement.style.setProperty('--accent-glow', t.glow);
  if (persist && typeof savePrefsToServer === 'function') savePrefsToServer();
}
