package main

import (
	"context"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/geoip"
	"github.com/apimgr/gitignore/src/scheduler"
)

// newGeoIP builds the GeoIP manager from config and opens any databases already
// present on disk (AI.md PART 19). Missing databases are expected on first run:
// lookups stay unavailable and fail open until the first scheduler download
// completes. The manager is nil-safe for callers when GeoIP is disabled, but it
// is always constructed so the config surface stays consistent.
func newGeoIP(cfg *config.Config, dataDir string) *geoip.Manager {
	gm := geoip.New(cfg.Server.GeoIP, dataDir)
	gm.Load()
	return gm
}

// geoipUpdateHandler adapts the manager's Update method to a scheduler handler
// for the geoip_update task (AI.md PART 18/19).
func geoipUpdateHandler(gm *geoip.Manager) scheduler.HandlerFunc {
	return func(ctx context.Context) error {
		return gm.Update(ctx)
	}
}

// bootstrapGeoIP triggers a best-effort first-run download when GeoIP is
// enabled but no database is present yet (AI.md PART 19: "downloaded on first
// run"). It runs in the background so a slow or offline mirror never blocks
// startup; the weekly scheduler task retries on the normal cadence.
func bootstrapGeoIP(ctx context.Context, gm *geoip.Manager) {
	if gm == nil || !gm.Enabled() || gm.Available() {
		return
	}
	go func() {
		_ = gm.Update(ctx)
	}()
}
