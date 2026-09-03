// Package geoip implements built-in GeoIP lookups and country-based risk
// signals (AI.md PART 19). Databases come from sapics/ip-location-db in MMDB
// format and are never embedded — they download on first run and refresh via
// the scheduler's geoip_update task.
//
// GeoIP is a risk signal, never the sole access gate: a missing, stale, or
// failed lookup always fails open so the rest of the security pipeline (rate
// limiting, authentication, authorization) still runs. Private, loopback, and
// link-local addresses are never country-blocked.
//
// The reader interface decouples lookup and decision logic from the maxminddb
// library so those paths are testable without real MMDB files.
package geoip

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/oschwald/maxminddb-golang"

	"github.com/apimgr/gitignore/src/config"
)

// File names for the downloaded MMDB databases (AI.md PART 19 "Database
// Sources"). WHOIS has no file — it joins ASN and Country at query time.
const (
	fileASN     = "asn.mmdb"
	fileCountry = "country.mmdb"
	fileCityV4  = "dbip-city-ipv4.mmdb"
	fileCityV6  = "dbip-city-ipv6.mmdb"
)

// asnRecord maps the ip-location-db ASN MMDB fields (tag names per
// ~/.claude/memory/security_conventions.md "Go struct definitions").
type asnRecord struct {
	ASN uint32 `maxminddb:"autonomous_system_number"`
	Org string `maxminddb:"autonomous_system_organization"`
}

// countryRecord maps the ip-location-db Country MMDB field.
type countryRecord struct {
	CountryCode string `maxminddb:"country_code"`
}

// cityRecord maps the ip-location-db City MMDB fields.
type cityRecord struct {
	City        string  `maxminddb:"city"`
	CountryCode string  `maxminddb:"country_code"`
	Latitude    float64 `maxminddb:"latitude"`
	Longitude   float64 `maxminddb:"longitude"`
	Postcode    string  `maxminddb:"postcode"`
	State1      string  `maxminddb:"state1"`
	State2      string  `maxminddb:"state2"`
	Timezone    string  `maxminddb:"timezone"`
}

// ASNInfo is the result of an ASN lookup.
type ASNInfo struct {
	Number uint32
	Org    string
}

// CountryInfo is the result of a country lookup.
type CountryInfo struct {
	CountryCode string
}

// CityInfo is the result of a city lookup.
type CityInfo struct {
	City        string
	CountryCode string
	State1      string
	State2      string
	Postcode    string
	Timezone    string
	Latitude    float64
	Longitude   float64
}

// WhoisInfo joins ASN and country data at query time (AI.md PART 19: WHOIS is
// not a separate download).
type WhoisInfo struct {
	RegistrantOrg string
	ASN           uint32
	CountryCode   string
}

// mmReader is the subset of *maxminddb.Reader that the manager needs. Tests
// substitute a fake implementation.
type mmReader interface {
	Lookup(ip net.IP, result any) error
	Close() error
}

// Manager owns the open MMDB readers and the country-blocking policy. All
// public methods are safe for concurrent use; readers are swapped atomically
// under a write lock during Load.
type Manager struct {
	mu  sync.RWMutex
	cfg config.GeoIPConfig
	dir string

	asn     mmReader
	country mmReader
	cityV4  mmReader
	cityV6  mmReader

	// openReader opens an MMDB file; swappable in tests. Defaults to openMMDB.
	openReader func(path string) (mmReader, error)

	// mirrorBase, when set, overrides the per-file download host (tests point
	// this at an httptest server). Empty means use the AI.md PART 19 URLs.
	mirrorBase string
}

// New builds a Manager from config. When cfg.Dir is empty the storage path is
// derived as {dataDir}/security/geoip (AI.md PART 19). No files are opened
// until Load is called.
func New(cfg config.GeoIPConfig, dataDir string) *Manager {
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		dir = filepath.Join(dataDir, "security", "geoip")
	}
	return &Manager{
		cfg:        cfg,
		dir:        dir,
		openReader: openMMDB,
	}
}

// openMMDB opens a real MMDB file behind the mmReader interface.
func openMMDB(path string) (mmReader, error) {
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// Enabled reports whether GeoIP is turned on in config.
func (m *Manager) Enabled() bool { return m.cfg.Enabled }

// Dir returns the resolved GeoIP storage directory.
func (m *Manager) Dir() string { return m.dir }

// Load opens whichever configured databases are present on disk. Missing files
// leave that lookup unavailable and never produce an error, so the server
// starts normally before the first download completes (AI.md PART 19
// graceful-degradation rule). A present-but-corrupt file is logged and treated
// as unavailable. Load is idempotent: it closes any previously opened readers
// first, so it doubles as the post-update reload.
func (m *Manager) Load() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeLocked()
	if !m.cfg.Enabled {
		return
	}
	if m.cfg.Databases.ASN {
		m.asn = m.tryOpen(fileASN)
	}
	if m.cfg.Databases.Country {
		m.country = m.tryOpen(fileCountry)
	}
	if m.cfg.Databases.City {
		m.cityV4 = m.tryOpen(fileCityV4)
		m.cityV6 = m.tryOpen(fileCityV6)
	}
}

