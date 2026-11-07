package server

import (
	"net"
	"testing"
)

// TestBuildTrustedProxiesAlwaysIncludesDefaults verifies the always-trusted
// loopback/private/link-local ranges are present regardless of input.
func TestBuildTrustedProxiesAlwaysIncludesDefaults(t *testing.T) {
	nets := buildTrustedProxies("", nil)
	if len(nets) < len(alwaysTrustedCIDRs) {
		t.Fatalf("expected at least %d networks, got %d", len(alwaysTrustedCIDRs), len(nets))
	}
	ip := net.ParseIP("127.0.0.1")
	found := false
	for _, n := range nets {
		if n.Contains(ip) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 127.0.0.1 to be covered by an always-trusted CIDR")
	}
}

func TestBuildTrustedProxiesListenAddrWidening(t *testing.T) {
	nets := buildTrustedProxies("10.20.30.40:8080", nil)
	ip := net.ParseIP("10.20.30.99")
	found := false
	for _, n := range nets {
		if n.Contains(ip) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected same /24 as listen address to be trusted")
	}
}

func TestBuildTrustedProxiesWildcardBindAddsNoExtra(t *testing.T) {
	withWildcard := buildTrustedProxies("0.0.0.0:8080", nil)
	withoutAny := buildTrustedProxies("", nil)
	if len(withWildcard) != len(withoutAny) {
		t.Errorf("wildcard bind should not add a /24, got %d nets vs baseline %d", len(withWildcard), len(withoutAny))
	}
}

func TestBuildTrustedProxiesAdditionalEntries(t *testing.T) {
	cases := []struct {
		name    string
		entry   string
		checkIP string
	}{
		{"bare ipv4", "203.0.113.5", "203.0.113.5"},
		{"cidr", "198.51.100.0/24", "198.51.100.1"},
		{"whitespace trimmed", "  203.0.113.9  ", "203.0.113.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nets := buildTrustedProxies("", []string{tc.entry})
			ip := net.ParseIP(tc.checkIP)
			found := false
			for _, n := range nets {
				if n.Contains(ip) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q to cover %s", tc.entry, tc.checkIP)
			}
		})
	}
}

func TestBuildTrustedProxiesSkipsEmptyAndInvalid(t *testing.T) {
	baseline := buildTrustedProxies("", nil)
	nets := buildTrustedProxies("", []string{"", "   ", "not-an-ip-or-cidr-or-hostname!!"})
	if len(nets) != len(baseline) {
		t.Errorf("expected empty/invalid entries to be skipped, got %d nets vs baseline %d", len(nets), len(baseline))
	}
}

func TestParseListenIP(t *testing.T) {
	cases := []struct {
		name string
		addr string
		want string
	}{
		{"empty", "", ""},
		{"wildcard v4", "0.0.0.0", ""},
		{"wildcard v4 with port", "0.0.0.0:8080", ""},
		{"wildcard v6", "::", ""},
		{"bracketed wildcard v6 with port", "[::]:8080", ""},
		{"bare ip", "127.0.0.1", "127.0.0.1"},
		{"ip with port", "127.0.0.1:80", "127.0.0.1"},
		{"bracketed ipv6 with port", "[::1]:80", "::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseListenIP(tc.addr)
			if tc.want == "" {
				if got != nil {
					t.Errorf("parseListenIP(%q) = %v, want nil", tc.addr, got)
				}
				return
			}
			if got == nil || got.String() != tc.want {
				t.Errorf("parseListenIP(%q) = %v, want %s", tc.addr, got, tc.want)
			}
		})
	}
}

func TestIsTrustedPeer(t *testing.T) {
	s := &Server{trustedProxies: buildTrustedProxies("", []string{"203.0.113.5"})}

	cases := []struct {
		name string
		addr string
		want bool
	}{
		{"trusted loopback with port", "127.0.0.1:12345", true},
		{"trusted additional ip with port", "203.0.113.5:9999", true},
		{"untrusted public ip", "198.51.100.77:443", false},
		{"malformed address falls back to raw host", "not-an-address", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.isTrustedPeer(tc.addr); got != tc.want {
				t.Errorf("isTrustedPeer(%q) = %v, want %v", tc.addr, got, tc.want)
			}
		})
	}
}
