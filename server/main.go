package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DeviceInfo stores information about a device
type DeviceInfo struct {
	ComputerName  string    `json:"computerName"`
	LocalIP       string    `json:"localIp"`
	PublicIP      string    `json:"publicIp"`
	SSHPort       string    `json:"sshPort"`
	SSHStatus     string    `json:"sshStatus"`
	CurrentUser   string    `json:"currentUser"`
	LastUpdate    time.Time `json:"lastUpdate"`
	UserAgent     string    `json:"userAgent"`
	RemoteAddress string    `json:"remoteAddress"`
}

// DeviceUpdate represents the payload from client
type DeviceUpdate struct {
	Hostname     string `json:"hostname"`
	ComputerName string `json:"computerName"`
	LocalIP      string `json:"localIp"`
	PublicIP     string `json:"publicIp"`
	SSHPort      string `json:"sshPort"`
	SSHStatus    string `json:"sshStatus"`
	CurrentUser  string `json:"currentUser"`
	Timestamp    string `json:"timestamp"`
}

// Server represents the IP tracker server
type Server struct {
	devices    map[string]DeviceInfo
	dataFile   string
	mutex      sync.RWMutex
	htmlTmpl   *template.Template
	tmplSource string
}

// timeAgo formats a time value as a "time ago" string
func timeAgo(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	seconds := diff.Seconds()
	minutes := diff.Minutes()
	hours := diff.Hours()
	days := hours / 24
	months := days / 30
	years := days / 365

	if years >= 1 {
		return fmt.Sprintf("%.0f years ago", years)
	} else if months >= 1 {
		return fmt.Sprintf("%.0f months ago", months)
	} else if days >= 1 {
		return fmt.Sprintf("%.0f days ago", days)
	} else if hours >= 1 {
		return fmt.Sprintf("%.0f hours ago", hours)
	} else if minutes >= 1 {
		return fmt.Sprintf("%.0f minutes ago", minutes)
	} else if seconds >= 10 {
		return fmt.Sprintf("%.0f seconds ago", seconds)
	}
	return "just now"
}

func NewServer(dataFile string) *Server {
	s := &Server{
		devices:    make(map[string]DeviceInfo),
		dataFile:   dataFile,
		tmplSource: getHTMLTemplate(),
	}

	// Create template function map
	funcMap := template.FuncMap{
		"timeAgo": timeAgo,
	}

	// Parse HTML template with function map
	var err error
	s.htmlTmpl, err = template.New("index").Funcs(funcMap).Parse(s.tmplSource)
	if err != nil {
		log.Printf("Warning: Failed to parse HTML template: %v", err)
	}

	// Load existing device data
	s.loadDevices()

	return s
}

func (s *Server) loadDevices() {
	// Create directory if it doesn't exist
	dir := filepath.Dir(s.dataFile)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("Error creating directory: %v", err)
			return
		}
	}

	// Check if file exists
	if _, err := os.Stat(s.dataFile); os.IsNotExist(err) {
		log.Printf("Data file does not exist yet: %s", s.dataFile)
		return
	}

	// Read and parse data file
	data, err := os.ReadFile(s.dataFile)
	if err != nil {
		log.Printf("Error reading data file: %v", err)
		return
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if err := json.Unmarshal(data, &s.devices); err != nil {
		log.Printf("Error parsing data file: %v", err)
	}
}

