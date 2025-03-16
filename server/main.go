package main

import (
	"embed"
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

// Embed templates only since static directory is empty
//
//go:embed templates/*.html
var content embed.FS

// DeviceInfo stores information about a device
type DeviceInfo struct {
	ComputerName  string    `json:"computerName"`
	IPv4Local     string    `json:"ipv4Local"`
	IPv4Public    string    `json:"ipv4Public"`
	IPv6Local     string    `json:"ipv6Local"`
	IPv6Public    string    `json:"ipv6Public"`
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
	IPv4Local    string `json:"ipv4Local"`
	IPv4Public   string `json:"ipv4Public"`
	IPv6Local    string `json:"ipv6Local"`
	IPv6Public   string `json:"ipv6Public"`
	SSHPort      string `json:"sshPort"`
	SSHStatus    string `json:"sshStatus"`
	CurrentUser  string `json:"currentUser"`
	Timestamp    string `json:"timestamp"`
}

// Server represents the IP tracker server
type Server struct {
	devices   map[string]DeviceInfo
	dataFile  string
	mutex     sync.RWMutex
	templates *template.Template
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

// Helper functions for templates
func add(a, b int) int {
	return a + b
}

func even(a int) bool {
	return a%2 == 0
}

func NewServer(dataFile string) *Server {
	s := &Server{
		devices:  make(map[string]DeviceInfo),
		dataFile: dataFile,
	}

	// Create template function map
	funcMap := template.FuncMap{
		"timeAgo": timeAgo,
		"add":     add,
		"even":    even,
	}

	// Parse templates
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(content, "templates/*.html")
	if err != nil {
		log.Printf("Warning: Failed to parse templates: %v", err)
	}
	s.templates = tmpl

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

	// Check if this is an HTMX request asking for just the table
	if r.Header.Get("HX-Request") == "true" {
		if err := s.templates.ExecuteTemplate(w, "device_table.html", s.devices); err != nil {
			log.Printf("Error executing template: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
		return
	}

	// Otherwise, render the full page
	if s.templates != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.templates.ExecuteTemplate(w, "index.html", s.devices); err != nil {
			log.Printf("Error executing template: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	} else {
		// Fallback if templates failed to parse
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
	if update.Hostname == "" || (update.IPv4Local == "" && update.IPv4Public == "" &&
		update.IPv6Local == "" && update.IPv6Public == "") {
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
		IPv4Local:     update.IPv4Local,
		IPv4Public:    update.IPv4Public,
		IPv6Local:     update.IPv6Local,
		IPv6Public:    update.IPv6Public,
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

	log.Printf("Update received for %s: IPv4=%s/%s, IPv6=%s/%s, User=%s",
		update.ComputerName, update.IPv4Local, update.IPv4Public,
		update.IPv6Local, update.IPv6Public, update.CurrentUser)

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

// Handler for SSH command fragment for HTMX
func (s *Server) handleSSHCommand(w http.ResponseWriter, r *http.Request) {
	hostname := r.FormValue("hostname")
	preferIPv6 := r.FormValue("ipv6") == "true"

	if hostname == "" {
		http.Error(w, "Hostname required", http.StatusBadRequest)
		return
	}

	s.mutex.RLock()
	device, exists := s.devices[hostname]
	s.mutex.RUnlock()

	if !exists {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	var ipToUse string

	// Determine which IP to use based on preference and availability
	if preferIPv6 && device.IPv6Public != "" {
		ipToUse = device.IPv6Public
	} else if device.IPv4Public != "" {
		ipToUse = device.IPv4Public
	} else if preferIPv6 && device.IPv6Local != "" {
		ipToUse = device.IPv6Local
	} else {
		ipToUse = device.IPv4Local
	}

	sshCommand := fmt.Sprintf("ssh %s@%s", device.CurrentUser, ipToUse)
	if device.SSHPort != "" && device.SSHPort != "22" {
		sshCommand += fmt.Sprintf(" -p %s", device.SSHPort)
	}

	// Use template to render the command interface
	if err := s.templates.ExecuteTemplate(w, "ssh_command.html", map[string]string{
		"Command": sshCommand,
	}); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func getEnv(key, defaultValue string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = defaultValue
	}
	return value
}

func main() {
	// Get port from environment variable or use default
	port := getEnv("PORT", "3000")
	dataFile := getEnv("DATA_FILE", "devices.json")

	// Create server
	server := NewServer(dataFile)

	// Set up routes
	http.HandleFunc("/", server.handleIndex)
	http.HandleFunc("/update", server.handleUpdate)
	http.HandleFunc("/devices", server.handleListDevices)
	http.HandleFunc("/devices/", server.handleGetDevice)
	http.HandleFunc("/ssh-command", server.handleSSHCommand)

	// Start server
	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
