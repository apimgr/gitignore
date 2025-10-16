package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"

	"github.com/apimgr/gitignore/src/database"
	"github.com/apimgr/gitignore/src/paths"
	"github.com/apimgr/gitignore/src/server"
	"github.com/apimgr/gitignore/src/templates"
)

var (
	// Version information (set by build flags)
	Version   = "1.0.0"
	Commit    = "dev"
	BuildDate = "unknown"
)

func main() {
	// Command-line flags
	var (
		configDir  = flag.String("config", "", "Configuration directory (default: OS-specific)")
		dataDir    = flag.String("data", "", "Data directory (default: OS-specific)")
		logsDir    = flag.String("logs", "", "Logs directory (default: OS-specific)")
		port       = flag.Int("port", 0, "HTTP port (default: random 64000-64999)")
		address    = flag.String("address", "0.0.0.0", "Listen address")
		dbType     = flag.String("db-type", "sqlite", "Database type: sqlite, mysql, postgres")
		dbPath     = flag.String("db-path", "", "SQLite database path")
		dbURL      = flag.String("db-url", "", "Database connection string")
		devMode    = flag.Bool("dev", false, "Enable development mode")
		showVer    = flag.Bool("version", false, "Show version information")
		showStatus = flag.Bool("status", false, "Show server status and exit")
	)
	flag.Parse()

	// Show version
	if *showVer {
		fmt.Printf("GitIgnore API Server v%s\n", Version)
		fmt.Printf("Commit: %s\n", Commit)
		fmt.Printf("Built: %s\n", BuildDate)
		os.Exit(0)
	}

	// Check DEV environment variable
	if os.Getenv("DEV") == "true" {
		*devMode = true
	}

	// Initialize paths
	pathMgr := paths.New()
	if *configDir != "" {
		pathMgr.SetConfigDir(*configDir)
	}
	if *dataDir != "" {
		pathMgr.SetDataDir(*dataDir)
	}
	if *logsDir != "" {
		pathMgr.SetLogsDir(*logsDir)
	}

	// Override from environment variables
	if envConfig := os.Getenv("CONFIG_DIR"); envConfig != "" {
		pathMgr.SetConfigDir(envConfig)
	}
	if envData := os.Getenv("DATA_DIR"); envData != "" {
		pathMgr.SetDataDir(envData)
	}
	if envLogs := os.Getenv("LOGS_DIR"); envLogs != "" {
		pathMgr.SetLogsDir(envLogs)
	}

	// Create directories
	if err := pathMgr.EnsureDirectories(); err != nil {
		log.Fatalf("Failed to create directories: %v", err)
	}

	// Determine database connection
	var dbConn string
	if *dbURL != "" {
		dbConn = *dbURL
	} else if envDBURL := os.Getenv("DATABASE_URL"); envDBURL != "" {
		dbConn = envDBURL
	} else if *dbType == "sqlite" {
		// Default SQLite path
		if *dbPath != "" {
			dbConn = *dbPath
		} else {
			dbConn = pathMgr.DataPath("gitignore.db")
		}
	}

	// Initialize database
	db, err := database.New(*dbType, dbConn)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize database schema
	if err := db.InitSchema(); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Check if admin credentials exist, if not generate them
	adminUser := os.Getenv("ADMIN_USER")
	if adminUser == "" {
		adminUser = "administrator"
	}
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	adminToken := os.Getenv("ADMIN_TOKEN")

	credentialsExist, err := db.AdminCredentialsExist()
	if err != nil {
		log.Fatalf("Failed to check admin credentials: %v", err)
	}

	if !credentialsExist {
		// Generate credentials
		if adminPassword == "" {
			adminPassword = generateRandomPassword(16)
		}
		if adminToken == "" {
			adminToken = generateRandomToken(64)
		}

		// Save to database
		if err := db.CreateAdminCredentials(adminUser, adminPassword, adminToken); err != nil {
			log.Fatalf("Failed to create admin credentials: %v", err)
		}

		// Write credentials file
		credFile := pathMgr.ConfigPath("admin_credentials")

		// Determine initial URL - NEVER show localhost (SPEC compliant)
		initialURL := getAccessibleURL(*port)

		credContent := fmt.Sprintf(`GITIGNORE API - ADMIN CREDENTIALS
========================================
WEB UI LOGIN:
  URL:      %s/admin
  Username: %s
  Password: %s

API TOKEN:
  Header:   Authorization: Bearer <token>
  Token:    %s

Created: %s

NOTE: The server URL will be auto-detected from reverse proxy
      headers, public IP, or hostname on first request.
      Check database setting 'server.detected_host' for actual URL.
========================================
`, initialURL, adminUser, adminPassword, adminToken, time.Now().Format("2006-01-02 15:04:05"))

		if err := os.WriteFile(credFile, []byte(credContent), 0600); err != nil {
			log.Printf("Warning: Failed to write credentials file: %v", err)
		} else {
			log.Printf("Admin credentials saved to: %s", credFile)
		}

		// Display credentials
		fmt.Println("\n" + credContent)
		fmt.Println("⚠️  IMPORTANT: Save these credentials securely! They will not be shown again.")
	}

	// Load templates
	log.Println("Loading .gitignore templates...")
	templateMgr, err := templates.New()
	if err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}
	log.Printf("Loaded %d templates", templateMgr.Count())

	// Determine port
	serverPort := *port
	if serverPort == 0 {
		if envPort := os.Getenv("PORT"); envPort != "" {
			fmt.Sscanf(envPort, "%d", &serverPort)
		}
	}
	if serverPort == 0 {
		// Random port in range 64000-64999
		rand.Seed(time.Now().UnixNano())
		serverPort = 64000 + rand.Intn(1000)
	}

	// Show status and exit
	if *showStatus {
		fmt.Printf("Server would start on %s:%d\n", *address, serverPort)
		fmt.Printf("Config: %s\n", pathMgr.GetConfigDir())
		fmt.Printf("Data:   %s\n", pathMgr.GetDataDir())
		fmt.Printf("Logs:   %s\n", pathMgr.GetLogsDir())
		fmt.Printf("Templates: %d\n", templateMgr.Count())
		os.Exit(0)
	}

	// Initialize server
	srv := server.New(&server.Config{
		Address:     *address,
		Port:        serverPort,
		DevMode:     *devMode,
		Database:    db,
		Templates:   templateMgr,
		Paths:       pathMgr,
		Version:     Version,
		Commit:      Commit,
		BuildDate:   BuildDate,
	})

	// Start server
	log.Printf("Starting GitIgnore API Server v%s", Version)
	log.Printf("Server listening on http://%s:%d", *address, serverPort)
	if *devMode {
		log.Println("⚠️  Development mode enabled")
	}
	log.Printf("Admin UI: http://%s:%d/admin", *address, serverPort)
	log.Printf("API:      http://%s:%d/api/v1", *address, serverPort)
	log.Printf("Health:   http://%s:%d/healthz", *address, serverPort)

	if err := srv.Start(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// generateRandomPassword generates a random password of given length
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// generateRandomToken generates a random hex token of given length
func generateRandomToken(length int) string {
	const charset = "0123456789abcdef"
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// getAccessibleURL returns the most relevant URL for accessing the server
// Priority: FQDN > outbound IP > hostname > fallback
// NEVER shows localhost, 127.0.0.1, or 0.0.0.0 (SPEC compliant)
func getAccessibleURL(port int) string {
	portStr := fmt.Sprintf("%d", port)

	// Try to get hostname
	hostname, err := os.Hostname()
	if err == nil && hostname != "" && hostname != "localhost" {
		// Try to resolve hostname to see if it's a valid FQDN
		if addrs, err := net.LookupHost(hostname); err == nil && len(addrs) > 0 {
			return fmt.Sprintf("http://%s:%s", hostname, portStr)
		}
	}

	// Try to get outbound IP (most likely accessible IP)
	if ip := getOutboundIP(); ip != "" && !strings.HasPrefix(ip, "127.") && ip != "0.0.0.0" {
		return fmt.Sprintf("http://%s:%s", ip, portStr)
	}

	// Fallback to hostname if we have one
	if hostname != "" && hostname != "localhost" {
		return fmt.Sprintf("http://%s:%s", hostname, portStr)
	}

	// Last resort: use a generic message
	return fmt.Sprintf("http://<your-host>:%s", portStr)
}

// getOutboundIP gets the preferred outbound IP of this machine
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
