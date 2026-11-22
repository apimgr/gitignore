//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package diskcheck

import (
	"fmt"
	"os"
	"syscall"
)

func measureVolume(path string) (VolumeUsage, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return VolumeUsage{}, err
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return VolumeUsage{}, err
	}
	blockSize := uint64(fs.Bsize)
	total := fs.Blocks * blockSize
	free := fs.Bavail * blockSize
	usedBlocks := fs.Blocks - fs.Bfree
	usedBytes := usedBlocks * blockSize
	usedPercent := 0.0
	if total > 0 {
		usedPercent = float64(usedBytes) * 100 / float64(total)
	}
	if _, err := os.Stat(path); err != nil {
		return VolumeUsage{}, err
	}
	return VolumeUsage{
		UsedPercent: usedPercent,
		FreeBytes:   free,
		TotalBytes:  total,
		VolumeID:    fmt.Sprintf("dev:%d", stat.Dev),
	}, nil
}
