package blocklist

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

const (
	FireHOLID      = "firehol_level1"
	FireHOLURL     = "https://raw.githubusercontent.com/firehol/blocklist-ipsets/master/firehol_level1.netset"
	maxSourceBytes = 64 << 20
)

var sourceIDPattern = regexp.MustCompile(`^[a-z0-9_]+$`)

// Source describes one operator-configured blocklist source.
type Source struct {
	ID      string `yaml:"id"`
	Type    string `yaml:"type"`
	URL     string `yaml:"url"`
	Enabled *bool  `yaml:"enabled"`
}

// Config contains the policy and paths required to update blocklist files.
type Config struct {
	DataDir         string
	SecurityDir     string
	Enabled         bool
	ExtraSources    []Source
	TrustedPrefixes []netip.Prefix
	HTTPClient      *http.Client
}

// Result describes one successfully stored source without exposing its URL.
type Result struct {
	ID      string
	Entries int
	Skipped int
	Updated time.Time
}

// Update refreshes FireHOL and every enabled extra source. Each source is
// independently atomic: a failed or empty source never replaces its last good
// copy. A partial update returns the successful results and joined errors.
func Update(ctx context.Context, cfg Config) ([]Result, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	securityDir := strings.TrimSpace(cfg.SecurityDir)
	if securityDir == "" && strings.TrimSpace(cfg.DataDir) != "" {
		securityDir = filepath.Join(cfg.DataDir, "security")
	}
	if securityDir == "" {
		return nil, errors.New("blocklist: data directory is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 2 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Scheme != "https" {
					return errors.New("blocklist: HTTPS redirect required")
				}
				if len(via) >= 5 {
					return errors.New("blocklist: too many redirects")
				}
				return nil
			},
		}
	}
	sources := []Source{{ID: FireHOLID, Type: "ip", URL: FireHOLURL}}
	seen := map[string]bool{FireHOLID: true}
	for _, source := range cfg.ExtraSources {
		if source.Enabled != nil && !*source.Enabled {
			continue
		}
		if err := validateSource(source); err != nil {
			return nil, err
		}
		if seen[source.ID] {
			return nil, fmt.Errorf("blocklist: duplicate or reserved source id %q", source.ID)
		}
		seen[source.ID] = true
		sources = append(sources, source)
	}
	trusted := append([]netip.Prefix(nil), cfg.TrustedPrefixes...)
	for i := range trusted {
		if trusted[i].IsValid() {
			trusted[i] = trusted[i].Masked()
		}
	}
	if err := os.MkdirAll(filepath.Join(securityDir, "blocklists"), 0700); err != nil {
		return nil, fmt.Errorf("blocklist: create data directory: %w", err)
	}
	dir := filepath.Join(securityDir, "blocklists")
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("blocklist: secure data directory: %w", err)
	}
	results := make([]Result, 0, len(sources))
	var updateErrors []error
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return results, errors.Join(append(updateErrors, err)...)
		}
		result, err := updateSource(ctx, client, dir, source, trusted)
		if err != nil {
			log.Printf("blocklist: update failed for %s: %v", source.ID, err)
			updateErrors = append(updateErrors, fmt.Errorf("%s: %w", source.ID, err))
			continue
		}
		results = append(results, result)
	}
	return results, errors.Join(updateErrors...)
}

func validateSource(source Source) error {
	if source.ID == FireHOLID || !sourceIDPattern.MatchString(source.ID) {
		return fmt.Errorf("blocklist: invalid or reserved source id %q", source.ID)
	}
	if source.Type != "ip" && source.Type != "domain" {
		return fmt.Errorf("blocklist: source %q has unsupported type %q", source.ID, source.Type)
	}
	u, err := url.Parse(source.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return fmt.Errorf("blocklist: source %q URL must be HTTPS", source.ID)
	}
	return nil
}

