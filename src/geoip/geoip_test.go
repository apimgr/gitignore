package geoip

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

// fakeReader is an in-memory mmReader for tests. It populates lookup records by
// result type, keyed on the string form of the IP.
type fakeReader struct {
	country map[string]string
	asn     map[string]asnRecord
	city    map[string]cityRecord
	closed  bool
}

func (f *fakeReader) Lookup(ip net.IP, result any) error {
	key := ip.String()
	switch v := result.(type) {
	case *countryRecord:
		v.CountryCode = f.country[key]
	case *asnRecord:
		*v = f.asn[key]
	case *cityRecord:
		*v = f.city[key]
	}
	return nil
}

func (f *fakeReader) Close() error {
	f.closed = true
	return nil
}

const t_dataDir = "/tmp/geoip-test-does-not-exist"

func TestCountryAllowed_DenyMode(t *testing.T) {
	m := New(config.GeoIPConfig{Enabled: true, DenyCountries: []string{"CN", "RU"}}, t_dataDir)
	m.country = &fakeReader{country: map[string]string{
		"1.2.3.4": "cn",
		"5.6.7.8": "us",
	}}

	if allowed, code := m.CountryAllowed(net.ParseIP("1.2.3.4")); allowed || code != "CN" {
		t.Fatalf("CN should be blocked: allowed=%v code=%q", allowed, code)
	}
	if allowed, code := m.CountryAllowed(net.ParseIP("5.6.7.8")); !allowed || code != "US" {
		t.Fatalf("US should be allowed: allowed=%v code=%q", allowed, code)
	}
}

func TestCountryAllowed_AllowModeWins(t *testing.T) {
	// Both lists set: allowlist takes precedence (AI.md PART 19).
	m := New(config.GeoIPConfig{
		Enabled:        true,
		AllowCountries: []string{"US"},
		DenyCountries:  []string{"US"},
	}, t_dataDir)
	m.country = &fakeReader{country: map[string]string{
		"5.6.7.8": "us",
		"9.9.9.9": "de",
	}}

	if allowed, _ := m.CountryAllowed(net.ParseIP("5.6.7.8")); !allowed {
		t.Fatal("US must be allowed in allowlist mode even when also in deny list")
	}
	if allowed, _ := m.CountryAllowed(net.ParseIP("9.9.9.9")); allowed {
		t.Fatal("DE must be blocked in allowlist mode")
	}
}

func TestCountryAllowed_PrivateIPNeverBlocked(t *testing.T) {
	m := New(config.GeoIPConfig{Enabled: true, DenyCountries: []string{"US"}}, t_dataDir)
	m.country = &fakeReader{country: map[string]string{"192.168.1.10": "us"}}

	for _, ip := range []string{"192.168.1.10", "127.0.0.1", "10.0.0.5", "::1"} {
		if allowed, code := m.CountryAllowed(net.ParseIP(ip)); !allowed || code != "" {
			t.Fatalf("private/loopback %s must never be blocked: allowed=%v code=%q", ip, allowed, code)
		}
	}
}

func TestCountryAllowed_MissingDBFailsOpen(t *testing.T) {
	m := New(config.GeoIPConfig{Enabled: true, DenyCountries: []string{"CN"}}, t_dataDir)
	// country reader nil (no database loaded).
	if allowed, code := m.CountryAllowed(net.ParseIP("1.2.3.4")); !allowed || code != "" {
		t.Fatalf("missing DB must fail open: allowed=%v code=%q", allowed, code)
	}
}

func TestLookupWhois_JoinsASNAndCountry(t *testing.T) {
	m := New(config.GeoIPConfig{Enabled: true}, t_dataDir)
	m.asn = &fakeReader{asn: map[string]asnRecord{
		"8.8.8.8": {ASN: 15169, Org: "GOOGLE"},
	}}
	m.country = &fakeReader{country: map[string]string{"8.8.8.8": "us"}}

	info, ok := m.LookupWhois(net.ParseIP("8.8.8.8"))
	if !ok {
		t.Fatal("expected whois result")
	}
	if info.ASN != 15169 || info.RegistrantOrg != "GOOGLE" || info.CountryCode != "US" {
		t.Fatalf("unexpected whois: %+v", info)
	}
}

func TestLookupCity_SelectsByFamily(t *testing.T) {
	m := New(config.GeoIPConfig{Enabled: true}, t_dataDir)
	m.cityV4 = &fakeReader{city: map[string]cityRecord{
		"8.8.8.8": {City: "Mountain View", CountryCode: "us", Timezone: "America/Los_Angeles"},
	}}
	m.cityV6 = &fakeReader{city: map[string]cityRecord{
		"2001:4860:4860::8888": {City: "V6City", CountryCode: "us"},
	}}

	if c, ok := m.LookupCity(net.ParseIP("8.8.8.8")); !ok || c.City != "Mountain View" {
		t.Fatalf("v4 city lookup failed: %+v ok=%v", c, ok)
	}
	if c, ok := m.LookupCity(net.ParseIP("2001:4860:4860::8888")); !ok || c.City != "V6City" {
		t.Fatalf("v6 city lookup failed: %+v ok=%v", c, ok)
	}
}

func TestLoad_MissingFilesNoPanic(t *testing.T) {
	dir := t.TempDir()
	m := New(config.GeoIPConfig{
		Enabled:   true,
		Dir:       dir,
		Databases: config.GeoIPDatabasesConfig{ASN: true, Country: true, City: true},
	}, dir)
	m.Load()
	if m.Available() {
		t.Fatal("no databases present, Available must be false")
	}
	// Lookups must fail open, not panic.
	if _, ok := m.LookupCountry(net.ParseIP("1.2.3.4")); ok {
		t.Fatal("lookup on missing DB must return false")
	}
}

func TestUpdate_DownloadsAndReloads(t *testing.T) {
	payload := []byte("fake-mmdb-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := New(config.GeoIPConfig{
		Enabled:   true,
		Dir:       dir,
		Databases: config.GeoIPDatabasesConfig{ASN: true, Country: true, City: true},
	}, dir)
	m.mirrorBase = srv.URL
	// Avoid parsing the fake bytes as a real MMDB during the post-update reload.
	m.openReader = func(path string) (mmReader, error) { return &fakeReader{}, nil }

	if err := m.Update(context.Background()); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	for _, name := range []string{fileASN, fileCountry, fileCityV4, fileCityV6} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("expected %s downloaded: %v", name, err)
		}
		if string(got) != string(payload) {
			t.Fatalf("%s content mismatch", name)
		}
	}
	if !m.Available() {
		t.Fatal("databases should be loaded after Update")
	}
}

func TestUpdate_DisabledIsNoop(t *testing.T) {
	dir := t.TempDir()
	m := New(config.GeoIPConfig{Enabled: false, Dir: dir}, dir)
	if err := m.Update(context.Background()); err != nil {
		t.Fatalf("disabled Update must be a no-op, got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("disabled Update must not write files")
	}
}

func TestDownloadFile_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "x.mmdb")
	err := downloadFile(context.Background(), srv.Client(), srv.URL, dest)
	if err == nil {
		t.Fatal("expected error on non-200 status")
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatal("no file should be written on failure")
	}
}
