#!/bin/bash

# Configuration
SERVER_URL="http://127.0.0.1:3000/update"
LOG_FILE="$HOME/.ip_monitor.log"
LAST_IP_FILE="$HOME/.last_ip"
HOSTNAME=$(hostname)

# Create a function to get the current primary IP address
get_current_ip() {
  # Get active network service from scutil
  local active_service=$(scutil --nwi | grep "Network interfaces" -A 10 | grep -v "Network interfaces" | grep -v "^$" | head -1 | awk '{print $1}')

  # Get IP address for the active service (handles both Wi-Fi and Ethernet)
  local ip=$(ipconfig getifaddr $active_service 2>/dev/null)

  # If no IP found, try common interface names as fallback
  if [ -z "$ip" ]; then
    for interface in en0 en1 en2 en3; do
      ip=$(ipconfig getifaddr $interface 2>/dev/null)
      if [ -n "$ip" ]; then
        break
      fi
    done
  fi

  echo $ip
}

# Function to send notification to server
notify_server() {
  local ip_info=$1
  local timestamp=$(date +"%Y-%m-%d %H:%M:%S")

  # Split the IP info
  IFS=',' read -r local_ip public_ip <<<"$ip_info"

  # Get SSH port (default 22)
  local ssh_port=$(grep "^Port " /etc/ssh/sshd_config 2>/dev/null | awk '{print $2}')
  if [ -z "$ssh_port" ]; then
    ssh_port=22
  fi

  # Check if SSH is running
  local ssh_status="inactive"
  if pgrep -x "sshd" >/dev/null; then
    ssh_status="active"
  fi

  # Get computer name (more user-friendly than hostname)
  local computer_name=$(scutil --get ComputerName 2>/dev/null || echo "$HOSTNAME")

  # Get current user
  local current_user=$(ls -l /dev/console | awk '{print $3}')

  echo "[$timestamp] Sending IP update: Local=$local_ip, Public=$public_ip" >>"$LOG_FILE"

  # Send HTTP request to server
  curl -s -X POST "$SERVER_URL/update" \
    -H "Content-Type: application/json" \
    -d "{
            \"hostname\":\"$HOSTNAME\",
            \"computerName\":\"$computer_name\",
            \"localIp\":\"$local_ip\",
            \"publicIp\":\"$public_ip\",
            \"sshPort\":\"$ssh_port\",
            \"sshStatus\":\"$ssh_status\",
            \"currentUser\":\"$current_user\",
            \"timestamp\":\"$timestamp\"
        }" \
    -o /dev/null

  # Store successful IP update
  echo "$ip_info" >"$LAST_IP_FILE"
}

# Initialize last IP file if it doesn't exist
if [ ! -f "$LAST_IP_FILE" ]; then
  touch "$LAST_IP_FILE"
fi

# Get current IPs
CURRENT_IP_INFO=$(get_current_ip)
IFS=',' read -r LOCAL_IP PUBLIC_IP <<<"$CURRENT_IP_INFO"

# Get last reported IPs
LAST_IP_INFO=""
if [ -f "$LAST_IP_FILE" ]; then
  LAST_IP_INFO=$(cat "$LAST_IP_FILE")
fi

# Only send notification if IP has changed or is new
if [ -n "$LOCAL_IP" ] && [ -n "$PUBLIC_IP" ] && [ "$CURRENT_IP_INFO" != "$LAST_IP_INFO" ]; then
  notify_server "$CURRENT_IP_INFO"
fi

# Exit cleanly
exit 0