// tryOpen opens a database file by name, returning nil when the file is absent
// or cannot be parsed. Caller must hold the write lock.
func (m *Manager) tryOpen(name string) mmReader {
	path := filepath.Join(m.dir, name)
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	r, err := m.openReader(path)
	if err != nil {
		log.Printf("geoip: failed to open %s: %v (lookups for this database disabled)", name, err)
		return nil
	}
	return r
}

// closeLocked closes and clears every open reader. Caller must hold the write
// lock.
func (m *Manager) closeLocked() {
	for _, r := range []mmReader{m.asn, m.country, m.cityV4, m.cityV6} {
		if r != nil {
			_ = r.Close()
		}
	}
	m.asn, m.country, m.cityV4, m.cityV6 = nil, nil, nil, nil
}

// Close releases all open database readers.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeLocked()
	return nil
}

// Available reports whether at least one database is currently loaded.
func (m *Manager) Available() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.asn != nil || m.country != nil || m.cityV4 != nil || m.cityV6 != nil
}

// LookupCountry resolves the ISO 3166-1 alpha-2 country code for ip. The second
// return is false when the country database is unavailable or has no record.
func (m *Manager) LookupCountry(ip net.IP) (CountryInfo, bool) {
	if ip == nil {
		return CountryInfo{}, false
	}
	m.mu.RLock()
	r := m.country
	m.mu.RUnlock()
	if r == nil {
		return CountryInfo{}, false
	}
	var rec countryRecord
	if err := r.Lookup(ip, &rec); err != nil || rec.CountryCode == "" {
		return CountryInfo{}, false
	}
	return CountryInfo{CountryCode: strings.ToUpper(rec.CountryCode)}, true
}

// LookupASN resolves the autonomous system number and organization for ip.
func (m *Manager) LookupASN(ip net.IP) (ASNInfo, bool) {
	if ip == nil {
		return ASNInfo{}, false
	}
	m.mu.RLock()
	r := m.asn
	m.mu.RUnlock()
	if r == nil {
		return ASNInfo{}, false
	}
	var rec asnRecord
	if err := r.Lookup(ip, &rec); err != nil || (rec.ASN == 0 && rec.Org == "") {
		return ASNInfo{}, false
	}
	return ASNInfo{Number: rec.ASN, Org: rec.Org}, true
}

// LookupCity resolves city-level data for ip, selecting the IPv4 or IPv6
// database by address family.
func (m *Manager) LookupCity(ip net.IP) (CityInfo, bool) {
	if ip == nil {
		return CityInfo{}, false
	}
	m.mu.RLock()
	r := m.cityV4
	if ip.To4() == nil {
		r = m.cityV6
	}
	m.mu.RUnlock()
	if r == nil {
		return CityInfo{}, false
	}
	var rec cityRecord
	if err := r.Lookup(ip, &rec); err != nil {
		return CityInfo{}, false
	}
	if rec.City == "" && rec.CountryCode == "" {
		return CityInfo{}, false
	}
	return CityInfo{
		City:        rec.City,
		CountryCode: strings.ToUpper(rec.CountryCode),
		State1:      rec.State1,
		State2:      rec.State2,
		Postcode:    rec.Postcode,
		Timezone:    rec.Timezone,
		Latitude:    rec.Latitude,
		Longitude:   rec.Longitude,
	}, true
}

// LookupWhois joins the ASN and Country databases at query time (AI.md PART 19:
// WHOIS is not a separate download). It returns false only when neither source
// yields data.
func (m *Manager) LookupWhois(ip net.IP) (WhoisInfo, bool) {
	asn, aok := m.LookupASN(ip)
	country, cok := m.LookupCountry(ip)
	if !aok && !cok {
		return WhoisInfo{}, false
	}
	return WhoisInfo{
		RegistrantOrg: asn.Org,
		ASN:           asn.Number,
		CountryCode:   country.CountryCode,
	}, true
}

// CountryAllowed reports whether a request from ip is permitted by the country
// policy, plus the resolved country code (may be empty). It is fail-open: a
// nil/private/loopback address, a missing country database, an unresolved
// country, or any lookup error all return allowed=true. Country blocking is a
// risk signal, not the sole access gate (AI.md PART 19).
//
// Mode: allow_countries (allowlist) wins when set; otherwise deny_countries
// (blocklist); otherwise everything is allowed.
func (m *Manager) CountryAllowed(ip net.IP) (allowed bool, country string) {
	if ip == nil || isPrivate(ip) {
		return true, ""
	}
	info, ok := m.LookupCountry(ip)
	if !ok {
		return true, ""
	}
	code := info.CountryCode
	if len(m.cfg.AllowCountries) > 0 {
		return containsCode(m.cfg.AllowCountries, code), code
	}
	if len(m.cfg.DenyCountries) > 0 {
		return !containsCode(m.cfg.DenyCountries, code), code
	}
	return true, code
}

// isPrivate reports whether ip is a private, loopback, link-local, or
// unspecified address — categories that are never country-blocked (AI.md
// PART 19: RFC 1918 and internal IPs).
func isPrivate(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// containsCode reports whether code appears in list, comparing case-insensitively
// and trimming surrounding whitespace on each entry.
func containsCode(list []string, code string) bool {
	for _, c := range list {
		if strings.EqualFold(strings.TrimSpace(c), code) {
			return true
		}
	}
	return false
}
