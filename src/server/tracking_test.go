package server

import (
	"strings"
	"testing"

	"github.com/apimgr/gitignore/src/config"
)

func TestTrackingProviderPreservesConfiguredPathAndUsesOriginForCSP(t *testing.T) {
	base := "https://analytics.example/base/path"
	tests := []struct {
		name   string
		typeID string
		id     string
		want   string
	}{
		{name: "matomo", typeID: "matomo", id: "1", want: base + "/matomo.php"},
		{name: "owa", typeID: "owa", id: "site", want: base + "/"},
		{name: "fathom", typeID: "fathom", id: "site", want: base + "/tracker.js"},
		{name: "plausible", typeID: "plausible", id: "example.com", want: base + "/js/script.js"},
		{name: "umami", typeID: "umami", id: "site", want: base + "/script.js"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := trackingProviderFor(config.TrackingConfig{Type: tt.typeID, ID: tt.id, URL: base})
			combined := string(provider.tags) + provider.body
			if !strings.Contains(combined, tt.want) {
				t.Errorf("provider omitted configured path %q: %s", tt.want, combined)
			}
			if len(provider.hosts) == 0 || provider.hosts[len(provider.hosts)-1] != "https://analytics.example" {
				t.Errorf("CSP hosts = %v, want origin https://analytics.example", provider.hosts)
			}
			if (tt.typeID == "matomo" || tt.typeID == "owa") && provider.tags != "" {
				t.Errorf("same-origin tracking.js tag duplicated in provider tags: %s", provider.tags)
			}
		})
	}
}
