package main

import (
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// GlobalMemoryStatusEx e GetProcessMemoryInfo não têm wrapper em
// golang.org/x/sys/windows; chamadas direto nas DLLs do sistema.
var (
	modkernel32              = windows.NewLazySystemDLL("kernel32.dll")
	modpsapi                 = windows.NewLazySystemDLL("psapi.dll")
	procGlobalMemoryStatusEx = modkernel32.NewProc("GlobalMemoryStatusEx")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")
)

// memoryStatusEx espelha MEMORYSTATUSEX.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// processMemoryCounters espelha PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

func readProcessRSSMB() float64 {
	// Working set = equivalente ao RSS do Linux.
	var pmc processMemoryCounters
	pmc.Cb = uint32(unsafe.Sizeof(pmc))
	r, _, _ := procGetProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&pmc)),
		uintptr(pmc.Cb),
	)
	if r == 0 {
		return 0
	}
	return float64(pmc.WorkingSetSize) / (1024 * 1024)
}

// readProcessCpuTime devolve o tempo de CPU (kernel+user) consumido pelo
// processo. FILETIME aqui é uma duração em unidades de 100ns.
func readProcessCpuTime() time.Duration {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &creation, &exit, &kernel, &user); err != nil {
		return 0
	}
	return time.Duration(filetimeTicks(kernel)+filetimeTicks(user)) * 100
}

func filetimeTicks(ft windows.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

func readHostMemory() (totalMB, usedMB, freeMB, availMB, percent float64) {
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 || ms.TotalPhys == 0 {
		return 0, 0, 0, 0, 0
	}

	// Windows não separa "free" de "available" como o /proc/meminfo:
	// AvailPhys já inclui standby/cache reaproveitável.
	totalMB = float64(ms.TotalPhys) / (1024 * 1024)
	availMB = float64(ms.AvailPhys) / (1024 * 1024)
	freeMB = availMB
	usedMB = totalMB - availMB
	percent = (usedMB / totalMB) * 100.0
	return
}

// readHostLoad: Windows não tem load average.
func readHostLoad() (load1, load5, load15 float64) {
	return 0, 0, 0
}

func readHostDisk(path string) (totalGB, usedGB, freeGB, percent float64) {
	if path == "" {
		path = "."
	}
	// GetDiskFreeSpaceEx exige diretório; o path recebido é o arquivo do banco.
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, 0, 0, 0
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(abs))
	if err != nil {
		return 0, 0, 0, 0
	}

	var freeBytes, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(dir, &freeBytes, &totalBytes, &totalFree); err != nil {
		return 0, 0, 0, 0
	}

	if totalBytes > 0 {
		usedBytes := totalBytes - freeBytes
		totalGB = float64(totalBytes) / (1024 * 1024 * 1024)
		freeGB = float64(freeBytes) / (1024 * 1024 * 1024)
		usedGB = float64(usedBytes) / (1024 * 1024 * 1024)
		percent = (usedGB / totalGB) * 100.0
	}
	return
}
