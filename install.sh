#!/usr/bin/env bash
set -e

INSTALL_DIR="/opt/ghoghnoos"
BIN_PATH="$INSTALL_DIR/ghoghnoos-tunnel"
CONF_FILE="$INSTALL_DIR/config.env"
SERVICE_FILE="/etc/systemd/system/ghoghnoos.service"
REPO_URL="https://github.com/sahand363/ghoghnoos-tunnel"
CLI_PATH="/usr/local/bin/ghoghnoos"

mkdir -p "$INSTALL_DIR"

write_systemd_unit() {
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Ghoghnoos Tunnel Multi-Engine Service
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
systemctl enable ghoghnoos >/dev/null 2>&1 || true
}

# Cleanup older litetun service residue if present
cleanup_legacy() {
  systemctl stop litetun 2>/dev/null || true
  systemctl disable litetun 2>/dev/null || true
  rm -f /etc/systemd/system/litetun.service /usr/local/bin/litetun
}

# 1. Reverse Sentinel Enrollment Mode:
# curl -Ls .../install.sh | bash -s -- --enroll <MasterIP:Port> <Token> [NodeName]
if [[ "$1" == "--enroll" ]]; then
  cleanup_legacy
  MASTER_TARGET="$2"
  TOKEN="$3"
  NODE_NAME="${4:-Sentinel-Node}"
  echo -e "\033[1;36m[*] Downloading Ghoghnoos Tunnel Sentinel Agent...\033[0m"
  systemctl stop ghoghnoos 2>/dev/null || true
  curl -Ls "$REPO_URL/releases/latest/download/ghoghnoos-tunnel" -o "$BIN_PATH.tmp"
  mv -f "$BIN_PATH.tmp" "$BIN_PATH"
  chmod +x "$BIN_PATH"
  curl -Ls "$REPO_URL/raw/main/install.sh" | tr -d '\r' > "$CLI_PATH"
  chmod +x "$CLI_PATH"

  # Run interactive ANSI Dial-In Matrix (tests Direct-TLS -> WSS -> L4 Control API)
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
    echo -e "PANEL_PORT=6001\nPANEL_PATH=sentinel\nPANEL_USER=admin\nPANEL_PASS=admin\nCTRL_L4_PORT=6005\nCTRL_DIRECT_PORT=6006\nCTRL_WSS_PORT=6007\nCTRL_METHODS=wss,direct" > "$CONF_FILE"
  fi
  write_systemd_unit
  exit 0
fi

# 3. Interactive CLI Management Menu (`ghoghnoos`)
if [[ ! -f "$BIN_PATH" ]]; then
  cleanup_legacy
  echo -e "\033[1;36m[*] Installing Ghoghnoos Tunnel for the first time...\033[0m"
  curl -Ls "$REPO_URL/releases/latest/download/ghoghnoos-tunnel" -o "$BIN_PATH"
  chmod +x "$BIN_PATH"
  RAND_PATH=$(head -c 4 /dev/urandom | xxd -p 2>/dev/null || echo "admin88")
  echo -e "PANEL_PORT=6001\nPANEL_PATH=$RAND_PATH\nPANEL_USER=admin\nPANEL_PASS=admin\nCTRL_L4_PORT=6005\nCTRL_DIRECT_PORT=6006\nCTRL_WSS_PORT=6007\nCTRL_METHODS=wss,direct" > "$CONF_FILE"
  curl -Ls "$REPO_URL/raw/main/install.sh" | tr -d '\r' > "$CLI_PATH"
  chmod +x "$CLI_PATH"
  write_systemd_unit
  systemctl restart ghoghnoos
fi

source "$CONF_FILE" 2>/dev/null || true
SERVER_IP=$(hostname -I | awk '{print $1}')

echo -e "\033[1;36m====================================================\033[0m"
echo -e "\033[1;33m       🦅 Ghoghnoos Tunnel Management CLI          \033[0m"
echo -e "\033[1;36m====================================================\033[0m"
echo -e " Panel URL   : \033[1;32mhttp://${SERVER_IP}:${PANEL_PORT:-6001}/${PANEL_PATH}/\033[0m"
echo -e " Username    : \033[1;37m${PANEL_USER:-admin}\033[0m"
echo -e "----------------------------------------------------"
echo -e " 1) Restart Service"
echo -e " 2) Change Panel Port / Path / Credentials"
echo -e " 3) View Live Service Logs"
echo -e " 4) Update Binary from GitHub Releases"
echo -e " 5) Uninstall Ghoghnoos Tunnel"
echo -e " 0) Exit"
echo -e "----------------------------------------------------"
read -rp "Select option [0-5]: " OPT

case "$OPT" in
  1)
    systemctl restart ghoghnoos
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
CTRL_L4_PORT=${CTRL_L4_PORT:-6005}
CTRL_DIRECT_PORT=${CTRL_DIRECT_PORT:-6006}
CTRL_WSS_PORT=${CTRL_WSS_PORT:-6007}
CTRL_METHODS=${CTRL_METHODS:-wss,direct}
EOF
    systemctl restart ghoghnoos
    echo -e "\033[1;32m[✓] Updated! New URL: http://${SERVER_IP}:${PANEL_PORT}/${PANEL_PATH}/\033[0m"
    ;;
  3)
    journalctl -u ghoghnoos -n 50 -f
    ;;
  4)
    echo -e "\033[1;36m[*] Downloading latest release from GitHub...\033[0m"
    systemctl stop ghoghnoos 2>/dev/null || true
    curl -Ls "$REPO_URL/releases/latest/download/ghoghnoos-tunnel" -o "$BIN_PATH.tmp"
    mv -f "$BIN_PATH.tmp" "$BIN_PATH"
    chmod +x "$BIN_PATH"
    curl -Ls "$REPO_URL/raw/main/install.sh" | tr -d '\r' > "$CLI_PATH"
    chmod +x "$CLI_PATH"
    systemctl restart ghoghnoos
    echo -e "\033[1;32m[✓] Updated and restarted!\033[0m"
    ;;
  5)
    systemctl stop ghoghnoos 2>/dev/null || true
    systemctl disable ghoghnoos 2>/dev/null || true
    rm -rf "$SERVICE_FILE" "$INSTALL_DIR" "$CLI_PATH"
    cleanup_legacy
    systemctl daemon-reload
    echo -e "\033[1;31m[✓] Uninstalled completely.\033[0m"
    ;;
  *)
    exit 0
    ;;
esac
