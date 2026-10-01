package main

import (
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"
)

// Os leitores dependentes de SO (readProcessRSSMB, readProcessCpuTime,
// readHostMemory, readHostLoad, readHostDisk) ficam em metrics_unix.go
// (/proc + statfs) e metrics_windows.go (Win32). Retornam 0 quando a métrica
// não está disponível.

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
	procLastCPU      time.Duration
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

func readProcessCpuPercent() float64 {
	procCpuMu.Lock()
	defer procCpuMu.Unlock()

	now := time.Now()
	cpu := readProcessCpuTime()
	if cpu == 0 {
		return 0
	}

	if procLastTime.IsZero() {
		procLastTime = now
		procLastCPU = cpu
		procLastUsagePct = 0
		return 0
	}

	deltaSec := now.Sub(procLastTime).Seconds()
	if deltaSec >= 0.5 {
		deltaCPU := (cpu - procLastCPU).Seconds()
		numCPU := float64(runtime.NumCPU())
		if numCPU <= 0 {
			numCPU = 1
		}
		// deltaCPU = segundos de CPU consumidos pelo processo no intervalo
		// (deltaSec * numCPU) = capacidade total de segundos de processamento de todos os núcleos da VPS
		pct := (deltaCPU / (deltaSec * numCPU)) * 100.0
		if pct < 0 {
			pct = 0
		}
		procLastUsagePct = pct
		procLastCPU = cpu
		procLastTime = now
	}
	return procLastUsagePct
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
