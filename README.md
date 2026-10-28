Markdown

# 🦅 Ghoghnoos Tunnel

> A resilient, stealth-focused multi-transport tunneling system and web management dashboard for Linux servers.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/Platform-Linux%20(Ubuntu%20%2F%20Debian)-orange?style=flat&logo=linux)](https://ubuntu.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## 🌟 Key Features

* **3 Built-in Transport Engines:**
  * **WSS (85% Stealth):** Standards-compliant WebSocket handshake over TLS 1.3 masqueraded under common CDN/SNI hostnames.
  * **Direct-TLS 1.3 (75% Stealth):** Zero-HTTP overhead raw TLS stream relay with selectable browser TLS cipher fingerprinting.
  * **Port Forward (Raw L4):** High-throughput TCP byte relay designed for endpoints already using outer obfuscation (such as REALITY).
* **Unified Token-Bucket Speed Limiter:** Shared bandwidth throttle per tunnel across concurrent parallel connections, preventing speedtest limit bypasses.
* **Bidirectional Rendezvous & Sentinel Mode:** Links remote nodes even when inbound SSH is blocked, supporting both active reverse dialing and passive listen-only probing.
* **Zero-Overhead 30-Day Bandwidth Tracking:** Real-time kernel counter monitoring (`/proc/net/dev`) in RAM, flushing aggregated usage to disk every hour to preserve SSD health.
* **Bilingual Interface:** Full English and Persian (RTL / دیده‌بان) localization with full-width responsive system metric charts.

---

## ⚡ Quick Start & Installation

Run this one-line command on your **Master** server (Ubuntu/Debian):

```bash
curl -Ls [https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh](https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh) | bash

Once installed, open your browser and navigate to:
Plaintext

http://<YOUR-SERVER-IP>:6001/admin/

    Default Username: admin

    Default Password: admin

🖥️ Sentinel Remote Setup (Exit Node)

To connect a remote exit node without sharing SSH credentials, run the enrollment command generated inside your dashboard's Servers tab:
Bash

curl -Ls [https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh](https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh) | bash -s -- --enroll <MASTER_IP>:6005:6006:6007 <ENROLL_TOKEN> Server-2

During enrollment, you can select:

    Bidirectional Mode (Default): Connects outbound to Master while keeping local fallback listeners active.

    Listen-Only Mode: Keeps passive listeners open, awaiting Master's forward rendezvous probe.

⌨️ CLI Management Menu

You can manage the service, view logs, change credentials, or update the binary at any time from your SSH terminal by running:
Bash

ghoghnoos

🤝 Acknowledgments & Collaboration

    Author & Maintainer: @sahand363

    Code Collaboration: Developed in partnership with Google Gemini.

Plaintext

Co-authored-by: Gemini <gemini@google.com>

📄 License

This project is licensed under the MIT License.