func updateSource(ctx context.Context, client *http.Client, dir string, source Source, trusted []netip.Prefix) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/plain, */*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSourceBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("read source: %w", err)
	}
	if len(data) > maxSourceBytes {
		return Result{}, errors.New("source exceeds 64 MiB limit")
	}
	entries, skipped, err := parseEntries(data, source.Type, trusted)
	if err != nil {
		return Result{}, err
	}
	contents := []byte(strings.Join(entries, "\n") + "\n")
	if err := atomicWrite(filepath.Join(dir, source.ID+".txt"), contents, 0600); err != nil {
		return Result{}, fmt.Errorf("store source: %w", err)
	}
	now := time.Now().UTC()
	if err := atomicWrite(filepath.Join(dir, source.ID+".last_updated"), []byte(now.Format(time.RFC3339)+"\n"), 0600); err != nil {
		return Result{}, fmt.Errorf("write source timestamp: %w", err)
	}
	return Result{ID: source.ID, Entries: len(entries), Skipped: skipped, Updated: now}, nil
}

func parseEntries(data []byte, kind string, trusted []netip.Prefix) ([]string, int, error) {
	entries := make([]string, 0)
	seen := make(map[string]struct{})
	skipped := 0
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if i := strings.IndexAny(line, " \t;"); i >= 0 {
			line = line[:i]
		}
		if line == "" {
			continue
		}
		var normalized string
		var ok bool
		if kind == "ip" {
			var prefix netip.Prefix
			prefix, ok = parsePrefix(line)
			if ok && overlapsProtected(prefix, trusted) {
				ok = false
			}
			if ok {
				normalized = prefix.Masked().String()
			}
		} else {
			normalized, ok = normalizeDomainEntry(line)
		}
		if !ok {
			skipped++
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		entries = append(entries, normalized)
	}
	if err := scanner.Err(); err != nil {
		return nil, skipped, fmt.Errorf("scan source: %w", err)
	}
	if len(entries) == 0 {
		return nil, skipped, errors.New("source contains no valid entries")
	}
	sort.Strings(entries)
	return entries, skipped, nil
}

func parsePrefix(value string) (netip.Prefix, bool) {
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked(), true
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Prefix{}, false
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), true
}

var protectedPrefixes = func() []netip.Prefix {
	values := []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
		"169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "::/128",
		"::1/128", "fc00::/7", "fe80::/10",
	}
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}()

func overlapsProtected(candidate netip.Prefix, trusted []netip.Prefix) bool {
	for _, protected := range protectedPrefixes {
		if prefixOverlap(candidate, protected) {
			return true
		}
	}
	for _, allowed := range trusted {
		if allowed.IsValid() && prefixOverlap(candidate, allowed) {
			return true
		}
	}
	return false
}

func prefixOverlap(a, b netip.Prefix) bool {
	if a.Addr().Is4() != b.Addr().Is4() {
		return false
	}
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

func normalizeDomainEntry(value string) (string, bool) {
	wildcard := strings.HasPrefix(value, "*.")
	if wildcard {
		value = strings.TrimPrefix(value, "*.")
	}
	if strings.ContainsAny(value, "/:@[]") {
		return "", false
	}
	value = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
	ascii, err := idna.Lookup.ToASCII(value)
	if err != nil || ascii == "" || len(ascii) > 253 {
		return "", false
	}
	for _, label := range strings.Split(ascii, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return "", false
			}
		}
	}
	if addr, err := netip.ParseAddr(ascii); err == nil && addr.IsValid() {
		return "", false
	}
	if wildcard {
		if _, err := publicsuffix.EffectiveTLDPlusOne(ascii); err != nil {
			return "", false
		}
		return "*." + ascii, true
	}
	return ascii, true
}

func atomicWrite(path string, contents []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".blocklist-*.tmp")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = parent.Close() }()
	return parent.Sync()
}

// DomainMatches reports whether a normalized domain entry matches a domain.
func DomainMatches(entry, candidate string) bool {
	normalized, ok := normalizeDomainEntry(candidate)
	if !ok || strings.HasPrefix(normalized, "*.") {
		return false
	}
	if strings.HasPrefix(entry, "*.") {
		base := strings.TrimPrefix(entry, "*.")
		return normalized != base && strings.HasSuffix(normalized, "."+base)
	}
	return strings.EqualFold(entry, normalized)
}

// HostFromURL returns an IDNA-normalized hostname while ignoring IP literals.
func HostFromURL(value string) string {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		host = u.Path
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if addr, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil && addr.IsValid() {
		return ""
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return ""
	}
	return ascii
}

// ClientIPMatches reports whether an address is covered by a stored IP entry.
func ClientIPMatches(entry string, address net.IP) bool {
	prefix, ok := parsePrefix(entry)
	if !ok {
		return false
	}
	addr, ok := netip.AddrFromSlice(address)
	if !ok {
		return false
	}
	return prefix.Contains(addr.Unmap())
}
