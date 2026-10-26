const helpTexts = {
  en: {
    enroll: {
      title: '🔑 Stealth-Prioritized Reverse Dial-In & 12h Auto-Upgrade',
      body: 'When SSH from Iran to Kharej is blocked or filtered, run this one-liner on the remote server.\n\n1. Stealth Priority: Tests WSS (85% Stealth on :6003) ➔ Direct-TLS (75% Stealth on :6002) ➔ L4 API (20% Stealth on :6001).\n2. Survival Loop: Sends a randomized 60–85s jittered heartbeat.\n3. 12-Hour Opportunistic Upgrade: If connected via a lower-stealth fallback, it automatically tests higher-stealth transports every 12 hours and upgrades seamlessly.'
    },
    stealth: {
      title: '🛡️ Method Stealth Percentage Explained',
      body: '• WSS (85% Stealth): Wraps traffic in TLS 1.3 + HTTP/1.1 WebSocket headers and serves a decoy 200 OK HTML webpage if probed.\n\n• Direct TLS (75% Stealth): Pure TLS 1.3 encrypted stream with browser cipher-suite profiles and TCP ClientHello segmentation, without HTTP header overhead.\n\n• Port Forward (20% Stealth): Raw L4 TCP forwarding (like socat). Has no outer encryption of its own—best used when your inbound service already uses REALITY or TLS.'
    },
    sni: {
      title: 'TLS SNI Hostname (ServerName)',
      body: 'The hostname sent inside the TLS ClientHello handshake when connecting from the Entry server to the Exit server.\n\nWorks in both WSS and Direct-TLS modes. You can set any domain (e.g., www.speedtest.net, cloudflare.com, or your own domain).'
    },
    seg: {
      title: 'TCP ClientHello Segment Size',
      body: 'Splits the initial TLS handshake packet into smaller TCP segments.\n\n• 0 = Off (Standard single-packet TCP delivery).\n• 64 or 128 = Cuts the ClientHello header across multiple TCP packets so simple single-packet DPI matchers cannot parse the handshake.'
    },
    fq: {
      title: 'FQ (Fair Queueing Scheduler)',
      body: 'A Linux kernel packet scheduler (sch_fq).\n\nUnlike standard FIFO queues, FQ paces individual network flows and eliminates bufferbloat. Google BBR requires FQ pacing to sustain maximum speed across high-latency links.'
    },
    bbr: {
      title: 'Google BBR Congestion Control',
      body: 'Replaces legacy CUBIC with model-based congestion control. BBR measures actual bottleneck bandwidth and RTT instead of assuming packet loss equals congestion—keeping cross-border speeds high.'
    },
    pin: {
      title: 'SHA-256 Certificate Pinning',
      body: 'Verifies the exact cryptographic SHA-256 fingerprint of your Exit server leaf certificate. Any intermediary attempting to intercept or MITM the TLS connection is blocked immediately.'
    },
    forward: {
      title: 'Raw Port Forwarding (L4 TCP Relay)',
      body: 'Pipes raw TCP bytes directly from the Entry port to the Exit service port (native Go replacement for socat) with integrated speed limiting and live latency monitoring.'
    },
    port: {
      title: 'Panel & Sentinel Port (Default: 6001)',
      body: 'The TCP port used for the web panel and Sentinel communication. Using 6001 (or a custom port) avoids conflicts with common web ports.'
    },
    path: {
      title: 'Secret URL Path',
      body: 'Protects your web panel behind a secret URL slug so automated scanners get a 404 Not Found.'
    }
  },
  fa: {
    enroll: {
      title: '🔑 اتصال معکوس با اولویت مخفی‌سازی و ارتقای ۱۲ ساعته',
      body: 'زمانی که پورت SSH سرور خارج مسدود یا فیلتر است، این دستور تک‌خطی را در ترمینال سرور خارج اجرا کنید.\n\n۱. اولویت مخفی‌سازی: ابتدا متد WSS (۸۵٪ روی پورت 6003)، سپس Direct-TLS (۷۵٪ روی پورت 6002) و در نهایت L4 API (۲۰٪ روی پورت 6001) را تست می‌کند.\n۲. حلقه بقا: هر ۶۰ تا ۸۵ ثانیه وضعیت را همگام‌سازی می‌کند.\n۳. ارتقای خودکار ۱۲ ساعته: اگر روی متد ضعیف‌تر متصل شده باشد، هر ۱۲ ساعت متدهای امن‌تر را بررسی کرده و به صورت خودکار به امن‌ترین متد ارتقا می‌دهد.'
    },
    stealth: {
      title: '🛡️ راهنمای درصد مخفی‌سازی (Stealth) متدها',
      body: '• متد WSS (۸۵٪ مخفی‌سازی): ترافیک را داخل TLS 1.3 و هدرهای وب‌سوکت HTTP/1.1 قرار می‌دهد.\n\n• متد Direct TLS (۷۵٪ مخفی‌سازی): استریم مستقیم رمزنگاری‌شده TLS 1.3 بدون سربار HTTP همراه با پروفایل مرورگر.\n\n• متد Port Forward (۲۰٪ مخفی‌سازی): انتقال مستقیم لایه ۴ (مشابه socat) بدون رمزنگاری بیرونی.'
    },
    sni: {
      title: 'دامنه TLS SNI (ServerName)',
      body: 'نام دامنه‌ای که در بسته اولیه TLS ClientHello ارسال می‌شود (مثلاً www.speedtest.net یا دامنه دلخواه).'
    },
    seg: {
      title: 'اندازه قطعه‌بندی TCP ClientHello',
      body: 'بسته اولیه هندشیک TLS را به قطعات کوچک‌تر تقسیم می‌کند تا سیستم‌های DPI نتوانند آن را یکجا بررسی کنند.'
    },
    fq: {
      title: 'زمان‌بند پکت FQ (Fair Queueing)',
      body: 'صف‌بندی هوشمند کرنل لینوکس برای مدیریت جریان ترافیک و حذف تاخیر بافر.'
    },
    bbr: {
      title: 'کنترل ازدحام Google BBR',
      body: 'جایگزین هوشمند الگوریتم‌های سنتی در کرنل لینوکس برای حفظ حداکثر سرعت در شرایط پکت‌لاس.'
    },
    pin: {
      title: 'پین گواهی رمزنگاری SHA-256',
      body: 'اثرانگشت دقیق گواهی TLS سرور خارج را بررسی کرده و از هرگونه حمله مرد میانی جلوگیری می‌کند.'
    },
    forward: {
      title: 'پورت فورواردینگ مستقیم (لایه ۴)',
      body: 'انتقال مستقیم بایت‌های TCP بین پورت‌های ورودی و خروجی با بالاترین بازدهی ممکن.'
    },
    port: {
      title: 'پورت پنل و سنتینل (پیش‌فرض: 6001)',
      body: 'پورت ارتباطی پنل مدیریت و ایجنت سنتینل.'
    },
    path: {
      title: 'آدرس مخفی پنل (Secret Path)',
      body: 'قرار دادن پنل مدیریت در مسیری تصادفی جهت جلوگیری از اسکن خودکار.'
    }
  }
};

