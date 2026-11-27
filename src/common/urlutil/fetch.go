package urlutil

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// FetchRemoteImageConfig defines size, timeout, MIME, and scheme limits for
// server-side image fetches.
type FetchRemoteImageConfig struct {
	MaxSize        int64
	Timeout        time.Duration
	AllowedTypes   []string
	AllowedSchemes []string
}

// DefaultFetchRemoteImageConfig returns conservative HTTPS-only image limits.
func DefaultFetchRemoteImageConfig() FetchRemoteImageConfig {
	return FetchRemoteImageConfig{
		MaxSize: 10 << 20, Timeout: 30 * time.Second,
		AllowedTypes:   []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/x-icon"},
		AllowedSchemes: []string{"https"},
	}
}

// ValidateRemoteURL rejects credentials, non-HTTPS schemes, private addresses,
// and internal hostnames before a request is made.
func ValidateRemoteURL(ctx context.Context, rawURL string, cfg FetchRemoteImageConfig) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("invalid remote image URL")
	}
	allowed := false
	for _, scheme := range cfg.AllowedSchemes {
		if strings.EqualFold(u.Scheme, scheme) {
			allowed = true
			break
		}
	}
	if !allowed || !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("remote image URL scheme is not allowed")
	}
	if port := u.Port(); port != "" && port != "443" {
		return fmt.Errorf("remote image URL port is not allowed")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("remote image URL host is local or internal")
	}
	addresses, err := resolvePublicAddresses(ctx, host)
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		return fmt.Errorf("remote image URL has no public addresses")
	}
	return nil
}

// FetchRemoteImage fetches a public HTTPS image with redirect, address, size,
// MIME, and timeout validation.
func FetchRemoteImage(ctx context.Context, rawURL string, cfg FetchRemoteImageConfig) ([]byte, string, error) {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 10 << 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if len(cfg.AllowedTypes) == 0 {
		cfg.AllowedTypes = DefaultFetchRemoteImageConfig().AllowedTypes
	}
	if len(cfg.AllowedSchemes) == 0 {
		cfg.AllowedSchemes = []string{"https"}
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	if err := ValidateRemoteURL(ctx, rawURL, cfg); err != nil {
		return nil, "", fmt.Errorf("remote image URL validation failed")
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := resolvePublicAddresses(ctx, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				connection, dialErr := (&net.Dialer{Timeout: cfg.Timeout, KeepAlive: 30 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				lastErr = dialErr
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("no public address available")
			}
			return nil, lastErr
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   cfg.Timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return ValidateRemoteURL(request.Context(), request.URL.String(), cfg)
		},
	}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating remote image request")
	}
	request.Header.Set("User-Agent", "gitignore/1.0")
	request.Header.Set("Accept", strings.Join(cfg.AllowedTypes, ", "))
	response, err := client.Do(request)
	if err != nil {
		return nil, "", fmt.Errorf("fetching remote image")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("remote image returned HTTP %d", response.StatusCode)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	allowed := false
	for _, kind := range cfg.AllowedTypes {
		if contentType == strings.ToLower(kind) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, "", fmt.Errorf("remote image MIME type is not allowed")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, cfg.MaxSize+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading remote image")
	}
	if int64(len(body)) > cfg.MaxSize {
		return nil, "", fmt.Errorf("remote image exceeds size limit")
	}
	return body, contentType, nil
}

func resolvePublicAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		if !isPublicAddress(address) {
			return nil, fmt.Errorf("remote image host is not public")
		}
		return []netip.Addr{address.Unmap()}, nil
	}
	resolved, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("remote image DNS lookup failed")
	}
	addresses := make([]netip.Addr, 0, len(resolved))
	for _, address := range resolved {
		address = address.Unmap()
		if !isPublicAddress(address) {
			return nil, fmt.Errorf("remote image host resolves to a non-public address")
		}
		addresses = append(addresses, address)
	}
	return addresses, nil
}

func isPublicAddress(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	blocked := []netip.Prefix{
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	for _, prefix := range blocked {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
