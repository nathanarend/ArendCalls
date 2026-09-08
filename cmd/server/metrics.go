package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type ProcessMetrics struct {
	UptimeSeconds     int64   `json:"uptimeSeconds"`
	UptimeFormatted   string  `json:"uptimeFormatted"`
	MemoryAllocMB     float64 `json:"memoryAllocMb"`
	MemorySysMB       float64 `json:"memorySysMb"`
	MemoryHeapAllocMB float64 `json:"memoryHeapAllocMb"`
	MemoryHeapInuseMB float64 `json:"memoryHeapInuseMb"`
	MemoryRSSMB       float64 `json:"memoryRssMb"`
	CpuPercent        float64 `json:"cpuPercent"`
	Goroutines        int     `json:"goroutines"`
	NumGC             uint32  `json:"numGc"`
	NumCPU            int     `json:"numCpu"`
	GoVersion         string  `json:"goVersion"`
	ActiveCalls       int     `json:"activeCalls"`
	TotalSessions     int     `json:"totalSessions"`
	ConnectedSessions int     `json:"connectedSessions"`
}

type HostMetrics struct {
	MemTotalMB       float64 `json:"memTotalMb"`
	MemUsedMB        float64 `json:"memUsedMb"`
	MemFreeMB        float64 `json:"memFreeMb"`
	MemAvailableMB   float64 `json:"memAvailableMb"`
	MemUsagePercent  float64 `json:"memUsagePercent"`
	Load1            float64 `json:"load1"`
	Load5            float64 `json:"load5"`
	Load15           float64 `json:"load15"`
	CPUCores         int     `json:"cpuCores"`
	DiskTotalGB      float64 `json:"diskTotalGb"`
	DiskUsedGB       float64 `json:"diskUsedGb"`
	DiskFreeGB       float64 `json:"diskFreeGb"`
	DiskUsagePercent float64 `json:"diskUsagePercent"`
}

type SystemMetricsResponse struct {
	Process   ProcessMetrics `json:"process"`
	Host      HostMetrics    `json:"host"`
	Timestamp int64          `json:"timestamp"`
}

var (
	procCpuMu        sync.Mutex
	procLastTicks    uint64
	procLastTime     time.Time
	procLastUsagePct float64
)

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func readProcessRSSMB() float64 {
	// Read /proc/self/statm on Linux (fields: size resident shared text lib data dt)
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 2 {
		pages, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			pageSize := uint64(os.Getpagesize())
			bytes := pages * pageSize
			return float64(bytes) / (1024 * 1024)
		}
	}
	return 0
}

func readProcessCpuTicks() uint64 {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	str := string(data)
	idx := strings.LastIndex(str, ")")
	if idx < 0 || idx+1 >= len(str) {
		return 0
	}
	fields := strings.Fields(str[idx+1:])
	if len(fields) < 13 {
		return 0
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	return utime + stime
}

func readProcessCpuPercent() float64 {
	procCpuMu.Lock()
	defer procCpuMu.Unlock()

	now := time.Now()
	ticks := readProcessCpuTicks()
	if ticks == 0 {
		return 0
	}

	if procLastTime.IsZero() {
		procLastTime = now
		procLastTicks = ticks
		procLastUsagePct = 0
		return 0
	}

	deltaSec := now.Sub(procLastTime).Seconds()
	if deltaSec >= 0.5 {
		deltaTicks := float64(ticks - procLastTicks)
		numCPU := float64(runtime.NumCPU())
		if numCPU <= 0 {
			numCPU = 1
		}
		// (deltaTicks / 100.0) = segundos de CPU consumidos pelo processo no intervalo
		// (deltaSec * numCPU) = capacidade total de segundos de processamento de todos os núcleos da VPS
		pct := ((deltaTicks / 100.0) / (deltaSec * numCPU)) * 100.0
		if pct < 0 {
			pct = 0
		}
		procLastUsagePct = pct
		procLastTicks = ticks
		procLastTime = now
	}
	return procLastUsagePct
}

func readHostMemory() (totalMB, usedMB, freeMB, availMB, percent float64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, 0, 0
	}
	defer file.Close()

	var totalKB, freeKB, availKB, buffersKB, cachedKB uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[1]), "kB"))
		val, _ := strconv.ParseUint(valStr, 10, 64)

		switch key {
		case "MemTotal":
			totalKB = val
		case "MemFree":
			freeKB = val
		case "MemAvailable":
			availKB = val
		case "Buffers":
			buffersKB = val
		case "Cached":
			cachedKB = val
		}
	}

	if totalKB > 0 {
		if availKB == 0 {
			// Fallback calculation if MemAvailable is missing
			availKB = freeKB + buffersKB + cachedKB
		}
		usedKB := totalKB - availKB
		totalMB = float64(totalKB) / 1024.0
		availMB = float64(availKB) / 1024.0
		freeMB = float64(freeKB) / 1024.0
		usedMB = float64(usedKB) / 1024.0
		percent = (usedMB / totalMB) * 100.0
	}
	return
}

