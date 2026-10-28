#!/usr/bin/env bash
set -e

INSTALL_DIR="/opt/ghoghnoos"
BIN_PATH="$INSTALL_DIR/ghoghnoostun"
CONF_FILE="$INSTALL_DIR/config.env"
SERVICE_FILE="/etc/systemd/system/ghoghnoos.service"
REPO_URL="https://github.com/sahand363/ghoghnoos-tunnel"
CLI_PATH="/usr/local/bin/ghoghnoos"

mkdir -p "$INSTALL_DIR"

if [[ ! -e "/opt/litetun" ]]; then
  ln -s "$INSTALL_DIR" /opt/litetun 2>/dev/null || true
fi

write_systemd_unit() {
  printf "[Unit]\nDescription=Ghoghnoos Tunnel Core Service\nAfter=network.target\n\n[Service]\nType=simple\nWorkingDirectory=%s\nEnvironmentFile=-%s\nExecStart=%s\nRestart=always\nRestartSec=3\nLimitNOFILE=1048576\n\n[Install]\nWantedBy=multi-user.target\n" "$INSTALL_DIR" "$CONF_FILE" "$BIN_PATH" > "$SERVICE_FILE"
  systemctl daemon-reload
  systemctl enable ghoghnoos >/dev/null 2>&1 || true
}

save_config() {
  printf "PANEL_PORT=%s\nPANEL_PATH=%s\nPANEL_USER=%s\nPANEL_PASS=%s\nPANEL_DOMAIN=%s\nPANEL_SSL_CERT=%s\nPANEL_SSL_KEY=%s\nCTRL_L4_PORT=%s\nCTRL_DIRECT_PORT=%s\nCTRL_WSS_PORT=%s\nCTRL_METHODS=%s\n" \
    "${PANEL_PORT:-6001}" \
    "${PANEL_PATH:-admin}" \
    "${PANEL_USER:-admin}" \
    "${PANEL_PASS:-admin}" \
    "${PANEL_DOMAIN:-}" \
    "${PANEL_SSL_CERT:-}" \
    "${PANEL_SSL_KEY:-}" \
    "${CTRL_L4_PORT:-6005}" \
    "${CTRL_DIRECT_PORT:-6006}" \
    "${CTRL_WSS_PORT:-6007}" \
    "${CTRL_METHODS:-wss,direct}" > "$CONF_FILE"
}

cleanup_legacy() {
  systemctl stop litetun 2>/dev/null || true
  systemctl disable litetun 2>/dev/null || true
  rm -f /etc/systemd/system/litetun.service /usr/local/bin/litetun "$INSTALL_DIR/ghoghnoos-tunnel"
}

download_binary() {
  curl -Ls "$REPO_URL/releases/latest/download/ghoghnoostun" -o "$BIN_PATH.tmp"
  if [[ ! -s "$BIN_PATH.tmp" ]]; then
    curl -Ls "$REPO_URL/releases/latest/download/ghoghnoos-tunnel" -o "$BIN_PATH.tmp"
  fi
  mv -f "$BIN_PATH.tmp" "$BIN_PATH"
  chmod +x "$BIN_PATH"
}

# 1. Reverse Sentinel Enrollment Mode
if [[ "$1" == "--enroll" ]]; then
  cleanup_legacy
  MASTER_TARGET="$2"
  TOKEN="$3"
  NODE_NAME="${4:-Sentinel-Node}"
  echo -e "\033[1;36m[*] Downloading Ghoghnoos Tunnel Sentinel Agent...\033[0m"
  systemctl stop ghoghnoos 2>/dev/null || true
  download_binary
  curl -Ls "$REPO_URL/raw/main/install.sh?v=$(date +%s)" | tr -d '\r' > "$CLI_PATH"
  chmod +x "$CLI_PATH"

  "$BIN_PATH" --enroll "$MASTER_TARGET" "$TOKEN" "$NODE_NAME"

  write_systemd_unit
  systemctl restart ghoghnoos
  echo -e "\033[1;32m[✓] Sentinel enrolled & background Survival Loop daemonized!\033[0m"
  exit 0
