//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package diskcheck

import (
	"errors"
)

func measureVolume(string) (VolumeUsage, error) {
	return VolumeUsage{}, errors.New("disk usage measurement is unsupported on this platform")
}