func readHostLoad() (load1, load5, load15 float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		load1, _ = strconv.ParseFloat(fields[0], 64)
		load5, _ = strconv.ParseFloat(fields[1], 64)
		load15, _ = strconv.ParseFloat(fields[2], 64)
	}
	return
}

func readHostDisk(path string) (totalGB, usedGB, freeGB, percent float64) {
	var stat syscall.Statfs_t
	if path == "" {
		path = "."
	}
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, 0, 0
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	if totalBytes > 0 {
		usedBytes := totalBytes - freeBytes
		totalGB = float64(totalBytes) / (1024 * 1024 * 1024)
		freeGB = float64(freeBytes) / (1024 * 1024 * 1024)
		usedGB = float64(usedBytes) / (1024 * 1024 * 1024)
		percent = (usedGB / totalGB) * 100.0
	}
	return
}

func (s *server) handleSystemMetrics(w http.ResponseWriter, r *http.Request) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	totalSessions, connectedSessions := s.sessions.SessionCounts()
	activeCalls := s.broker.ActiveCallCount()

	uptime := time.Since(s.startTime)
	rssMB := readProcessRSSMB()
	if rssMB == 0 {
		rssMB = float64(memStats.Sys) / (1024 * 1024)
	}
	cpuPercent := readProcessCpuPercent()

	hostMemTotal, hostMemUsed, hostMemFree, hostMemAvail, hostMemPercent := readHostMemory()
	load1, load5, load15 := readHostLoad()
	diskTotal, diskUsed, diskFree, diskPercent := readHostDisk(s.dbPath)

	resp := SystemMetricsResponse{
		Timestamp: time.Now().UnixMilli(),
		Process: ProcessMetrics{
			UptimeSeconds:     int64(uptime.Seconds()),
			UptimeFormatted:   formatDuration(uptime),
			MemoryAllocMB:     float64(memStats.Alloc) / (1024 * 1024),
			MemorySysMB:       float64(memStats.Sys) / (1024 * 1024),
			MemoryHeapAllocMB: float64(memStats.HeapAlloc) / (1024 * 1024),
			MemoryHeapInuseMB: float64(memStats.HeapInuse) / (1024 * 1024),
			MemoryRSSMB:       rssMB,
			CpuPercent:        cpuPercent,
			Goroutines:        runtime.NumGoroutine(),
			NumGC:             memStats.NumGC,
			NumCPU:            runtime.NumCPU(),
			GoVersion:         runtime.Version(),
			ActiveCalls:       activeCalls,
			TotalSessions:     totalSessions,
			ConnectedSessions: connectedSessions,
		},
		Host: HostMetrics{
			MemTotalMB:       hostMemTotal,
			MemUsedMB:        hostMemUsed,
			MemFreeMB:        hostMemFree,
			MemAvailableMB:   hostMemAvail,
			MemUsagePercent:  hostMemPercent,
			Load1:            load1,
			Load5:            load5,
			Load15:           load15,
			CPUCores:         runtime.NumCPU(),
			DiskTotalGB:      diskTotal,
			DiskUsedGB:       diskUsed,
			DiskFreeGB:       diskFree,
			DiskUsagePercent: diskPercent,
		},
	}

	writeJSON(w, http.StatusOK, resp)
}
