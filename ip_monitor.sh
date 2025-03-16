#!/bin/bash

# Configuration
SERVER_URL="http://127.0.0.1:3000"
LOG_FILE="$HOME/.ip_monitor.log"
LAST_IP_FILE="$HOME/.last_ip"
HOSTNAME=$(hostname)

# Function to get the current IP addresses (both IPv4 and IPv6)
get_current_ips() {
  # Get active network service from scutil
  active_service=$(scutil --nwi | grep "Network interfaces" -A 10 | grep -v "Network interfaces" | grep -v "^$" | head -1 | awk '{print $1}')

  # Get IPv4 address for the active service
  ipv4_local=$(ipconfig getifaddr "$active_service" 2>/dev/null)

  # If no IP found, try common interface names as fallback
  if [ -z "$ipv4_local" ]; then
    for interface in en0 en1 en2 en3; do
      ipv4_local=$(ipconfig getifaddr $interface 2>/dev/null)
      if [ -n "$ipv4_local" ]; then
        break
      fi
    done
  fi

  # Get IPv6 address (non-link-local)
  local ipv6_local=""
  for interface in $(ifconfig -l); do
    # Look for a non-link-local IPv6 address
    ipv6_candidate=$(ifconfig "$interface" | grep "inet6" | grep -v "fe80:" | awk '{print $2}' | head -1)
    if [ -n "$ipv6_candidate" ]; then
      ipv6_local=$ipv6_candidate
      break
    fi
  done

  # user curl http://a.ident.me -4 or -6 for ipv4 and ipv6
  # Get public IPv4 address
  # Return all IPs

  ipv4_public=$(curl -s http://a.ident.me -4)
  ipv6_public=$(curl -s http://a.ident.me -6)

  echo "$ipv4_local,$ipv4_public,$ipv6_local,$ipv6_public"
}

# Function to send notification to server
notify_server() {
  local ip_info=$1
  timestamp=$(date +"%Y-%m-%d %H:%M:%S")

  # Split the IP info
  IFS=',' read -r ipv4_local ipv4_public ipv6_local ipv6_public <<<"$ip_info"

  # Get SSH port (default 22)
  ssh_port=$(grep "^Port " /etc/ssh/sshd_config 2>/dev/null | awk '{print $2}')
  if [ -z "$ssh_port" ]; then
    ssh_port=22
  fi

  # Check if SSH is running
  local ssh_status="inactive"
  if pgrep -x "sshd" >/dev/null; then
    ssh_status="active"
  fi

  # Get computer name (more user-friendly than hostname)
  computer_name=$(scutil --get ComputerName 2>/dev/null || echo "$HOSTNAME")

  # Get current user
  # use find instead
  current_user=$(whoami)

  echo "[$timestamp] Sending IP update: IPv4=$ipv4_local/$ipv4_public, IPv6=$ipv6_local/$ipv6_public" >>"$LOG_FILE"

  # Send HTTP request to server
  curl -s -X POST "$SERVER_URL/update" \
    -H "Content-Type: application/json" \
    -d "{
            \"hostname\":\"$HOSTNAME\",
            \"computerName\":\"$computer_name\",
            \"ipv4Local\":\"$ipv4_local\",
            \"ipv4Public\":\"$ipv4_public\",
            \"ipv6Local\":\"$ipv6_local\",
            \"ipv6Public\":\"$ipv6_public\",
            \"sshPort\":\"$ssh_port\",
            \"sshStatus\":\"$ssh_status\",
            \"currentUser\":\"$current_user\",
            \"timestamp\":\"$timestamp\"
        }"

  # Store successful IP update
  echo "$ip_info" >"$LAST_IP_FILE"
}

# Initialize last IP file if it doesn't exist
if [ ! -f "$LAST_IP_FILE" ]; then
  touch "$LAST_IP_FILE"
fi

# Get current IPs
CURRENT_IP_INFO=$(get_current_ips)
echo "Current IP info: $CURRENT_IP_INFO"
IFS=',' read -r IPV4_LOCAL IPV4_PUBLIC IPV6_LOCAL IPV6_PUBLIC <<<"$CURRENT_IP_INFO"

# Get last reported IPs
LAST_IP_INFO=""
if [ -f "$LAST_IP_FILE" ]; then
  LAST_IP_INFO=$(cat "$LAST_IP_FILE")
fi

echo "IPv4: $IPV4_LOCAL / $IPV4_PUBLIC"
echo "IPv6: $IPV6_LOCAL / $IPV6_PUBLIC"
echo "Last IP info: $LAST_IP_INFO"

# Send notification if IPs have changed or if this is first run
if [ "$CURRENT_IP_INFO" != "$LAST_IP_INFO" ]; then
  notify_server "$CURRENT_IP_INFO"
fi

# Exit cleanly
exit 0
