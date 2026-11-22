//go:build windows

package diskcheck

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func measureVolume(path string) (VolumeUsage, error) {
	volume := filepath.VolumeName(path)
	if volume == "" {
		return VolumeUsage{}, fmt.Errorf("cannot determine volume for %q", path)
	}
	root := volume + `\`
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return VolumeUsage{}, err
	}
	var available, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(rootPtr, &available, &total, &free); err != nil {
		return VolumeUsage{}, err
	}
	usedPercent := 0.0
	if total > 0 {
		usedPercent = float64(total-free) * 100 / float64(total)
	}
	return VolumeUsage{
		UsedPercent: usedPercent,
		FreeBytes:   available,
		TotalBytes:  total,
		VolumeID:    "volume:" + strings.ToLower(volume),
	}, nil
}