fi

# 2. Automated SSH Sentinel Mode
if [[ "$1" == "--sentinel" ]]; then
  cleanup_legacy
  if [[ ! -f "$CONF_FILE" ]]; then
    PANEL_PORT=6001
    PANEL_PATH=sentinel
    PANEL_USER=admin
    PANEL_PASS=admin
    save_config
  fi
  write_systemd_unit
  exit 0
fi

# 3. First-Time Fresh Install
if [[ ! -f "$BIN_PATH" ]] || [[ ! -f "$CONF_FILE" ]]; then
  cleanup_legacy
  echo -e "\033[1;36m====================================================\033[0m"
  echo -e "\033[1;33m     🦅 Ghoghnoos Tunnel — Initial Setup Wizard     \033[0m"
  echo -e "\033[1;36m====================================================\033[0m"

  echo -e "\033[1;36m[*] Downloading latest Ghoghnoos Tunnel binary...\033[0m"
  download_binary

  if [[ -f "$CONF_FILE" ]]; then
    source "$CONF_FILE" 2>/dev/null || true
  else
    RAND_PATH=$(head -c 4 /dev/urandom | xxd -p 2>/dev/null || echo "admin88")
    INIT_PORT=""
    INIT_USER=""
    INIT_PASS=""

    if [[ -e /dev/tty ]]; then
      echo -e "\033[1;32mPlease choose your initial Web Panel login credentials:\033[0m"
      read -rp "  ➤ Web Panel Port [default: 6001]: " INIT_PORT </dev/tty
      read -rp "  ➤ Panel Username [default: admin]: " INIT_USER </dev/tty
      while [[ -z "$INIT_PASS" ]]; do
        read -rp "  ➤ Panel Password (required): " INIT_PASS </dev/tty
        if [[ -z "$INIT_PASS" ]]; then
          echo -e "\033[1;31m    [!] Password cannot be empty. Please enter a password.\033[0m"
        fi
      done
    fi

    PANEL_PORT="${INIT_PORT:-6001}"
    PANEL_PATH="$RAND_PATH"
    PANEL_USER="${INIT_USER:-admin}"
    PANEL_PASS="${INIT_PASS:-admin}"
    save_config
  fi

  curl -Ls "$REPO_URL/raw/main/install.sh?v=$(date +%s)" | tr -d '\r' > "$CLI_PATH"
  chmod +x "$CLI_PATH"
  write_systemd_unit
  systemctl restart ghoghnoos

  SERVER_IP=$(hostname -I | awk '{print $1}')
  SCHEME="http"
  HOST_TARGET="$SERVER_IP"
  if [[ -n "$PANEL_DOMAIN" ]] && [[ -n "$PANEL_SSL_CERT" ]]; then
    SCHEME="https"
    HOST_TARGET="$PANEL_DOMAIN"
  fi

  echo ""
  echo -e "\033[1;32m[✓] Ghoghnoos Tunnel Installed Successfully!\033[0m"
  echo -e "\033[1;36m----------------------------------------------------\033[0m"
  echo -e " 🌐 Panel URL : \033[1;32m${SCHEME}://${HOST_TARGET}:${PANEL_PORT}/${PANEL_PATH}/\033[0m"
  echo -e " 👤 Username  : \033[1;37m${PANEL_USER}\033[0m"
  echo -e " 💻 SSH Menu  : Type '\033[1;33mghoghnoos\033[0m' anytime in SSH"
  echo -e "\033[1;36m----------------------------------------------------\033[0m"
  exit 0
fi

# 4. Interactive CLI Management Menu (`ghoghnoos`)
source "$CONF_FILE" 2>/dev/null || true
SERVER_IP=$(hostname -I | awk '{print $1}')

SCHEME="http"
HOST_TARGET="$SERVER_IP"
SSL_STATUS="\033[1;33mDisabled (HTTP)\033[0m"
if [[ -n "$PANEL_DOMAIN" ]] && [[ -f "$PANEL_SSL_CERT" ]]; then
  SCHEME="https"
  HOST_TARGET="$PANEL_DOMAIN"
  SSL_STATUS="\033[1;32mEnabled (HTTPS - ${PANEL_DOMAIN})\033[0m"
