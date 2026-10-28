# 🦅 Ghoghnoos Tunnel

> A resilient, stealth-focused multi-transport tunneling system and web management dashboard for Linux servers.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Platform](https://img.shields.io/badge/Platform-Linux%20(Ubuntu%20%2F%20Debian)-orange?style=flat&logo=linux)](https://ubuntu.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## 🌟 Key Features

- **3 Built-in Transport Engines**
  - **WSS :** Standards-compliant WebSocket handshake over TLS 1.3, masqueraded under common CDN/SNI hostnames.
  - **Direct-TLS 1.3 :** Zero-HTTP-overhead raw TLS stream relay with selectable browser TLS cipher fingerprinting.
  - **Port Forward :** High-throughput TCP byte relay designed for endpoints already using outer obfuscation, such as REALITY.
- **Unified Token-Bucket Speed Limiter:** Shared bandwidth throttling per tunnel across concurrent parallel connections, preventing speed-test limit bypasses.
- **Bidirectional Rendezvous & Sentinel Mode:** Connects remote nodes even when inbound SSH is blocked, supporting both active reverse dialing and passive listen-only probing.
- **30-Day Bandwidth Tracking:** Real-time kernel counter monitoring in RAM, with aggregated usage flushed to disk hourly to reduce SSD writes.
- **Bilingual Interface:** Full English and Persian localization, including RTL support and responsive, full-width system metric charts.

---

## ⚡ Quick Start & Installation

Run the following command on your **Master server** (Ubuntu/Debian):

```bash
curl -Ls https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh | bash
```

Once installed, open your browser and navigate to:

```text
http://<YOUR-SERVER-IP>:6001/admin/
```

**Default Login Credentials**

- **Username:** `admin`
- **Password:** `admin`

> ⚠️ **Security Notice:** Change the default credentials immediately after your first login. Do not expose the dashboard publicly while using default credentials.

---

## 🖥️ Sentinel Remote Setup (Exit Node)

To connect a remote exit node without sharing SSH credentials, run the enrollment command generated in your dashboard's **Servers** tab:

```bash
curl -Ls https://raw.githubusercontent.com/sahand363/ghoghnoos-tunnel/main/install.sh | bash -s -- --enroll <MASTER_IP>:6005:6006:6007 <ENROLL_TOKEN> Server-2
```

During enrollment, you can select one of the following modes:

- **Bidirectional Mode (Default):** Connects outbound to the Master while keeping local fallback listeners active.
- **Listen-Only Mode:** Keeps passive listeners open, awaiting the Master's forward rendezvous probe.

---

## ⌨️ CLI Management

Manage the service, view logs, change credentials, or update the binary directly from your SSH terminal:

```bash
ghoghnoos
```

---

## 🤝 Acknowledgments & Collaboration

- **Author & Maintainer:** [@sahand363](https://github.com/sahand363)
- **Code Collaboration:** Developed in partnership with Google Gemini.

---

## 📄 License

This project is licensed under the [MIT License](LICENSE).
