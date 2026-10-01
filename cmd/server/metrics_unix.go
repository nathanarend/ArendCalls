//go:build !windows

package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

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

// readProcessCpuTime devolve o tempo de CPU (user+system) consumido pelo
// processo. /proc/self/stat reporta em clock ticks de USER_HZ (100/s).
func readProcessCpuTime() time.Duration {
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
	return time.Duration(utime+stime) * (time.Second / 100)
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
