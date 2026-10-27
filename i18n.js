const dict = {
  en: {
    servers: 'Servers', config: 'Config', tunnels: 'Tunnels', logs: 'Log',
    add_srv_title: 'Connect Remote Server & Deploy Sentinel',
    add_srv_sub: 'Choose Direct SSH Push or Bidirectional Rendezvous (when SSH is filtered).',
    mode_ssh_btn: '🚀 Method A: SSH Push',
    mode_enroll_btn: '🔑 Method B: Reverse / Rendezvous',
    srv_name: 'Server Name', srv_ip: 'Server IP Address', srv_port: 'SSH Port',
    srv_user: 'SSH Username', srv_pass: 'SSH Password',
    btn_deploy: '🚀 Connect via SSH & Deploy Sentinel (0% ➔ 100%)',
    enroll_title: '🔑 Step 1: Run Sentinel One-Liner (Interactive Mode & Port Selection)',
    enroll_sub: 'Run this command on your remote VPS. It lets you choose between [1] Connect & Listen (Default) or [2] Listen Only (Passive Mode) and customize control ports.',
    enroll_node_lbl: 'New Sentinel Name (Auto-Numbered)',
    enroll_addr_lbl: 'Master Control Target (IP:L4:Direct:WSS)',
    enroll_cmd_lbl: 'Paste this One-Liner on your Remote Server Terminal:',
    btn_rotate_tok: '🔄 Rotate Token',
    btn_copy_enroll: '📋 Copy Reverse Enrollment One-Liner Command',
    rv_title: '▶ Step 2 (Optional): Master Forward Rendezvous Prober',
    rv_sub: 'If reverse dial-in is blocked (or Sentinel was installed in Listen-Only mode), enter the remote server IP below and press Play (▶). Master will probe enabled methods until connected!',
    btn_start_rv: 'Start Forward Probe',
    network_settings: 'Server Port, Control Ports & Login Configuration',
    lbl_panel_port: 'Web Panel Port', lbl_panel_path: 'Secret URL Path',
    ctrl_ports_title: '🔗 Dedicated Control & Rendezvous Ports (Check to Enable Method)',
    lbl_ctrl_wss: 'WSS Port (85%)', lbl_ctrl_direct: 'Direct-TLS (75%)', lbl_ctrl_l4: 'L4 Raw Port (20%)',
    lbl_panel_user: 'Panel Username', lbl_panel_pass: 'New Panel Password (blank = keep current)',
    save_restart: 'Save & Restart Selected Server',
    bbr_title: 'TCP BBR & FQ Kernel Acceleration',
    bbr_sub: 'Boosts tunnel throughput and eliminates packet-loss throttling on the selected server.',
    btn_bbr: '⚡ Enable Google BBR + FQ on Selected Server',
    step1_method: '1. Select Tunnel Transport Method',
    stealth_lbl: 'Stealth',
    wss_desc: 'HTTP/1.1 WebSocket Upgrade over TLS 1.3',
    direct_desc: 'Zero-HTTP Raw TLS 1.3 Stream Relay',
    forward_desc: 'Raw L4 TCP Relay (Native socat stream)',
    forward_note: 'Direct byte relay from Entry port straight to Exit port without outer TLS wrapping. Fastest speed when your inbound proxy already uses REALITY or TLS.',
    new_tunnel: '2. Bridge Tunnel Between Servers', reset: 'Clear Form',
    src_srv: 'Entry Server (Relay)', dst_srv: 'Exit Server (Target Node)',
    tun_name: 'Tunnel Name', tun_port_lbl: 'Tunnel Port',
    inb_port_lbl: 'Forwarding Inbound Port(s)', spd_limit_lbl: 'Speed (MB/s)',
    sni_lbl: 'TLS SNI Hostname (ServerName)', wspath_lbl: 'WebSocket Path (WSS Mode)',
    fp_lbl: 'TLS Cipher Suite Profile', seg_lbl: 'TCP ClientHello Segment Size',
    cert_pin_lbl: 'Pinned SHA-256 Certificate (Auto-filled from Exit Server)',
    create: '⚡ Establish Tunnel Between Selected Servers',
    active_tunnels: 'Active Tunnels (End-to-End Latency Ping & Edit)'
  },
  fa: {
    servers: 'سرورها', config: 'تنظیمات', tunnels: 'تانل‌ها', logs: 'لاگ',
    add_srv_title: 'اتصال به سرور جدید و نصب دیده‌بان',
    add_srv_sub: 'نصب مستقیم با SSH یا کاوش دوطرفه هوشمند (زمانی که SSH مسدود است).',
    mode_ssh_btn: '🚀 روش اول: نصب با SSH',
    mode_enroll_btn: '🔑 روش دوم: اتصال معکوس / دوطرفه',
    srv_name: 'نام سرور', srv_ip: 'آدرس IP سرور', srv_port: 'پورت SSH',
    srv_user: 'نام کاربری SSH', srv_pass: 'رمز عبور SSH',
    btn_deploy: '🚀 اتصال با SSH و نصب دیده‌بان (۰٪ تا ۱۰۰٪)',
    enroll_title: '🔑 گام ۱: اجرای دستور تک‌خطی دیده‌بان (انتخاب حالت و پورت‌ها)',
    enroll_sub: 'این دستور را در ترمینال سرور خارج اجرا کنید. امکان انتخاب بین [1] اتصال دوطرفه (پیش‌فرض) یا [2] فقط شنود (Listen Only) و تغییر پورت‌ها را به شما می‌دهد.',
    enroll_node_lbl: 'نام سرور دیده‌بان (شماره‌گذاری خودکار)',
    enroll_addr_lbl: 'آدرس و پورت‌های سرور مستر (IP:L4:Direct:WSS)',
    enroll_cmd_lbl: 'این دستور تک‌خطی را در ترمینال سرور خارج پیست کنید:',
    btn_rotate_tok: '🔄 تغییر توکن امنیتی',
    btn_copy_enroll: '📋 کپی دستور تک‌خطی نصب دیده‌بان',
    rv_title: '▶ گام ۲ (اختیاری): کاوشگر مستقیم مستر به دیده‌بان',
    rv_sub: 'اگر اتصال معکوس مسدود است (یا دیده‌بان روی حالت فقط شنود نصب شده)، IP سرور دیده‌بان را وارد کرده و دکمه (▶) را بزنید تا مستر به آن متصل شود!',
    btn_start_rv: 'شروع کاوش مستقیم',
    network_settings: 'تنظیمات پورت پنل، پورت‌های کنترل و ورود',
    lbl_panel_port: 'پورت وب پنل', lbl_panel_path: 'مسیر مخفی پنل (URL Path)',
    ctrl_ports_title: '🔗 پورت‌های اختصاصی کنترل و کاوش دوطرفه (تیک = فعال)',
    lbl_ctrl_wss: 'پورت WSS (٪۸۵)', lbl_ctrl_direct: 'پورت Direct-TLS (٪۷۵)', lbl_ctrl_l4: 'پورت L4 خام (٪۲۰)',
    lbl_panel_user: 'نام کاربری پنل', lbl_panel_pass: 'رمز عبور جدید پنل (خالی = بدون تغییر)',
    save_restart: 'ذخیره و ری‌استارت سرور انتخاب‌شده',
    bbr_title: 'شتاب‌دهنده هسته لینوکس (Google BBR + FQ)',
    bbr_sub: 'افزایش سرعت تانل و جلوگیری از افت سرعت ناشی از پکت‌لاس روی سرور انتخاب‌شده.',
    btn_bbr: '⚡ فعال‌سازی Google BBR + FQ روی سرور انتخاب‌شده',
    step1_method: '۱. انتخاب روش و موتور انتقال تانل',
    stealth_lbl: 'مخفی‌سازی',
    wss_desc: 'ارتقای WebSocket روی بستر TLS 1.3',
    direct_desc: 'استریم مستقیم TLS 1.3 بدون سربار HTTP',
    forward_desc: 'انتقال مستقیم لایه ۴ (مشابه socat)',
    forward_note: 'انتقال مستقیم بایت‌ها از پورت ورودی به پورت خروجی بدون رمزنگاری اضافه.',
    new_tunnel: '۲. برقراری تانل بین دو سرور', reset: 'پاک کردن فرم',
    src_srv: 'سرور ورودی (Relay)', dst_srv: 'سرور خروجی (خارج)',
    tun_name: 'نام تانل', tun_port_lbl: 'پورت تانل',
    inb_port_lbl: 'پورت(های) فورواردینگ داخلی', spd_limit_lbl: 'سرعت (MB/s)',
    sni_lbl: 'دامنه TLS SNI (قابل تغییر)', wspath_lbl: 'مسیر وب‌سوکت (مخصوص WSS)',
    fp_lbl: 'پروفایل رمزنگاری مرورگر', seg_lbl: 'اندازه قطعه‌بندی ClientHello',
    cert_pin_lbl: 'پین گواهی SHA-256 (خودکار از سرور دیده‌بان)',
    create: '⚡ برقراری تانل بین دو سرور انتخاب‌شده',
    active_tunnels: 'تانل‌های فعال (تست تاخیر شبکه و ویرایش)'
  }
};

