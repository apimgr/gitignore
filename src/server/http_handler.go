package server

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/pires/go-proxyproto"
)

type torRequestContextKey struct{}

type torRequestIdentity struct {
	circuitID string
}

// HTTPHandler returns the shared HTTP stack used by the clearnet and overlay
// listeners.
func (s *Server) HTTPHandler() http.Handler {
	if s == nil || s.server == nil {
		return http.NotFoundHandler()
	}
	return s.server.Handler
}

// SetTorOnionAddress publishes the currently active onion identity for request
// detection and URL generation.
func (s *Server) SetTorOnionAddress(address string) {
	if s == nil {
		return
	}
	s.torOnionAddress.Store(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(address)), ".onion"))
}

func (s *Server) isTorRequest(r *http.Request) bool {
	if _, ok := r.Context().Value(torRequestContextKey{}).(torRequestIdentity); ok {
		return true
	}
	if s == nil || r == nil {
		return false
	}
	address, _ := s.torOnionAddress.Load().(string)
	if address == "" && s.currentConfig() != nil {
		address = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s.currentConfig().Tor.OnionAddress)), ".onion")
	}
	if address == "" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(r.Host))
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".onion")
	return host == address
}

func (s *Server) markTorRequest(r *http.Request) *http.Request {
	circuitID := "tor"
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if address, parseErr := netip.ParseAddr(host); parseErr == nil && address.Is6() {
			bytes := address.As16()
			if bytes[0] == 0xfc {
				circuitID = fmt.Sprintf("%016x", binary.BigEndian.Uint64(bytes[8:]))
			}
		}
	}
	ctx := context.WithValue(r.Context(), torRequestContextKey{}, torRequestIdentity{circuitID: circuitID})
	clone := r.WithContext(ctx)
	clone.RemoteAddr = "tor:" + circuitID
	return clone
}

// SetTorBackendPort binds the dedicated loopback PROXY-protocol listener used
// only by Tor's hidden service. Port zero closes the current listener.
func (s *Server) SetTorBackendPort(port int) error {
	if s == nil {
		return nil
	}
	s.torBackendMu.Lock()
	defer s.torBackendMu.Unlock()
	if s.torBackendPort == port {
		return nil
	}
	if s.torBackendListener != nil {
		_ = s.torBackendListener.Close()
		s.torBackendListener = nil
		s.torBackendPort = 0
	}
	if port <= 0 {
		return nil
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("bind Tor backend listener: %w", err)
	}
	proxyListener := &proxyproto.Listener{Listener: listener, ReadHeaderTimeout: 5 * time.Second}
	s.torBackendListener = proxyListener
	s.torBackendPort = port
	go func() {
		if err := s.server.Serve(proxyListener); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			log.Printf("Tor backend listener stopped: %v", err)
		}
	}()
	return nil
}

// WatchTorBackend follows the Tor manager's runtime-selected port and closes
// stale listeners whenever the Tor process restarts or becomes unavailable.
func (s *Server) WatchTorBackend(ctx context.Context, backendPort func() int, onionAddress func() string) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	lastPort := -1
	lastAddress := ""
	for {
		select {
		case <-ctx.Done():
			_ = s.SetTorBackendPort(0)
			s.SetTorOnionAddress("")
			return
		case <-ticker.C:
			port := backendPort()
			if port != lastPort {
				if err := s.SetTorBackendPort(port); err != nil {
					log.Printf("Tor backend listener unavailable")
				}
				lastPort = port
			}
			address := onionAddress()
			if address != lastAddress {
				s.SetTorOnionAddress(address)
				lastAddress = address
			}
		}
	}
}
