package serverstat

import "testing"

func TestParseMeminfo(t *testing.T) {
	in := `MemTotal:        6083104 kB
MemFree:          200000 kB
MemAvailable:    1800000 kB
Buffers:           50000 kB
SwapTotal:       2097148 kB
SwapFree:        1997148 kB
`
	mt, ma, st, sf := parseMeminfo(in)
	if mt != 6083104*1024 || ma != 1800000*1024 || st != 2097148*1024 || sf != 1997148*1024 {
		t.Fatalf("meminfo: %d %d %d %d", mt, ma, st, sf)
	}
}

func TestParseCPUStat(t *testing.T) {
	// user nice system idle iowait irq softirq steal
	in := "cpu  100 0 50 800 40 0 10 0\ncpu0 ...\n"
	total, idle := parseCPUStat(in)
	// total = 100+0+50+800+40+0+10+0 = 1000 ; idle = 800+40 = 840
	if total != 1000 || idle != 840 {
		t.Fatalf("cpustat total=%d idle=%d", total, idle)
	}
}

func TestCPUPercent(t *testing.T) {
	// sampel: total naik 1000, idle naik 800 → busy 200/1000 = 20%
	if p := cpuPercent(1000, 800, 2000, 1600); p != 20 {
		t.Fatalf("cpu%% = %v, mau 20", p)
	}
	if p := cpuPercent(1000, 800, 1000, 800); p != 0 { // tak ada delta
		t.Fatalf("delta nol harus 0, dapat %v", p)
	}
	// idle naik lebih cepat dari total (mustahil) → clamp 0
	if p := cpuPercent(1000, 800, 1100, 1000); p != 0 {
		t.Fatalf("clamp bawah gagal: %v", p)
	}
}