const helpTexts = {
  en: {
    enroll: ['Bidirectional Rendezvous & Listen-Only Mode', 'When you run this one-liner on your remote Sentinel server:\n\n1. You can choose [1] Connect & Listen (Default) or [2] Listen Only (Passive Mode).\n2. It opens 3 dedicated local listeners (:6007 WSS, :6006 Direct-TLS, :6005 L4) so Master can probe Sentinel directly.'],
    rendezvous: ['Master Forward Rendezvous Prober (▶)', 'When enabled, Master actively probes the remote Sentinel server across your checked Control Transports (WSS :6007 ➔ Direct-TLS :6006 ➔ L4 :6005).'],
    ctrlports: ['Dedicated Control & Rendezvous Ports', 'Separate from your Web Panel port (:6001) so DPI probes never touch your UI:\n• WSS (:6007): 85% Stealth\n• Direct-TLS (:6006): 75% Stealth\n• L4 (:6005): 20% Raw HTTP API (Uncheck to disable)'],
    port: ['Web Panel Port', 'The TCP port used to access this control dashboard in your browser (Default: 6001).'],
    path: ['Secret URL Path', 'Optional secret folder path required to open the panel.'],
    bbr: ['TCP BBR Congestion Control', 'Google BBR maximizes throughput over high-latency links.'],
    fq: ['Fair Queueing (FQ) Packet Scheduler', 'Works alongside BBR to pace TCP packets smoothly.'],
    stealth: ['Transport Stealth Comparison', '• WSS (85%): Looks like standard HTTPS WebSocket traffic.\n• Direct-TLS (75%): Raw TLS 1.3 stream with zero HTTP overhead.\n• Port Forward (20%): Raw TCP L4 relay.'],
    sni: ['TLS SNI Hostname', 'The ServerName presented during the TLS 1.3 handshake.'],
    seg: ['TCP ClientHello Segmentation', 'Splits the initial TLS ClientHello packet into smaller TCP segments.'],
    forward: ['Raw L4 Port Forwarding', 'Relays raw TCP bytes directly without adding an outer TLS layer.'],
    pin: ['SHA-256 Certificate Pinning', 'Verifies the exact cryptographic fingerprint of your Exit Sentinel certificate.']
  },
  fa: {
    enroll: ['اتصال دوطرفه هوشمند و حالت فقط شنود (دیده‌بان)', 'با اجرای این دستور روی سرور دیده‌بان:\n\n۱. می‌توانید بین حالت [1] اتصال دوطرفه (پیش‌فرض) یا [2] فقط شنود (Listen Only) انتخاب کنید.\n۲. دیده‌بان روی پورت‌های کنترل (:6007 WSS، :6006 Direct-TLS و :6005 L4) گوش می‌دهد تا مستر بتواند مستقیماً به آن متصل شود.'],
    rendezvous: ['کاوشگر مستقیم مستر به دیده‌بان (▶)', 'با زدن دکمه Play (▶)، سرور مستر به صورت خودکار پورت‌های فعال دیده‌بان (WSS :6007 ➔ Direct :6006 ➔ L4 :6005) را کاوش می‌کند.'],
    ctrlports: ['پورت‌های اختصاصی کنترل و کاوش', 'جدا از پورت وب‌پنل (:6001) برای امنیت بیشتر:\n• پورت ۶۰۰۷ (WSS): ۸۵٪ مخفی‌سازی\n• پورت ۶۰۰۶ (Direct-TLS): ۷۵٪ مخفی‌سازی\n• پورت ۶۰۰۵ (L4): ارتباط خام (برداشتن تیک = غیرفعال)'],
    port: ['پورت وب پنل', 'پورتی که برای باز کردن این پنل در مرورگر استفاده می‌شود (پیش‌فرض: 6001).'],
    path: ['مسیر مخفی پنل', 'یک عبارت امنیتی در آدرس پنل برای جلوگیری از شناسایی پنل توسط اسکنرها.'],
    bbr: ['شتاب‌دهنده Google BBR', 'الگوریتم کنترل ازدحام گوگل که سرعت تانل را به حداکثر می‌رساند.'],
    fq: ['زمان‌بند بسته Fair Queueing (FQ)', 'در کنار BBR برای ارسال منظم بسته‌ها استفاده می‌شود.'],
    stealth: ['مقایسه درصد مخفی‌سازی تانل‌ها', '• روش WSS (٪۸۵): شبیه‌سازی کامل ترافیک وب‌سوکت HTTPS.\n• روش Direct-TLS (٪۷۵): استریم مستقیم TLS 1.3.\n• روش Port Forward (٪۲۰): انتقال خام لایه ۴.'],
    sni: ['دامنه TLS SNI', 'نام دامنه‌ای که در زمان هندشیک TLS 1.3 نمایش داده می‌شود.'],
    seg: ['قطعه‌بندی بسته ClientHello', 'بسته اولیه TLS را به قطعات کوچک‌تر تقسیم می‌کند تا فیلترینگ SNI دور زده شود.'],
    forward: ['فورواردینگ مستقیم لایه ۴', 'انتقال مستقیم بایت‌های TCP بدون لایه رمزنگاری اضافه.'],
    pin: ['پین امنیتی گواهی SHA-256', 'اثر انگشت دیجیتال گواهی سرور دیده‌بان را بررسی می‌کند تا از حملات شنود جلوگیری شود.']
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
    cyan: ['#38bdf8', 'rgba(56, 189, 248, 0.14)'],
    emerald: ['#10b981', 'rgba(16, 185, 129, 0.14)'],
    amber: ['#f59e0b', 'rgba(245, 158, 11, 0.14)'],
    indigo: ['#818cf8', 'rgba(129, 140, 248, 0.14)'],
    rose: ['#f43f5e', 'rgba(244, 63, 94, 0.14)']
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
  document.getElementById('help-modal-btn').innerText = lang === 'fa' ? 'متوجه شدم' : 'Understood';
  document.getElementById('help-modal-card').dir = lang === 'fa' ? 'rtl' : 'ltr';
  document.getElementById('help-modal').classList.remove('hidden');
}

function closeHelpModal() {
  document.getElementById('help-modal').classList.add('hidden');
}
