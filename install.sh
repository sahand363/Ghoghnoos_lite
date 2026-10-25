#!/usr/bin/env bash
set -e

INSTALL_DIR="/opt/litetun"
BIN_PATH="$INSTALL_DIR/ghoghnoos_lite"
CONF_FILE="$INSTALL_DIR/config.env"
SERVICE_FILE="/etc/systemd/system/litetun.service"
REPO_URL="https://github.com/sahand363/ghoghnoos_light"

mkdir -p "$INSTALL_DIR"

write_systemd_unit() {
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Ghoghnoos-lite (LiteTun) Multi-Engine Service
After=network.target

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
EnvironmentFile=-$CONF_FILE
ExecStart=$BIN_PATH
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable litetun >/dev/null 2>&1 || true
}

# 1. Reverse Sentinel Enrollment Mode:
# curl -Ls .../install.sh | bash -s -- --enroll <MasterIP:Port> <Token> [NodeName]
if [[ "$1" == "--enroll" ]]; then
  MASTER_TARGET="$2"
  TOKEN="$3"
  NODE_NAME="${4:-Sentinel-Node}"
  echo -e "\033[1;36m[*] Downloading Ghoghnoos-lite Sentinel Agent...\033[0m"
  systemctl stop litetun 2>/dev/null || true
  curl -Ls "$REPO_URL/releases/latest/download/ghoghnoos_lite" -o "$BIN_PATH.tmp"
  mv -f "$BIN_PATH.tmp" "$BIN_PATH"
  chmod +x "$BIN_PATH"
  curl -Ls "$REPO_URL/raw/main/install.sh" | tr -d '\r' > /usr/local/bin/litetun
  chmod +x /usr/local/bin/litetun

  # Run interactive ANSI Dial-In Matrix (tests Direct-TLS -> WSS -> L4 Control API)
  "$BIN_PATH" --enroll "$MASTER_TARGET" "$TOKEN" "$NODE_NAME"

  write_systemd_unit
  systemctl restart litetun
  echo -e "\033[1;32m[✓] Sentinel enrolled & background Survival Loop daemonized!\033[0m"
  exit 0
fi

# 2. Automated SSH Sentinel Mode
if [[ "$1" == "--sentinel" ]]; then
  if [[ ! -f "$CONF_FILE" ]]; then
    echo -e "PANEL_PORT=6001\nPANEL_PATH=sentinel\nPANEL_USER=admin\nPANEL_PASS=admin" > "$CONF_FILE"
  fi
  write_systemd_unit
  exit 0
fi

# 3. Interactive CLI Management Menu (`litetun`)
if [[ ! -f "$BIN_PATH" ]]; then
  echo -e "\033[1;36m[*] Installing Ghoghnoos-lite for the first time...\033[0m"
  curl -Ls "$REPO_URL/releases/latest/download/ghoghnoos_lite" -o "$BIN_PATH"
  chmod +x "$BIN_PATH"
  RAND_PATH=$(head -c 4 /dev/urandom | xxd -p 2>/dev/null || echo "admin88")
  echo -e "PANEL_PORT=6001\nPANEL_PATH=$RAND_PATH\nPANEL_USER=admin\nPANEL_PASS=admin" > "$CONF_FILE"
  curl -Ls "$REPO_URL/raw/main/install.sh" | tr -d '\r' > /usr/local/bin/litetun
  chmod +x /usr/local/bin/litetun
  write_systemd_unit
  systemctl restart litetun
fi

source "$CONF_FILE" 2>/dev/null || true
SERVER_IP=$(hostname -I | awk '{print $1}')

echo -e "\033[1;36m====================================================\033[0m"
echo -e "\033[1;33m   🦅 Ghoghnoos-lite (LiteTun) Management CLI       \033[0m"
echo -e "\033[1;36m====================================================\033[0m"
echo -e " Panel URL  : \033[1;32mhttp://${SERVER_IP}:${PANEL_PORT:-6001}/${PANEL_PATH}/\033[0m"
echo -e " Username   : \033[1;37m${PANEL_USER:-admin}\033[0m"
echo -e "----------------------------------------------------"
echo -e " 1) Restart Service"
echo -e " 2) Change Panel Port / Path / Credentials"
echo -e " 3) View Live Service Logs"
echo -e " 4) Update Binary from GitHub Releases"
echo -e " 5) Uninstall Ghoghnoos-lite"
echo -e " 0) Exit"
echo -e "----------------------------------------------------"
read -rp "Select option [0-5]: " OPT

case "$OPT" in
  1)
    systemctl restart litetun
    echo -e "\033[1;32m[✓] Service restarted.\033[0m"
    ;;
  2)
    read -rp "New Panel Port [${PANEL_PORT:-6001}]: " NEW_PORT
    read -rp "New Secret Path [${PANEL_PATH}]: " NEW_PATH
    read -rp "New Username [${PANEL_USER:-admin}]: " NEW_USER
    read -rp "New Password [${PANEL_PASS:-admin}]: " NEW_PASS
    PANEL_PORT="${NEW_PORT:-${PANEL_PORT:-6001}}"
    PANEL_PATH="${NEW_PATH:-$PANEL_PATH}"
    PANEL_USER="${NEW_USER:-${PANEL_USER:-admin}}"
    PANEL_PASS="${NEW_PASS:-${PANEL_PASS:-admin}}"
    cat > "$CONF_FILE" <<EOF
PANEL_PORT=$PANEL_PORT
PANEL_PATH=$PANEL_PATH
PANEL_USER=$PANEL_USER
PANEL_PASS=$PANEL_PASS
EOF
    systemctl restart litetun
    echo -e "\033[1;32m[✓] Updated! New URL: http://${SERVER_IP}:${PANEL_PORT}/${PANEL_PATH}/\033[0m"
    ;;
  3)
    journalctl -u litetun -n 40 --no-pager
    ;;
  4)
    echo -e "\033[1;36m[*] Downloading latest release from GitHub...\033[0m"
    systemctl stop litetun 2>/dev/null || true
    curl -Ls "$REPO_URL/releases/latest/download/ghoghnoos_lite" -o "$BIN_PATH.tmp"
    mv -f "$BIN_PATH.tmp" "$BIN_PATH"
    chmod +x "$BIN_PATH"
    curl -Ls "$REPO_URL/raw/main/install.sh" | tr -d '\r' > /usr/local/bin/litetun
    chmod +x /usr/local/bin/litetun
    systemctl restart litetun
    echo -e "\033[1;32m[✓] Updated and restarted!\033[0m"
    ;;
  5)
    systemctl stop litetun 2>/dev/null || true
    systemctl disable litetun 2>/dev/null || true
    rm -rf "$SERVICE_FILE" "$INSTALL_DIR" /usr/local/bin/litetun
    systemctl daemon-reload
    echo -e "\033[1;31m[✓] Uninstalled completely.\033[0m"
    ;;
  *)
    exit 0
    ;;
esac
