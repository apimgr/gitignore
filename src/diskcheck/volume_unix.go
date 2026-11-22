//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package diskcheck

import (
	"fmt"
	"os"
	"syscall"
)

type statCounter interface {
	~int | ~int32 | ~int64 | ~uint | ~uint32 | ~uint64
}

func counterValue[T statCounter](value T) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func saturatingProduct(left, right uint64) uint64 {
	if left != 0 && right > ^uint64(0)/left {
		return ^uint64(0)
	}
	return left * right
}

func measureVolume(path string) (VolumeUsage, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return VolumeUsage{}, err
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return VolumeUsage{}, err
	}
	blockSize := counterValue(fs.Bsize)
	totalBlocks := counterValue(fs.Blocks)
	freeBlocks := counterValue(fs.Bfree)
	availableBlocks := counterValue(fs.Bavail)
	if freeBlocks > totalBlocks {
		freeBlocks = totalBlocks
	}
	if availableBlocks > totalBlocks {
		availableBlocks = totalBlocks
	}
	total := saturatingProduct(totalBlocks, blockSize)
	free := saturatingProduct(availableBlocks, blockSize)
	usedBlocks := totalBlocks - freeBlocks
	usedBytes := saturatingProduct(usedBlocks, blockSize)
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
