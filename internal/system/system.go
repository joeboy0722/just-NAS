package system

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemStatus 結構定義系統當前狀態與效能指標
type SystemStatus struct {
	Hostname      string    `json:"hostname"`
	OSInfo        string    `json:"os_info"`
	Arch          string    `json:"arch"`
	UptimeSeconds uint64    `json:"uptime_seconds"`
	UptimeHours   float64   `json:"uptime_hours"`
	CPUUsagePct   float64   `json:"cpu_usage_pct"`
	RAMTotal      uint64    `json:"ram_total"`
	RAMUsed       uint64    `json:"ram_used"`
	RAMFree       uint64    `json:"ram_free"`
	RAMUsagePct   float64   `json:"ram_usage_pct"`
	LoadAvg       []float64 `json:"load_avg"`
	Timestamp     time.Time `json:"timestamp"`
}

// GetStatus 讀取並返回系統狀態資訊
func GetStatus() (*SystemStatus, error) {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "Alpine-NAS"
	}

	status := &SystemStatus{
		Hostname:  hostname,
		OSInfo:    fmtOSInfo(),
		Arch:      runtime.GOARCH,
		LoadAvg:   []float64{0.0, 0.0, 0.0},
		Timestamp: time.Now(),
	}

	if runtime.GOOS == "linux" {
		readLinuxProcSystemInfo(status)
	} else {
		return nil, fmt.Errorf("系統監控需要 Linux 環境")
	}

	return status, nil
}

func fmtOSInfo() string {
	if runtime.GOOS == "linux" {
		return "Alpine Linux"
	}
	return runtime.GOOS
}

// readLinuxProcSystemInfo 讀取 Linux /proc 虛擬檔案系統獲取系統真實狀態
func readLinuxProcSystemInfo(status *SystemStatus) {
	// 讀取 /proc/uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) > 0 {
			if uptime, err := strconv.ParseFloat(parts[0], 64); err == nil {
				status.UptimeSeconds = uint64(uptime)
				status.UptimeHours = uptime / 3600.0
			}
		}
	}

	// 讀取 /proc/meminfo
	if file, err := os.Open("/proc/meminfo"); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		var memTotal, memFree, buffers, cached, available uint64

		for scanner.Scan() {
			line := scanner.Text()
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			val, _ := strconv.ParseUint(fields[1], 10, 64)
			switch fields[0] {
			case "MemAvailable:":
				available = val * 1024
			case "MemTotal:":
				memTotal = val * 1024 // 轉 bytes
			case "MemFree:":
				memFree = val * 1024
			case "Buffers:":
				buffers = val * 1024
			case "Cached:":
				cached = val * 1024
			}
		}

		if memTotal > 0 {
			actualFree := memFree + buffers + cached
			if available > 0 {
				actualFree = available
			}
			if actualFree > memTotal {
				actualFree = memTotal
			}
			memUsed := memTotal - actualFree
			status.RAMTotal = memTotal
			status.RAMUsed = memUsed
			status.RAMFree = actualFree
			status.RAMUsagePct = float64(memUsed) / float64(memTotal) * 100.0
		}
	}

	// 讀取 /proc/loadavg
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) >= 3 {
			l1, _ := strconv.ParseFloat(parts[0], 64)
			l5, _ := strconv.ParseFloat(parts[1], 64)
			l15, _ := strconv.ParseFloat(parts[2], 64)
			status.LoadAvg = []float64{l1, l5, l15}
		}
	}

	status.CPUUsagePct = cpuUsage()
}

var cpuMu sync.Mutex
var previousTotal, previousIdle uint64

func cpuUsage() float64 {
	cpuMu.Lock()
	defer cpuMu.Unlock()
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	f := strings.Fields(strings.SplitN(string(data), "\n", 2)[0])
	if len(f) < 5 {
		return 0
	}
	var total, idle uint64
	for i := 1; i < len(f) && i <= 8; i++ {
		v, _ := strconv.ParseUint(f[i], 10, 64)
		total += v
		if i == 4 || i == 5 {
			idle += v
		}
	}
	var usage float64
	if total > previousTotal && previousTotal != 0 && idle >= previousIdle {
		usage = 100 * (1 - float64(idle-previousIdle)/float64(total-previousTotal))
	}
	previousTotal, previousIdle = total, idle
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}
