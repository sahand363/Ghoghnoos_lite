# 🦅 Ghoghnoos Tunnel

> A resilient, stealth-focused multi-transport tunneling system and web management dashboard for Linux servers.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/Platform-Linux%20(Ubuntu%20%2F%20Debian)-orange?style=flat&logo=linux)](https://ubuntu.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## 🌟 Key Features

* **3 Built-in Transport Engines:**
  * **WSS (85% Stealth):** Standards-compliant WebSocket handshake over TLS 1.3 masqueraded under common CDN/SNI domains.
  * **Direct-TLS 1.3 (75% Stealth):** Raw TLS stream relay with configurable fingerprinting and zero HTTP overhead.
  * **Port Forward (Raw L4):** Ultra-fast TCP stream relay designed for endpoints already employing outer obfuscation (such as REALITY).
* **Unified Token-Bucket Speed Limiter:** Shared rate-limiting per tunnel across concurrent parallel streams, eliminating speedtest bypasses.
* **Bidirectional Rendezvous & Sentinel Mode:** Connects remote servers even when standard inbound SSH is filtered, supporting active reverse dialing or passive listen-only mode.
* **Zero-Overhead 30-Day Bandwidth Tracking:** In-memory kernel counter aggregations flushing to disk at 1-hour intervals with minimal CPU and SSD impact.
* **Bilingual Interface:** Full English and Persian (RTL / دیده‌بان) localization support with responsive system metric charts.

---

## ⚡ Quick Start & Installation
