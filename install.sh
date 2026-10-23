#!/bin/bash

REPO="sahand363/ghoghnoos_light"
APP_DIR="/opt/ghoghnoos"
BIN_PATH="$APP_DIR/ghoghnoos_lite"
CONF_FILE="$APP_DIR/config.env"
SERVICE_FILE="/etc/systemd/system/ghoghnoos.service"

# Ensure root privileges
if [ "$EUID" -ne 0 ]; then
  echo "Please run as root (sudo -i)"
  exit 1
fi

install_base() {
  mkdir -p "$APP_DIR"
  if [ ! -f "$CONF_FILE" ]; then
    echo "Generating initial panel configuration..."
    apt-get update -y >/dev/null 2>&1
    apt-get install -y curl openssl >/dev/null 2>&1
    
    RAND_PATH=$(openssl rand -hex 4)
    cat <<EOF > "$CONF_FILE"
PANEL_PORT=8080
PANEL_PATH=$RAND_PATH
PANEL_USER=admin
PANEL_PASS=admin
EOF
  fi

  if [ ! -f "$BIN_PATH" ]; then
    echo "Downloading latest ghoghnoos_lite binary from GitHub..."
    curl -L "https://github.com/$REPO/releases/latest/download/ghoghnoos_lite" -o "$BIN_PATH"
    chmod +x "$BIN_PATH"
  fi

  if [ ! -f "$SERVICE_FILE" ]; then
    cat <<EOF > "$SERVICE_FILE"
[Unit]
Description=Ghoghnoos Lite Tunnel Panel
After=network.target

[Service]
Type=simple
WorkingDirectory=$APP_DIR
EnvironmentFile=$CONF_FILE
ExecStart=$BIN_PATH
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable ghoghnoos >/dev/null 2>&1
    systemctl start ghoghnoos
  fi

  # Create 'ghoghnoos' shortcut command so you can open the menu anytime
  cp "$0" /usr/local/bin/ghoghnoos 2>/dev/null || curl -Ls "https://raw.githubusercontent.com/$REPO/main/install.sh" -o /usr/local/bin/ghoghnoos
  chmod +x /usr/local/bin/ghoghnoos
}

get_panel_url() {
  source "$CONF_FILE"
  IP=$(curl -s4 --max-time 3 ifconfig.me || hostname -I | awk '{print $1}')
  echo "http://${IP}:${PANEL_PORT}/${PANEL_PATH}/"
}

show_menu() {
  while true; do
    clear
    source "$CONF_FILE"
    STATUS=$(systemctl is-active ghoghnoos)
    echo "=========================================================="
    echo "              GHOGHNOOS LITE MANAGEMENT MENU              "
    echo "=========================================================="
    echo " Status    : $STATUS"
    echo " Panel URL : $(get_panel_url)"
    echo " Username  : $PANEL_USER"
    echo "=========================================================="
    echo "1) Start / Restart"
    echo "2) Stop"
    echo "3) View Logs"
    echo "4) Update"
    echo "5) Uninstall"
    echo "6) Rebuild Panel URL"
    echo "7) Set / Change Username & Password"
    echo "8) Enable / Disable Domain SSL (HTTPS on Port 5001)"
    echo "9) Make this panel Sentinel Node"
    echo "0) Exit"
    echo "=========================================================="
    read -p "Select [0-9]: " choice

    case $choice in
      1)
        systemctl restart ghoghnoos
        echo "✅ Service started/restarted!"
        sleep 1.5
        ;;
      2)
        systemctl stop ghoghnoos
        echo "🛑 Service stopped."
        sleep 1.5
        ;;
      3)
        echo "Press Ctrl+C to exit logs..."
        sleep 1
        journalctl -u ghoghnoos -f -n 50
        ;;
      4)
        echo "⬇️ Updating ghoghnoos_lite from GitHub..."
        curl -L "https://github.com/$REPO/releases/latest/download/ghoghnoos_lite" -o "$BIN_PATH"
        chmod +x "$BIN_PATH"
        systemctl restart ghoghnoos
        echo "✅ Updated and restarted!"
        sleep 2
        ;;
      5)
        systemctl stop ghoghnoos
        systemctl disable ghoghnoos
        rm -rf "$APP_DIR" "$SERVICE_FILE" /usr/local/bin/ghoghnoos
        systemctl daemon-reload
        echo "🗑️ Uninstalled completely."
        exit 0
        ;;
      6)
        NEW_PATH=$(openssl rand -hex 4)
        sed -i "s/^PANEL_PATH=.*/PANEL_PATH=$NEW_PATH/" "$CONF_FILE"
        systemctl restart ghoghnoos
        echo "🔗 New Panel URL: $(get_panel_url)"
        read -p "Press Enter to continue..."
        ;;
      7)
        read -p "Enter new Username: " new_user
        read -p "Enter new Password: " new_pass
        sed -i "s/^PANEL_USER=.*/PANEL_USER=$new_user/" "$CONF_FILE"
        sed -i "s/^PANEL_PASS=.*/PANEL_PASS=$new_pass/" "$CONF_FILE"
        systemctl restart ghoghnoos
        echo "✅ Credentials updated!"
        sleep 1.5
        ;;
      8)
        read -p "Enable HTTPS on Port 5001? (y/n): " ssl_yn
        if [ "$ssl_yn" = "y" ]; then
          sed -i "s/^PANEL_PORT=.*/PANEL_PORT=5001/" "$CONF_FILE"
          openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
            -keyout "$APP_DIR/server.key" -out "$APP_DIR/server.crt" \
            -subj "/CN=ghoghnoos" >/dev/null 2>&1
          echo "✅ SSL enabled on port 5001!"
        else
          sed -i "s/^PANEL_PORT=.*/PANEL_PORT=8080/" "$CONF_FILE"
          rm -f "$APP_DIR/server.key" "$APP_DIR/server.crt"
          echo "✅ Reverted to HTTP on port 8080."
        fi
        systemctl restart ghoghnoos
        sleep 2
        ;;
      9)
        echo "🛰️ Switching node to Sentinel (Slave/Agent) mode..."
        read -p "Enter Master Server IP:Port : " master_addr
        echo "MASTER_NODE=$master_addr" >> "$CONF_FILE"
        systemctl restart ghoghnoos
        echo "✅ Node configured as Sentinel Node!"
        sleep 2
        ;;
      0)
        exit 0
        ;;
      *)
        echo "Invalid option."
        sleep 1
        ;;
    esac
  done
}

install_base
show_menu
