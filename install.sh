#!/bin/bash

REPO="sahand363/ghoghnoos_light"
APP_DIR="/opt/litetun"
BIN_PATH="$APP_DIR/ghoghnoos_lite"
CONF_FILE="$APP_DIR/config.env"
SERVICE_FILE="/etc/systemd/system/litetun.service"

if [ "$EUID" -ne 0 ]; then
  echo "Please run as root (sudo -i)"
  exit 1
fi

install_base() {
  mkdir -p "$APP_DIR"
  apt-get update -y >/dev/null 2>&1
  apt-get install -y curl openssl sshpass >/dev/null 2>&1

  if [ ! -f "$CONF_FILE" ]; then
    RAND_PATH=$(openssl rand -hex 4)
    cat <<EOF > "$CONF_FILE"
PANEL_PORT=6001
PANEL_PATH=$RAND_PATH
PANEL_USER=admin
PANEL_PASS=admin
EOF
  fi

  if [ ! -f "$BIN_PATH" ]; then
    echo "Downloading latest binary from GitHub..."
    curl -L "https://github.com/$REPO/releases/latest/download/ghoghnoos_lite" -o "$BIN_PATH.tmp"
    mv -f "$BIN_PATH.tmp" "$BIN_PATH"
    chmod +x "$BIN_PATH"
  fi

  if [ ! -f "$SERVICE_FILE" ]; then
    cat <<EOF > "$SERVICE_FILE"
[Unit]
Description=LiteTun WSS Tunnel Panel
After=network.target

[Service]
Type=simple
WorkingDirectory=$APP_DIR
EnvironmentFile=$CONF_FILE
ExecStart=$BIN_PATH
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable litetun >/dev/null 2>&1
    systemctl start litetun
  fi

  curl -Ls "https://raw.githubusercontent.com/$REPO/main/install.sh" | tr -d '\r' > /usr/local/bin/litetun
  chmod +x /usr/local/bin/litetun
}

get_panel_url() {
  source "$CONF_FILE"
  IP=$(hostname -I | awk '{print $1}')
  echo "http://${IP}:${PANEL_PORT}/${PANEL_PATH}/"
}

show_menu() {
  while true; do
    clear
    source "$CONF_FILE"
    STATUS=$(systemctl is-active litetun)
    echo "=========================================================="
    echo "                 LITETUN MANAGEMENT MENU                  "
    echo "=========================================================="
    echo " Status    : $STATUS"
    echo " Panel Port: $PANEL_PORT"
    echo " Panel URL : $(get_panel_url)"
    echo " Command   : litetun"
    echo "=========================================================="
    echo "1) Start / Restart"
    echo "2) Stop"
    echo "3) View Logs"
    echo "4) Update"
    echo "5) Uninstall"
    echo "6) Set Custom URL Path"
    echo "7) Set Custom Panel Port"
    echo "8) Set / Change Username & Password"
    echo "9) Enable / Disable Domain SSL (Port 5001)"
    echo "0) Exit"
    echo "=========================================================="
    read -p "Select [0-9]: " choice < /dev/tty

    case $choice in
      1)
        systemctl restart litetun
        echo "✅ Service restarted!"
        sleep 1
        ;;
      2)
        systemctl stop litetun
        echo "🛑 Service stopped."
        sleep 1
        ;;
      3)
        journalctl -u litetun -f -n 50
        ;;
      4)
        echo "⬇️ Stopping service and updating binary..."
        apt-get install -y sshpass >/dev/null 2>&1
        systemctl stop litetun
        curl -L "https://github.com/$REPO/releases/latest/download/ghoghnoos_lite" -o "$BIN_PATH.tmp"
        mv -f "$BIN_PATH.tmp" "$BIN_PATH"
        chmod +x "$BIN_PATH"
        curl -Ls "https://raw.githubusercontent.com/$REPO/main/install.sh" | tr -d '\r' > /usr/local/bin/litetun
        chmod +x /usr/local/bin/litetun
        systemctl start litetun
        echo "✅ Updated and restarted cleanly!"
        sleep 2
        ;;
      5)
        systemctl stop litetun
        systemctl disable litetun
        rm -rf "$APP_DIR" "$SERVICE_FILE" /usr/local/bin/litetun
        systemctl daemon-reload
        echo "🗑️ Uninstalled."
        exit 0
        ;;
      6)
        read -p "Enter new URL Path: " input_path < /dev/tty
        [ -z "$input_path" ] && input_path=$(openssl rand -hex 4)
        sed -i "s/^PANEL_PATH=.*/PANEL_PATH=$input_path/" "$CONF_FILE"
        systemctl restart litetun
        echo "🔗 New URL: $(get_panel_url)"
        read -p "Press Enter..." < /dev/tty
        ;;
      7)
        read -p "Enter new Port: " new_port < /dev/tty
        if [ -n "$new_port" ]; then
          sed -i "s/^PANEL_PORT=.*/PANEL_PORT=$new_port/" "$CONF_FILE"
          systemctl restart litetun
          echo "🔗 New URL: $(get_panel_url)"
          read -p "Press Enter..." < /dev/tty
        fi
        ;;
      8)
        read -p "Enter new Username: " new_user < /dev/tty
        read -p "Enter new Password: " new_pass < /dev/tty
        sed -i "s/^PANEL_USER=.*/PANEL_USER=$new_user/" "$CONF_FILE"
        sed -i "s/^PANEL_PASS=.*/PANEL_PASS=$new_pass/" "$CONF_FILE"
        systemctl restart litetun
        sleep 1
        ;;
      9)
        read -p "Enable HTTPS on Port 5001? (y/n): " ssl_yn < /dev/tty
        if [ "$ssl_yn" = "y" ]; then
          sed -i "s/^PANEL_PORT=.*/PANEL_PORT=5001/" "$CONF_FILE"
          openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
            -keyout "$APP_DIR/server.key" -out "$APP_DIR/server.crt" \
            -subj "/CN=litetun" >/dev/null 2>&1
        else
          sed -i "s/^PANEL_PORT=.*/PANEL_PORT=8880/" "$CONF_FILE"
          rm -f "$APP_DIR/server.key" "$APP_DIR/server.crt"
        fi
        systemctl restart litetun
        sleep 1
        ;;
      0)
        exit 0
        ;;
    esac
  done
}

# Support silent install when called by Sentinel Deployer
if [ "$1" = "--sentinel" ]; then
  install_base
  exit 0
fi

install_base
show_menu
