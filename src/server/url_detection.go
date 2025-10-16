package server

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// detectServerURL detects the server's public URL from various sources
func (s *Server) detectServerURL(r *http.Request) string {
	// Priority 1: Check for reverse proxy headers
	if proto := getProxyProto(r); proto != "" {
		if host := getProxyHost(r); host != "" {
			port := getProxyPort(r)

			// Log detection for debugging
			if s.config.DevMode {
				log.Printf("URL Detection: Proxy headers detected - proto=%s host=%s port=%s", proto, host, port)
			}

			// Save to database for persistence
			s.saveDetectedURL(proto, host, port)

			return buildURL(proto, host, port)
		}
	}

	// Priority 2: Fall back to database values (persisted from previous requests)
	if url := s.getDetectedURLFromDB(); url != "" {
		if s.config.DevMode {
			log.Printf("URL Detection: Using saved URL from database - %s", url)
		}
		return url
	}

	// Priority 3: Try FQDN (hostname that resolves)
	hostname, err := os.Hostname()
	if err == nil && hostname != "" && hostname != "localhost" {
		// Try to resolve hostname to see if it's a valid FQDN
		if addrs, err := net.LookupHost(hostname); err == nil && len(addrs) > 0 {
			proto := s.detectProtocol(r)
			port := fmt.Sprintf("%d", s.config.Port)

			if s.config.DevMode {
				log.Printf("URL Detection: Using FQDN - proto=%s hostname=%s port=%s (resolves to %v)", proto, hostname, port, addrs)
			}

			// Save to database
			s.saveDetectedURL(proto, hostname, port)

			return buildURL(proto, hostname, port)
		}
	}

	// Priority 4: Detect outbound IP
	if ip := getOutboundIP(); ip != "" && !strings.HasPrefix(ip, "127.") && ip != "0.0.0.0" {
		proto := s.detectProtocol(r)
		port := fmt.Sprintf("%d", s.config.Port)

		if s.config.DevMode {
			log.Printf("URL Detection: Using outbound IP - proto=%s ip=%s port=%s", proto, ip, port)
		}

		// Save to database
		s.saveDetectedURL(proto, ip, port)

		return buildURL(proto, ip, port)
	}

	// Priority 5: Use hostname (if available and not localhost)
	if hostname != "" && hostname != "localhost" {
		proto := s.detectProtocol(r)
		port := fmt.Sprintf("%d", s.config.Port)

		if s.config.DevMode {
			log.Printf("URL Detection: Using hostname - proto=%s hostname=%s port=%s", proto, hostname, port)
		}

		// Save to database
		s.saveDetectedURL(proto, hostname, port)

		return buildURL(proto, hostname, port)
	}

	// Priority 6: Fallback to generic placeholder
	proto := s.detectProtocol(r)
	port := fmt.Sprintf("%d", s.config.Port)

	if s.config.DevMode {
		log.Printf("URL Detection: Using fallback - proto=%s port=%s", proto, port)
	}

	return buildURL(proto, "<your-host>", port)
}

// getProxyProto gets protocol from reverse proxy headers
func getProxyProto(r *http.Request) string {
	// X-Forwarded-Proto (most common)
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}

	// Forwarded (RFC 7239)
	if fwd := r.Header.Get("Forwarded"); fwd != "" {
		if strings.Contains(fwd, "proto=https") {
			return "https"
		}
		if strings.Contains(fwd, "proto=http") {
			return "http"
		}
	}

	return ""
}

// getProxyHost gets host from reverse proxy headers
func getProxyHost(r *http.Request) string {
	// X-Forwarded-Host (most common)
	if host := r.Header.Get("X-Forwarded-Host"); host != "" {
		return host
	}

	// Forwarded (RFC 7239)
	if fwd := r.Header.Get("Forwarded"); fwd != "" {
		parts := strings.Split(fwd, ";")
		for _, part := range parts {
			if strings.HasPrefix(strings.TrimSpace(part), "host=") {
				return strings.TrimPrefix(strings.TrimSpace(part), "host=")
			}
		}
	}

	// Host header (fallback)
	if host := r.Header.Get("Host"); host != "" {
		return host
	}

	return ""
}

// getProxyPort gets port from reverse proxy headers
func getProxyPort(r *http.Request) string {
	// X-Forwarded-Port
	if port := r.Header.Get("X-Forwarded-Port"); port != "" {
		return port
	}

	return ""
}

// detectProtocol detects the protocol (http/https)
func (s *Server) detectProtocol(r *http.Request) string {
	// Check TLS
	if r.TLS != nil {
		return "https"
	}

	// Check X-Forwarded-Proto
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return "https"
	}

	// Default to http
	return "http"
}

// detectPublicIP attempts to detect the server's public IP address
// Uses outbound IP detection (SPEC compliant)
func detectPublicIP() string {
	// Try outbound IP first (most reliable)
	if ip := getOutboundIP(); ip != "" {
		return ip
	}

	// Fallback to external services
	services := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
		"https://api.ip.sb/ip",
		"https://icanhazip.com",
	}

	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	for _, service := range services {
		resp, err := client.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				continue
			}

			ip := strings.TrimSpace(string(body))
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	return ""
}

// getOutboundIP gets the preferred outbound IP of this machine (SPEC required)
// Priority: FQDN > outbound IP > hostname > fallback
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// getHostname gets the system hostname
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	return hostname
}

// buildURL constructs a complete URL from components
func buildURL(proto, host, port string) string {
	// Remove port from host if it's already included
	if strings.Contains(host, ":") {
		host = strings.Split(host, ":")[0]
	}

	// Handle standard ports (omit from URL)
	if (proto == "http" && port == "80") || (proto == "https" && port == "443") || port == "" || port == "0" {
		return fmt.Sprintf("%s://%s", proto, host)
	}

	return fmt.Sprintf("%s://%s:%s", proto, host, port)
}

// saveDetectedURL saves the detected URL components to the database
func (s *Server) saveDetectedURL(proto, host, port string) {
	// Only update if values have changed (avoid unnecessary DB writes)
	currentProto, _ := s.config.Database.GetSetting("server.detected_proto")
	currentHost, _ := s.config.Database.GetSetting("server.detected_host")
	currentPort, _ := s.config.Database.GetSetting("server.detected_port")

	if currentProto != proto {
		s.config.Database.SetSetting("server.detected_proto", proto)
	}

	if currentHost != host {
		s.config.Database.SetSetting("server.detected_host", host)
	}

	if currentPort != port && port != "" {
		s.config.Database.SetSetting("server.detected_port", port)
	}

	// Update timestamp
	s.config.Database.SetSetting("server.last_proxy_update", time.Now().Format(time.RFC3339))
}

// getDetectedURLFromDB retrieves the persisted server URL from the database
func (s *Server) getDetectedURLFromDB() string {
	proto, err := s.config.Database.GetSetting("server.detected_proto")
	if err != nil || proto == "" {
		return ""
	}

	host, err := s.config.Database.GetSetting("server.detected_host")
	if err != nil || host == "" {
		return ""
	}

	port, _ := s.config.Database.GetSetting("server.detected_port")

	return buildURL(proto, host, port)
}
