package blocklist

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseEntriesProtectsNetworksAndNormalizesDomains(t *testing.T) {
	entries, skipped, err := parseEntries([]byte("# comment\n203.0.113.7\n10.0.0.0/8\n203.0.113.7\ninvalid\n"), "ip", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != "203.0.113.7/32" || skipped != 2 {
		t.Fatalf("IP entries/skipped = %v/%d", entries, skipped)
	}
	entries, skipped, err = parseEntries([]byte("*.Exämple.org\nexample.org\nbad..domain\n"), "domain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0] != "*.xn--exmple-cua.org" || entries[1] != "example.org" || skipped != 1 {
		t.Fatalf("domain entries/skipped = %v/%d", entries, skipped)
	}
}

func TestMatcherLoadsFreshIPAndDomainSources(t *testing.T) {
	securityDir := t.TempDir()
	blocklistDir := filepath.Join(securityDir, "blocklists")
	if err := os.MkdirAll(blocklistDir, 0700); err != nil {
		t.Fatal(err)
	}
	updated := time.Now().UTC().Format(time.RFC3339)
	for id, content := range map[string]string{
		FireHOLID: "203.0.113.0/24\n",
		"domains": "bad.example\n",
	} {
		if err := os.WriteFile(filepath.Join(blocklistDir, id+".txt"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(blocklistDir, id+".last_updated"), []byte(updated), 0600); err != nil {
			t.Fatal(err)
		}
	}
	enabled := true
	matcher := NewMatcher(securityDir, []Source{{ID: "domains", Type: "domain", URL: "https://lists.example/domains.txt", Enabled: &enabled}})
	now := time.Now().UTC()
	if err := matcher.Reload(now); err != nil {
		t.Fatal(err)
	}
	if source, matched := matcher.MatchIP("203.0.113.12", now); !matched || source != FireHOLID {
		t.Fatalf("IP match = %q, %v", source, matched)
	}
	if source, matched := matcher.MatchDomain("https://bad.example/path", now); !matched || source != "domains" {
		t.Fatalf("domain match = %q, %v", source, matched)
	}
	if source, matched := matcher.MatchDomain("https://good.example/path", now); matched {
		t.Fatalf("unexpected domain match from %q", source)
	}
	statuses := matcher.Status(now)
	if len(statuses) != 2 || statuses[0].State != "ok" || statuses[1].Entries != 1 {
		t.Fatalf("source statuses = %+v", statuses)
	}
	if matcher.MatchIP("not-an-ip", now); false {
		t.Fatal("invalid IP matched")
	}
}

func TestIPListExcludesConfiguredTrustedRanges(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	entries, skipped, err := parseEntries([]byte("198.51.100.1\n203.0.113.0/24\n"), "ip", trusted)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || skipped != 1 {
		t.Fatalf("entries/skipped = %v/%d", entries, skipped)
	}
}