const dict = {
  en: {
    servers: 'Servers', config: 'Config', tunnels: 'Tunnels', logs: 'Log',
    add_srv_title: 'Connect Remote Server & Deploy Sentinel',
    add_srv_sub: 'Choose Direct SSH Push or Reverse Token Dial-In (when SSH is filtered).',
    mode_ssh_btn: '🚀 Method A: SSH Push', mode_enroll_btn: '🔑 Method B: Reverse Dial-In',
    srv_name: 'Server Name', srv_ip: 'Server IP Address', srv_port: 'SSH Port',
    srv_user: 'SSH Username', srv_pass: 'SSH Password',
    btn_deploy: '🚀 Connect via SSH & Deploy Sentinel (0% ➔ 100%)',
    enroll_title: '🔑 Autonomous Reverse Sentinel Enrollment (Stealth Priority)',
    enroll_sub: 'If outbound SSH from Master is blocked, run this command on your remote VPS. The Sentinel tests WSS (85%) ➔ Direct-TLS (75%) ➔ L4 API (20%) and checks every 12h to upgrade to the highest available stealth transport.',
    enroll_node_lbl: 'New Sentinel Name (Auto-Numbered)',
    enroll_addr_lbl: 'Master Dial-In Target (IP:Port)',
    enroll_cmd_lbl: 'Paste this One-Liner on your Remote Server Terminal:',
    btn_rotate_tok: '🔄 Rotate Token',
    btn_copy_enroll: '📋 Copy Reverse Enrollment One-Liner Command',
    network_settings: 'Server Port, URL & Login Configuration', lbl_panel_port: 'Panel / Sentinel Port',
    lbl_panel_path: 'Secret URL Path', lbl_panel_user: 'Panel Username',
    lbl_panel_pass: 'New Panel Password (blank = keep current)', save_restart: 'Save & Restart Selected Server',
    bbr_title: 'TCP BBR & FQ Kernel Acceleration',
    bbr_sub: 'Boosts tunnel throughput and eliminates packet-loss throttling on the selected server.',
    btn_bbr: '⚡ Enable Google BBR + FQ on Selected Server',
    step1_method: '1. Select Tunnel Transport Method',
    wss_desc: 'HTTP/1.1 WebSocket Upgrade over TLS 1.3',
    direct_desc: 'Zero-HTTP Raw TLS 1.3 Stream Relay',
    forward_desc: 'Raw L4 TCP Relay (Native socat stream)',
    stealth_lbl: 'Stealth',
    new_tunnel: '2. Bridge Tunnel Between Servers', reset: 'Clear Form',
    src_srv: 'Entry Server (Relay)', dst_srv: 'Exit Server (Target Node)',
    tun_name: 'Tunnel Name', tun_port_lbl: 'Tunnel Port',
    inb_port_lbl: 'Forwarding Inbound Port(s)', spd_limit_lbl: 'Speed (MB/s)',
    sni_lbl: 'TLS SNI Hostname (ServerName)', wspath_lbl: 'WebSocket Path (WSS Mode)',
    fp_lbl: 'TLS Cipher Suite Profile', seg_lbl: 'TCP ClientHello Segment Size',
    forward_note: 'Direct byte relay from Entry port straight to Exit port without outer TLS wrapping. Fastest speed when your inbound proxy already uses REALITY or TLS.',
    cert_pin_lbl: 'Pinned SHA-256 Certificate (Auto-filled from Exit Server)',
    create: '⚡ Establish Tunnel Between Selected Servers',
    active_tunnels: 'Active Tunnels (End-to-End Latency Ping & Edit)'
  },
  fa: {
    servers: 'سرورها', config: 'تنظیمات', tunnels: 'تانل‌ها', logs: 'لاگ',
    add_srv_title: 'اتصال به سرور جدید و نصب سنتینل',
    add_srv_sub: 'نصب مستقیم با SSH یا اتصال معکوس با توکن (زمانی که SSH مسدود است).',
    mode_ssh_btn: '🚀 روش اول: نصب با SSH', mode_enroll_btn: '🔑 روش دوم: اتصال معکوس (Dial-In)',
    srv_name: 'نام سرور', srv_ip: 'آدرس IP سرور', srv_port: 'پورت SSH',
    srv_user: 'نام کاربری SSH', srv_pass: 'رمز عبور SSH',
    btn_deploy: '🚀 اتصال با SSH و نصب سنتینل (۰٪ تا ۱۰۰٪)',
    enroll_title: '🔑 ثبت‌نام معکوس سنتینل (اولویت بر اساس درصد مخفی‌سازی)',
    enroll_sub: 'اگر پورت SSH مسدود است، این دستور را در سرور خارج اجرا کنید. سنتینل به ترتیب WSS (۸۵٪)، سپس Direct-TLS (۷۵٪) و L4 API (۲۰٪) را تست کرده و هر ۱۲ ساعت امکان ارتقا به متد امن‌تر را بررسی می‌کند.',
    enroll_node_lbl: 'نام سرور سنتینل (شماره‌گذاری خودکار)',
    enroll_addr_lbl: 'آدرس و پورت سرور مستر (IP:Port)',
    enroll_cmd_lbl: 'این دستور تک‌خطی را در ترمینال سرور خارج اجرا کنید:',
    btn_rotate_tok: '🔄 تغییر توکن',
    btn_copy_enroll: '📋 کپی دستور تک‌خطی اتصال معکوس سنتینل',
    network_settings: 'تنظیمات پورت، آدرس و رمز ورود پنل', lbl_panel_port: 'پورت پنل / سنتینل',
    lbl_panel_path: 'آدرس مخفی پنل', lbl_panel_user: 'نام کاربری پنل',
    lbl_panel_pass: 'رمز عبور جدید پنل (خالی = بدون تغییر)', save_restart: 'ذخیره و ری‌استارت سرور انتخاب‌شده',
    bbr_title: 'شتاب‌دهنده کرنل TCP BBR و FQ',
    bbr_sub: 'افزایش سرعت تانل و جلوگیری از افت سرعت در سرور انتخاب‌شده.',
    btn_bbr: '⚡ فعال‌سازی BBR + FQ روی سرور انتخاب‌شده',
    step1_method: '۱. انتخاب متد تانل',
    wss_desc: 'ارتقای وب‌سوکت HTTP/1.1 روی بستر TLS 1.3',
    direct_desc: 'استریم مستقیم TLS 1.3 بدون سربار HTTP',
    forward_desc: 'انتقال مستقیم لایه ۴ (مشابه socat)',
    stealth_lbl: 'مخفی‌سازی',
    new_tunnel: '۲. برقراری تانل بین دو سرور', reset: 'پاک کردن فرم',
    src_srv: 'سرور ورودی (Relay)', dst_srv: 'سرور خروجی (خارج)',
    tun_name: 'نام تانل', tun_port_lbl: 'پورت تانل',
    inb_port_lbl: 'پورت(های) فورواردینگ داخلی', spd_limit_lbl: 'سرعت (MB/s)',
    sni_lbl: 'دامنه TLS SNI (قابل تغییر)', wspath_lbl: 'مسیر وب‌سوکت (مخصوص WSS)',
    fp_lbl: 'پروفایل رمزنگاری مرورگر', seg_lbl: 'اندازه قطعه‌بندی ClientHello',
    forward_note: 'انتقال مستقیم بایت‌های TCP بدون رمزنگاری بیرونی. بالاترین سرعت زمانی که سرویس شما خودش دارای REALITY یا TLS باشد.',
    cert_pin_lbl: 'پین گواهی SHA-256 (خودکار تنظیم می‌شود)',
    create: '⚡ برقراری تانل بین دو سرور انتخاب‌شده',
    active_tunnels: 'تانل‌های فعال (تست تاخیر شبکه و ویرایش)'
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
