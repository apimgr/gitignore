// Package diskcheck measures configured storage volumes and reports low space.
package diskcheck

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// Config controls the low-space checks and their notification callbacks.
type Config struct {
	DataDir          string
	LogDir           string
	BackupDir        string
	Enabled          bool
	ThresholdPercent float64
	Repeat           time.Duration
	Now              func() time.Time
	Notify           func(Event)
	Log              func(level, message string, fields map[string]interface{})
}

// Event describes a low-space or recovery event for one filesystem volume.
type Event struct {
	Path             string
	UsedPercent      float64
	FreeBytes        uint64
	ThresholdPercent float64
}

// VolumeUsage is a point-in-time measurement for one unique filesystem.
type VolumeUsage struct {
	Path        string
	UsedPercent float64
	FreeBytes   uint64
	TotalBytes  uint64
	VolumeID    string
}

// Checker tracks warning cadence independently for each filesystem volume.
type Checker struct {
	config Config
	mu     sync.Mutex
	alerts map[string]time.Time
}

// New constructs a checker. Omitted threshold and repeat values use the
// defaults from AI.md PART 17.
func New(config Config) *Checker {
	if config.ThresholdPercent <= 0 || config.ThresholdPercent > 100 {
		config.ThresholdPercent = 85
	}
	if config.Repeat <= 0 {
		config.Repeat = 24 * time.Hour
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Checker{config: config, alerts: make(map[string]time.Time)}
}

// Check measures each unique volume and emits low-space notifications according
// to the configured threshold, repeat interval, and recovery hysteresis.
func (c *Checker) Check(ctx context.Context) ([]VolumeUsage, error) {
	if !c.config.Enabled {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths := []string{c.config.DataDir, c.config.LogDir, c.config.BackupDir}
	volumes := make([]VolumeUsage, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	var failures []error
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return volumes, err
		}
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			failures = append(failures, fmt.Errorf("resolve disk path %q: %w", path, err))
			continue
		}
		usage, err := measureVolume(abs)
		if err != nil {
			failures = append(failures, fmt.Errorf("measure disk path %q: %w", abs, err))
			continue
		}
		if seen[usage.VolumeID] {
			continue
		}
		seen[usage.VolumeID] = true
		usage.Path = abs
		volumes = append(volumes, usage)
	}
	if len(volumes) > 0 {
		c.report(volumes)
	}
	if len(failures) > 0 {
		return volumes, errors.Join(failures...)
	}
	return volumes, nil
}

func (c *Checker) report(volumes []VolumeUsage) {
	now := c.config.Now().UTC()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, volume := range volumes {
		usage := Event{
			Path:             volume.Path,
			UsedPercent:      volume.UsedPercent,
			FreeBytes:        volume.FreeBytes,
			ThresholdPercent: c.config.ThresholdPercent,
		}
		last, alerted := c.alerts[volume.VolumeID]
		if volume.UsedPercent >= c.config.ThresholdPercent {
			if !alerted || now.Sub(last) >= c.config.Repeat {
				fields := map[string]interface{}{
					"path":              volume.Path,
					"used_percent":      volume.UsedPercent,
					"free_bytes":        volume.FreeBytes,
					"threshold_percent": c.config.ThresholdPercent,
				}
				c.log("WARN", "Disk space low", fields)
				if c.config.Notify != nil {
					c.config.Notify(usage)
				}
				c.alerts[volume.VolumeID] = now
			}
			continue
		}
		if alerted && volume.UsedPercent <= c.config.ThresholdPercent-5 {
			c.log("INFO", "Disk space recovered", map[string]interface{}{
				"path":              volume.Path,
				"used_percent":      volume.UsedPercent,
				"free_bytes":        volume.FreeBytes,
				"threshold_percent": c.config.ThresholdPercent,
			})
			delete(c.alerts, volume.VolumeID)
		}
	}
}

func (c *Checker) log(level, message string, fields map[string]interface{}) {
	if c.config.Log != nil {
		c.config.Log(level, message, fields)
	}
}
