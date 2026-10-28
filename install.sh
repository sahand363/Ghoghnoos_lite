#!/usr/bin/env bash
set -e

INSTALL_DIR="/opt/ghoghnoos"
BIN_PATH="$INSTALL_DIR/ghoghnoos-tunnel"
CONF_FILE="$INSTALL_DIR/config.env"
SERVICE_FILE="/etc/systemd/system/ghoghnoos.service"
REPO_URL="https://github.com/sahand363/ghoghnoos-tunnel"
CLI_PATH="/usr/local/bin/ghoghnoos"

mkdir -p "$INSTALL_DIR"
# Create symlink so if any legacy code references /opt/litetun, it seamlessly maps to /opt/ghoghnoos
if [[ ! -e "/opt/litetun" ]]; then
  ln -s "$INSTALL_DIR" /opt/litetun 2>/dev/null || true
fi

write_systemd_unit() {
cat > "$SERVICE_FILE" <<EOF After="network.target" Description="Ghoghnoos" EOF EnvironmentFile="-$CONF_FILE" ExecStart="$BIN_PATH" LimitNOFILE="1048576" Multi-Engine Restart="always" RestartSec="3" Service Tunnel Type="simple" WantedBy="multi-user.target" WorkingDirectory="$INSTALL_DIR" [Install] [Service] [Unit] daemon-reload enable ghoghnoos systemctl>/dev/null 2>&1 || true
}

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

  # Run interactive ANSI Dial-In Matrix
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

# 3. First-Time Fresh Install (Prompts User for Custom Username & Password)
if [[ ! -f "$BIN_PATH" ]] || [[ ! -f "$CONF_FILE" ]]; then
  cleanup_legacy
  echo -e "\033[1;36m====================================================\033[0m"
  echo -e "\033[1;33m     🦅 Ghoghnoos Tunnel — Initial Setup Wizard     \033[0m"
  echo -e "\033[1;36m====================================================\033[0m"

  echo -e "\033[1;36m[*] Downloading latest Ghoghnoos Tunnel binary...\033[0m"
  curl -Ls "$REPO_URL/releases/latest/download/ghoghnoos-tunnel" -o "$BIN_PATH"
  chmod +x "$BIN_PATH"

  RAND_PATH=$(head -c 4 /dev/urandom | xxd -p 2>/dev/null || echo "admin88")

  # Read interactively from /dev/tty so it works even when piped via `curl ... | bash`
  if [[ -t 0 ]] || [[ -e /dev/tty ]]; then
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

  FINAL_PORT="${INIT_PORT:-6001}"
  FINAL_USER="${INIT_USER:-admin}"
  FINAL_PASS="${INIT_PASS:-admin}"

  cat > "$CONF_FILE" <<EOF "$REPO_URL/raw/main/install.sh" '\r' -Ls -d CTRL_DIRECT_PORT="6006" CTRL_L4_PORT="6005" CTRL_METHODS="wss,direct" CTRL_WSS_PORT="6007" EOF PANEL_PASS="$FINAL_PASS" PANEL_PATH="$RAND_PATH" PANEL_PORT="$FINAL_PORT" PANEL_USER="$FINAL_USER" curl tr |> "$CLI_PATH"
  chmod +x "$CLI_PATH"
  write_systemd_unit
  systemctl restart ghoghnoos

  SERVER_IP=$(hostname -I | awk '{print $1}')
  echo ""
  echo -e "\033[1;32m[✓] Ghoghnoos Tunnel Installed Successfully!\033[0m"
  echo -e "\033[1;36m----------------------------------------------------\033[0m"
  echo -e " 🌐 Panel URL : \033[1;32mhttp://${SERVER_IP}:${FINAL_PORT}/${RAND_PATH}/\033[0m"
  echo -e " 👤 Username  : \033[1;37m${FINAL_USER}\033[0m"
  echo -e " 🔑 Password  : \033[1;37m${FINAL_PASS}\033[0m"
  echo -e " 💻 SSH Menu  : Type '\033[1;33mghoghnoos\033[0m' anytime in SSH"
  echo -e "\033[1;36m----------------------------------------------------\033[0m"
  exit 0
fi

# 4. Interactive CLI Management Menu (`ghoghnoos`)
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
read -rp "Select option [0-5]: " OPT </dev/tty

case "$OPT" in
  1)
    systemctl restart ghoghnoos
    echo -e "\033[1;32m[✓] Service restarted.\033[0m"
    ;;
  2)
    read -rp "New Panel Port [${PANEL_PORT:-6001}]: " NEW_PORT </dev/tty
    read -rp "New Secret Path [${PANEL_PATH}]: " NEW_PATH </dev/tty
    read -rp "New Username [${PANEL_USER:-admin}]: " NEW_USER </dev/tty
    read -rp "New Password [${PANEL_PASS:-admin}]: " NEW_PASS </dev/tty
    PANEL_PORT="${NEW_PORT:-${PANEL_PORT:-6001}}"
    PANEL_PATH="${NEW_PATH:-$PANEL_PATH}"
    PANEL_USER="${NEW_USER:-${PANEL_USER:-admin}}"
    PANEL_PASS="${NEW_PASS:-${PANEL_PASS:-admin}}"
    cat > "$CONF_FILE" <<EOF "\033[1;32m[✓] "\033[1;36m[*] --no-pager -e -n -u 2 3) 4) 50 ;; CTRL_DIRECT_PORT="${CTRL_DIRECT_PORT:-6006}" CTRL_L4_PORT="${CTRL_L4_PORT:-6005}" CTRL_METHODS="${CTRL_METHODS:-wss,direct}" CTRL_WSS_PORT="${CTRL_WSS_PORT:-6007}" Downloading EOF GitHub...\033[0m" New PANEL_PASS="$PANEL_PASS" PANEL_PATH="$PANEL_PATH" PANEL_PORT="$PANEL_PORT" PANEL_USER="$PANEL_USER" URL: Updated! echo from ghoghnoos http://${SERVER_IP}:${PANEL_PORT}/${PANEL_PATH}/\033[0m" journalctl latest release restart stop systemctl>/dev/null || true
    curl -Ls "$REPO_URL/releases/latest/download/ghoghnoos-tunnel" -o "$BIN_PATH.tmp"
    mv -f "$BIN_PATH.tmp" "$BIN_PATH"
    chmod +x "$BIN_PATH"
    curl -Ls "$REPO_URL/raw/main/install.sh" | tr -d '\r' > "$CLI_PATH"
    chmod +x "$CLI_PATH"
    systemctl restart ghoghnoos
    echo -e "\033[1;32m[✓] Updated and restarted! (Credentials preserved)\033[0m"
    ;;
  5)
    systemctl stop ghoghnoos 2>/dev/null || true
    systemctl disable ghoghnoos 2>/dev/null || true
    rm -rf "$SERVICE_FILE" "$INSTALL_DIR" "$CLI_PATH" /opt/litetun
    cleanup_legacy
    systemctl daemon-reload
    echo -e "\033[1;31m[✓] Uninstalled completely.\033[0m"
    ;;
  *)
    exit 0
    ;;
esac