func (s *Server) saveDevices() {
	s.mutex.RLock()
	data, err := json.MarshalIndent(s.devices, "", "  ")
	s.mutex.RUnlock()

	if err != nil {
		log.Printf("Error marshaling device data: %v", err)
		return
	}

	if err := os.WriteFile(s.dataFile, data, 0o644); err != nil {
		log.Printf("Error writing data file: %v", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if s.htmlTmpl != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.htmlTmpl.Execute(w, s.devices); err != nil {
			log.Printf("Error executing template: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	} else {
		// Fallback if template failed to parse
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(s.devices); err != nil {
			log.Printf("Error encoding JSON: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var update DeviceUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if update.Hostname == "" || (update.LocalIP == "" && update.PublicIP == "") {
		http.Error(w, "Missing required fields: hostname and at least one IP", http.StatusBadRequest)
		return
	}

	// Parse timestamp or use current time
	var lastUpdate time.Time
	var err error
	if update.Timestamp != "" {
		lastUpdate, err = time.Parse("2006-01-02 15:04:05", update.Timestamp)
		if err != nil {
			lastUpdate = time.Now()
		}
	} else {
		lastUpdate = time.Now()
	}

	// Update device info
	s.mutex.Lock()
	s.devices[update.Hostname] = DeviceInfo{
		ComputerName:  update.ComputerName,
		LocalIP:       update.LocalIP,
		PublicIP:      update.PublicIP,
		SSHPort:       update.SSHPort,
		SSHStatus:     update.SSHStatus,
		CurrentUser:   update.CurrentUser,
		LastUpdate:    lastUpdate,
		UserAgent:     r.UserAgent(),
		RemoteAddress: r.RemoteAddr,
	}
	s.mutex.Unlock()

	// Save updated device data
	go s.saveDevices()

	log.Printf("Update received for %s: Local=%s, Public=%s, User=%s",
		update.ComputerName, update.LocalIP, update.PublicIP, update.CurrentUser)

	// Respond with success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.devices); err != nil {
		log.Printf("Error encoding JSON: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	hostname := r.URL.Path[len("/devices/"):]
	if hostname == "" {
		http.Error(w, "Device hostname required", http.StatusBadRequest)
		return
	}

	s.mutex.RLock()
	device, exists := s.devices[hostname]
	s.mutex.RUnlock()

	if !exists {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(device); err != nil {
		log.Printf("Error encoding JSON: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func main() {
	// Get port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	// Get data file path from environment variable or use default
	dataFile := os.Getenv("DATA_FILE")
	if dataFile == "" {
		dataFile = "devices.json"
	}

	// Create server
	server := NewServer(dataFile)

	// Set up routes
	http.HandleFunc("/", server.handleIndex)
	http.HandleFunc("/update", server.handleUpdate)
	http.HandleFunc("/devices", server.handleListDevices)
	http.HandleFunc("/devices/", server.handleGetDevice)

	// Start server
	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func getHTMLTemplate() string {
	return `
<!DOCTYPE html>
<html>
<head>
  <title>Device Tracker</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <style>
    body { font-family: Arial, sans-serif; max-width: 1000px; margin: 0 auto; padding: 20px; }
    table { width: 100%; border-collapse: collapse; margin-top: 20px; }
    th, td { padding: 10px; text-align: left; border-bottom: 1px solid #ddd; }
    th { background-color: #f2f2f2; }
    .active { color: green; font-weight: bold; }
    .inactive { color: red; }
    .refresh { float: right; }
    .connection-info { background-color: #f8f8f8; padding: 15px; border-radius: 5px; margin-top: 20px; }
    .copy-btn {
      background-color: #4CAF50;
      color: white;
      border: none;
      padding: 5px 10px;
      text-align: center;
      text-decoration: none;
      display: inline-block;
      font-size: 14px;
      margin: 4px 2px;
      cursor: pointer;
      border-radius: 4px;
    }
    .connect-btn {
      background-color: #2196F3;
      color: white;
      border: none;
      padding: 6px 12px;
      border-radius: 4px;
      cursor: pointer;
    }
    .cmd {
      background-color: #333;
      color: white;
      padding: 10px;
      border-radius: 4px;
      font-family: monospace;
      margin: 10px 0;
      display: flex;
      justify-content: space-between;
      align-items: center;
    }
    .cmd-text {
      flex-grow: 1;
    }
    .copy-success {
      background-color: #4CAF50;
      color: white;
      padding: 5px 10px;
      border-radius: 4px;
      position: fixed;
      top: 20px;
      right: 20px;
      display: none;
    }
    .last-update {
      font-size: 12px;
      color: #666;
    }
  </style>
</head>
<body>
  <h1>Device Tracker <button class="refresh" onclick="location.reload()">Refresh</button></h1>
  <div id="copySuccess" class="copy-success">Copied to clipboard!</div>
  
  <table>
    <tr>
      <th>Device Name</th>
      <th>Status</th>
      <th>Public IP</th>
      <th>Local IP</th>
      <th>Current User</th>
      <th>Last Update</th>
      <th>Actions</th>
    </tr>
    {{range $hostname, $device := .}}
    <tr>
      <td>{{if $device.ComputerName}}{{$device.ComputerName}}{{else}}{{$hostname}}{{end}}</td>
      <td class="{{if eq $device.SSHStatus "active"}}active{{else}}inactive{{end}}">
        {{if eq $device.SSHStatus "active"}}Online{{else}}SSH Offline{{end}}
      </td>
      <td>{{$device.PublicIP}}</td>
      <td>{{$device.LocalIP}}</td>
      <td>{{$device.CurrentUser}}</td>
      <td>
        {{$device.LastUpdate.Format "2006-01-02 15:04:05"}}
        <div class="last-update">{{timeAgo $device.LastUpdate}}</div>
      </td>
      <td>
        {{if eq $device.SSHStatus "active"}}
          <button class="connect-btn" onclick="copySSHCommand('{{$hostname}}')">Copy SSH Command</button>
        {{else}}
          <button class="connect-btn" onclick="showConnectionInfo('{{$hostname}}')">Connect</button>
        {{end}}
      </td>
    </tr>
    {{end}}
  </table>
  
  <div id="connectionInfo" class="connection-info" style="display: none;"></div>
  
  <script>
    function copyToClipboard(text) {
      const el = document.createElement('textarea');
      el.value = text;
      document.body.appendChild(el);
      el.select();
      document.execCommand('copy');
      document.body.removeChild(el);
      
      // Show success message
      const successMsg = document.getElementById('copySuccess');
      successMsg.style.display = 'block';
      setTimeout(() => {
        successMsg.style.display = 'none';
      }, 2000);
    }
    
    function copySSHCommand(hostname) {
      const devices = {{.}};
      const device = devices[hostname];
      
      if (!device) return;
      
      let sshCommand = '';
      if (device.currentUser) {
        sshCommand = 'ssh ' + device.currentUser + '@' + device.publicIp;
        if (device.sshPort && device.sshPort !== '22') {
          sshCommand += ' -p ' + device.sshPort;
        }
      } else {
        sshCommand = 'ssh username@' + device.publicIp;
        if (device.sshPort && device.sshPort !== '22') {
          sshCommand += ' -p ' + device.sshPort;
        }
      }
      
      copyToClipboard(sshCommand);
    }
    
    function showConnectionInfo(hostname) {
      const devices = {{.}};
      const device = devices[hostname];
      const infoDiv = document.getElementById('connectionInfo');
      
      if (!device) {
        infoDiv.textContent = 'Device information not found';
        infoDiv.style.display = 'block';
        return;
      }
      
      let connectionInfo = '';
      
      // SSH Connection String
      if (device.sshStatus === 'active') {
        connectionInfo += '<h3>SSH Connection</h3>';
        let sshCommand = '';
        if (device.currentUser) {
          sshCommand = 'ssh ' + device.currentUser + '@' + device.publicIp;
          if (device.sshPort && device.sshPort !== '22') {
            sshCommand += ' -p ' + device.sshPort;
          }
        } else {
          sshCommand = 'ssh username@' + device.publicIp;
          if (device.sshPort && device.sshPort !== '22') {
            sshCommand += ' -p ' + device.sshPort;
          }
        }
        connectionInfo += '<div class="cmd"><span class="cmd-text">' + sshCommand + '</span><button class="copy-btn" onclick="copyToClipboard(\'' + sshCommand + '\')">Copy</button></div>';
        connectionInfo += '<small>Replace username with your macOS username if not shown</small>';
      } else {
        connectionInfo += '<h3>SSH appears to be offline on this device</h3>';
      }
      
      // Remote Desktop Info
      connectionInfo += '<h3>Screen Sharing (VNC)</h3>';
      let vncCommand = 'vnc://' + device.publicIp;
      connectionInfo += '<div class="cmd"><span class="cmd-text">' + vncCommand + '</span><button class="copy-btn" onclick="copyToClipboard(\'' + vncCommand + '\')">Copy</button></div>';
      connectionInfo += '<p>In Finder, select Go > Connect to Server and paste the VNC address</p>';
      
      // Local Network Connection
      connectionInfo += '<h3>Local Network Connection (if on same network)</h3>';
      let localSshCommand = '';
      if (device.currentUser) {
        localSshCommand = 'ssh ' + device.currentUser + '@' + device.publicIp;
        if (device.sshPort && device.sshPort !== '22') {
          localSshCommand += ' -p ' + device.sshPort;
        }
      } else {
        localSshCommand = 'ssh username@' + device.publicIp;
        if (device.sshPort && device.sshPort !== '22') {
          localSshCommand += ' -p ' + device.sshPort;
        }
      }
      connectionInfo += '<div class="cmd"><span class="cmd-text">' + localSshCommand + '</span><button class="copy-btn" onclick="copyToClipboard(\'' + localSshCommand + '\')">Copy</button></div>';
      
      // Additional tips
      connectionInfo += '<h3>Troubleshooting</h3>';
      connectionInfo += '<p>If connection fails:</p>';
      connectionInfo += '<ul>';
      connectionInfo += '<li>Check if you\'re behind a NAT/firewall</li>';
      connectionInfo += '<li>Set up port forwarding on your router for SSH and VNC</li>';
      connectionInfo += '<li>Consider using a VPN or reverse SSH tunnel if direct connection fails</li>';
      connectionInfo += '</ul>';
      
      infoDiv.innerHTML = connectionInfo;
      infoDiv.style.display = 'block';
    }
  </script>
</body>
</html>
`
}
