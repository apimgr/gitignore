package server

import (
	"github.com/apimgr/gitignore/src/server/metrics"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

func systemMetricsSnapshot(dataDir string) metrics.SystemSnapshot {
	snapshot := metrics.SystemSnapshot{DiskPath: dataDir}
	if values, err := cpu.Percent(0, false); err == nil && len(values) > 0 {
		snapshot.CPUPercent = values[0]
	}
	if memory, err := mem.VirtualMemory(); err == nil {
		snapshot.MemoryPercent = memory.UsedPercent
		snapshot.MemoryUsedBytes = float64(memory.Used)
		snapshot.MemoryTotalBytes = float64(memory.Total)
	}
	if dataDir != "" {
		if usage, err := disk.Usage(dataDir); err == nil {
			snapshot.DiskPercent = usage.UsedPercent
			snapshot.DiskUsedBytes = float64(usage.Used)
			snapshot.DiskTotalBytes = float64(usage.Total)
		}
	}
	return snapshot
}
