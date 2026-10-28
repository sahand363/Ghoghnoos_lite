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
* **Zero-Overhead 30-Day Bandwidth Tracking:** Real-time kernel counter monitoring in RAM, flushing aggregated usage to disk every hour to preserve SSD health.
* **Bilingual Interface:** Full English and Persian (RTL / دیده‌بان) localization with full-width responsive system metric charts.

---

## ⚡ Quick Start & Installation

Run this one-line command on your **Master** server (Ubuntu/Debian):

```bash
curl -Ls [https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh](https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh) | bash


Once installed, open your browser and navigate to:

http://<YOUR-SERVER-IP>:6001/admin/