fi

echo -e "\033[1;36m====================================================\033[0m"
echo -e "\033[1;33m       🦅 Ghoghnoos Tunnel Management CLI          \033[0m"
echo -e "\033[1;36m====================================================\033[0m"
echo -e " Panel URL   : \033[1;32m${SCHEME}://${HOST_TARGET}:${PANEL_PORT:-6001}/${PANEL_PATH}/\033[0m"
echo -e " Username    : \033[1;37m${PANEL_USER:-admin}\033[0m"
echo -e " SSL Status  : ${SSL_STATUS}"
echo -e "----------------------------------------------------"
echo -e " 1) Restart Service"
echo -e " 2) Change Panel Username & Password"
echo -e " 3) Change Panel Port & Secret URL Path"
echo -e " 4) Setup / Renew Domain SSL (Let's Encrypt Certbot)"
echo -e " 5) View Live Service Logs"
echo -e " 6) Update Binary from GitHub Releases"
echo -e " 7) Uninstall Ghoghnoos Tunnel"
echo -e " 0) Exit"
echo -e "----------------------------------------------------"
read -rp "Select option [0-7]: " OPT </dev/tty

case "$OPT" in
  1)
    systemctl restart ghoghnoos
    echo -e "\033[1;32m[✓] Service restarted.\033[0m"
    ;;
  2)
    echo -e "\033[1;36m--- Change Login Credentials ---\033[0m"
    read -rp "New Username [current: ${PANEL_USER:-admin}]: " NEW_USER </dev/tty
    read -rp "New Password [leave blank to keep current]: " NEW_PASS </dev/tty
    PANEL_USER="${NEW_USER:-${PANEL_USER:-admin}}"
    PANEL_PASS="${NEW_PASS:-${PANEL_PASS:-admin}}"
    save_config
    systemctl restart ghoghnoos
    echo -e "\033[1;32m[✓] Credentials updated! Username: ${PANEL_USER}\033[0m"
    ;;
  3)
    echo -e "\033[1;36m--- Change Panel Port & Path ---\033[0m"
    read -rp "New Panel Port [current: ${PANEL_PORT:-6001}]: " NEW_PORT </dev/tty
    read -rp "New Secret Path [current: ${PANEL_PATH}]: " NEW_PATH </dev/tty
    PANEL_PORT="${NEW_PORT:-${PANEL_PORT:-6001}}"
    PANEL_PATH="${NEW_PATH:-$PANEL_PATH}"
    save_config
    systemctl restart ghoghnoos
    echo -e "\033[1;32m[✓] Updated! New URL: ${SCHEME}://${HOST_TARGET}:${PANEL_PORT}/${PANEL_PATH}/\033[0m"
    ;;
  4)
    echo -e "\033[1;36m--- Domain & Let's Encrypt SSL Setup (Certbot) ---\033[0m"
    echo -e " 1) Issue / Activate SSL for a Domain (HTTPS)"
    echo -e " 2) Disable SSL and revert to IP (HTTP)"
    read -rp "Select SSL action [1-2]: " SSL_ACT </dev/tty
    if [[ "$SSL_ACT" == "2" ]]; then
      PANEL_DOMAIN=""
      PANEL_SSL_CERT=""
      PANEL_SSL_KEY=""
      rm -f /etc/cron.d/ghoghnoos-ssl
      save_config
      systemctl restart ghoghnoos
      echo -e "\033[1;33m[✓] SSL disabled. Reverted to HTTP: http://${SERVER_IP}:${PANEL_PORT}/${PANEL_PATH}/\033[0m"
      exit 0
    fi

    read -rp "Enter your Domain Name (e.g. panel.example.com): " INPUT_DOMAIN </dev/tty
    if [[ -z "$INPUT_DOMAIN" ]]; then
      echo -e "\033[1;31m[!] Domain cannot be empty.\033[0m"
      exit 1
    fi
    read -rp "Enter Email for Let's Encrypt notices [optional, press Enter to skip]: " INPUT_EMAIL </dev/tty

    if ! command -v certbot >/dev/null 2>&1; then
      echo -e "\033[1;36m[*] Installing Certbot...\033[0m"
      apt-get update -y >/dev/null 2>&1 || true
      apt-get install -y certbot >/dev/null 2>&1
    fi

    echo -e "\033[1;36m[*] Requesting Let's Encrypt SSL certificate for ${INPUT_DOMAIN}...\033[0m"
     EMAIL_ARG="--register-unsafely-without-email"
    if [[ -n "$INPUT_EMAIL" ]]; then
      EMAIL_ARG="--email $INPUT_EMAIL"
    fi

    certbot certonly --standalone -d "$INPUT_DOMAIN" --non-interactive --agree-tos $EMAIL_ARG

    CERT_LIVE="/etc/letsencrypt/live/$INPUT_DOMAIN/fullchain.pem"
    KEY_LIVE="/etc/letsencrypt/live/$INPUT_DOMAIN/privkey.pem"

    if [[ -f "$CERT_LIVE" ]] && [[ -f "$KEY_LIVE" ]]; then
      cp -f "$CERT_LIVE" "$INSTALL_DIR/ssl_cert.pem"
      cp -f "$KEY_LIVE" "$INSTALL_DIR/ssl_key.pem"
      chmod 600 "$INSTALL_DIR/ssl_key.pem"

      PANEL_DOMAIN="$INPUT_DOMAIN"
      PANEL_SSL_CERT="$INSTALL_DIR/ssl_cert.pem"
      PANEL_SSL_KEY="$INSTALL_DIR/ssl_key.pem"
      save_config

      # Setup automatic daily cron renewal check & service reload hook
      printf "0 3 * * * root certbot renew --quiet --deploy-hook 'cp -f %s %s/ssl_cert.pem && cp -f %s %s/ssl_key.pem && systemctl restart ghoghnoos'\n" "$CERT_LIVE" "$INSTALL_DIR" "$KEY_LIVE" "$INSTALL_DIR" > /etc/cron.d/ghoghnoos-ssl
      chmod 644 /etc/cron.d/ghoghnoos-ssl

      systemctl restart ghoghnoos
      echo ""
      echo -e "\033[1;32m[✓] SSL Certificate Issued & Auto-Renewal Cron Configured!\033[0m"
      echo -e " 🔒 Secure Panel URL: \033[1;32mhttps://${PANEL_DOMAIN}:${PANEL_PORT}/${PANEL_PATH}/\033[0m"
    else
      echo -e "\033[1;31m[!] Failed to locate issued certificate files. Ensure ${INPUT_DOMAIN} points to ${SERVER_IP} and port 80 is open.\033[0m"
    fi
    ;;
  5)
    journalctl -u ghoghnoos -n 50 -f
    ;;
  6)
    echo -e "\033[1;36m[*] Downloading latest release from GitHub...\033[0m"
    systemctl stop ghoghnoos 2>/dev/null || true
    download_binary
    curl -Ls "$REPO_URL/raw/main/install.sh?v=$(date +%s)" | tr -d '\r' > "$CLI_PATH"
    chmod +x "$CLI_PATH"
    systemctl restart ghoghnoos
    echo -e "\033[1;32m[✓] Updated and restarted! (Credentials & SSL preserved)\033[0m"
    ;;
  7)
    systemctl stop ghoghnoos 2>/dev/null || true
    systemctl disable ghoghnoos 2>/dev/null || true
    rm -rf "$SERVICE_FILE" "$INSTALL_DIR" "$CLI_PATH" /opt/litetun /etc/cron.d/ghoghnoos-ssl
    cleanup_legacy
    systemctl daemon-reload
    echo -e "\033[1;31m[✓] Uninstalled completely.\033[0m"
    ;;
  *)
    exit 0
    ;;
esac
