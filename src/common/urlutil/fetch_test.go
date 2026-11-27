package urlutil

import (
	"context"
	"net/netip"
	"testing"
)

func TestIsPublicAddressRejectsPrivateAndReservedRanges(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "100.64.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "::1", "fd00::1", "fe80::1", "2001:db8::1"} {
		if isPublicAddress(netip.MustParseAddr(address)) {
			t.Errorf("address %q was considered public", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !isPublicAddress(netip.MustParseAddr(address)) {
			t.Errorf("public address %q was rejected", address)
		}
	}
}

func TestValidateRemoteURLRejectsUnsafeInputs(t *testing.T) {
	cfg := DefaultFetchRemoteImageConfig()
	for _, rawURL := range []string{
		"http://example.com/image.png",
		"https://user:password@example.com/image.png",
		"https://127.0.0.1/image.png",
		"https://[::1]/image.png",
		"https://service.internal/image.png",
		"https://example.com:8443/image.png",
	} {
		if err := ValidateRemoteURL(context.Background(), rawURL, cfg); err == nil {
			t.Errorf("ValidateRemoteURL(%q) succeeded", rawURL)
		}
	}
}
