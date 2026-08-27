// Package serverstat membaca pemakaian sumber daya server (CPU/RAM/swap/disk)
// langsung dari /proc + statfs — tanpa dependensi eksternal (setia DNA Lamund).
// Hanya berfungsi di Linux (server data plane); parser murni diuji lintas OS.
package serverstat

import (
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Stats adalah snapshot pemakaian sumber daya (byte, kecuali CPUPercent).
type Stats struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsed    int64   `json:"mem_used"`
	MemTotal   int64   `json:"mem_total"`
	SwapUsed   int64   `json:"swap_used"`
	SwapTotal  int64   `json:"swap_total"`
	DiskUsed   int64   `json:"disk_used"`
	DiskTotal  int64   `json:"disk_total"`
}

// Collect mengumpulkan statistik; diskPath menentukan filesystem yang diukur.
func Collect(diskPath string) (Stats, error) {
	var s Stats

	mem, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return s, err
	}
	memTotal, memAvail, swapTotal, swapFree := parseMeminfo(string(mem))
	s.MemTotal, s.MemUsed = memTotal, memTotal-memAvail
	s.SwapTotal, s.SwapUsed = swapTotal, swapTotal-swapFree

	// CPU: dua sampel /proc/stat dengan jeda pendek.
	c1, _ := os.ReadFile("/proc/stat")
	t1, i1 := parseCPUStat(string(c1))
	time.Sleep(180 * time.Millisecond)
	c2, _ := os.ReadFile("/proc/stat")
	t2, i2 := parseCPUStat(string(c2))
	s.CPUPercent = cpuPercent(t1, i1, t2, i2)

	var fs syscall.Statfs_t
	if err := syscall.Statfs(diskPath, &fs); err == nil {
		bsize := int64(fs.Bsize)
		s.DiskTotal = int64(fs.Blocks) * bsize
		s.DiskUsed = s.DiskTotal - int64(fs.Bavail)*bsize
	}
	return s, nil
}

// parseMeminfo mengembalikan byte untuk MemTotal, MemAvailable, SwapTotal, SwapFree.
func parseMeminfo(content string) (memTotal, memAvail, swapTotal, swapFree int64) {
	for _, line := range strings.Split(content, "\n") {
		key, val, ok := kv(line)
		if !ok {
			continue
		}
		switch key {
		case "MemTotal":
			memTotal = val * 1024
		case "MemAvailable":
			memAvail = val * 1024
		case "SwapTotal":
			swapTotal = val * 1024
		case "SwapFree":
			swapFree = val * 1024
		}
	}
	return
}

// kv memparse baris meminfo "Key:   123 kB".
func kv(line string) (string, int64, bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", 0, false
	}
	key := strings.TrimSpace(line[:i])
	fields := strings.Fields(line[i+1:])
	if len(fields) == 0 {
		return "", 0, false
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return key, n, true
}

// parseCPUStat mengembalikan (total jiffies, idle jiffies) dari baris "cpu ...".
func parseCPUStat(content string) (total, idle uint64) {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:] // user nice system idle iowait irq softirq steal ...
		for i, f := range fields {
			n, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				continue
			}
			total += n
			if i == 3 { // idle
				idle += n
			}
			if i == 4 { // iowait dihitung sebagai idle juga
				idle += n
			}
		}
		return
	}
	return
}

// cpuPercent menghitung persen pemakaian dari dua sampel.
func cpuPercent(t1, i1, t2, i2 uint64) float64 {
	dt, di := int64(t2-t1), int64(i2-i1)
	if dt <= 0 {
		return 0
	}
	p := (1 - float64(di)/float64(dt)) * 100
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return math.Round(p*10) / 10 // 1 desimal
}
