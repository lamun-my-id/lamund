//go:build linux

package runner

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const cgroupRoot = "/sys/fs/cgroup"

// cgroupMgr mengelola sub-cgroup per app di bawah cgroup terdelegasi milik
// service (systemd Delegate=yes). ok=false → cgroup tak tersedia → jalan tanpa
// limit (fallback aman).
type cgroupMgr struct {
	base string // cgroup service (mis. /sys/fs/cgroup/system.slice/lamund-panel.service)
	ok   bool
}

func (m *cgroupMgr) enabled() bool { return m != nil && m.ok }

// selfCgroupRel membaca path cgroup v2 proses ini dari /proc/self/cgroup
// (baris "0::/...."). Return path relatif thd cgroupRoot.
func selfCgroupRel() (string, error) {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.HasPrefix(line, "0::") {
			return strings.TrimPrefix(line, "0::"), nil
		}
	}
	return "", fmt.Errorf("cgroup v2 tak ditemukan di /proc/self/cgroup")
}

// newCgroupMgr menyiapkan pengelola cgroup: pindahkan proses ini ke leaf
// 'supervisor' (agar base jadi inner node), lalu aktifkan controller memory/cpu/
// pids untuk anak-anak base. Gagal di langkah mana pun → ok=false (fallback).
func newCgroupMgr() *cgroupMgr {
	if _, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers")); err != nil {
		log.Printf("cgroup: bukan cgroup v2 (%v) → tanpa limit", err)
		return &cgroupMgr{} // bukan cgroup v2
	}
	rel, err := selfCgroupRel()
	if err != nil {
		log.Printf("cgroup: baca /proc/self/cgroup gagal: %v → tanpa limit", err)
		return &cgroupMgr{}
	}
	base := filepath.Join(cgroupRoot, rel)
	if _, err := os.Stat(base); err != nil {
		log.Printf("cgroup: base %s tak ada: %v → tanpa limit", base, err)
		return &cgroupMgr{}
	}
	// Pindahkan proses ini ke leaf agar base bebas-proses (syarat "no internal
	// processes" cgroup v2 sebelum mendistribusikan controller ke anak).
	sup := filepath.Join(base, "supervisor")
	if err := os.Mkdir(sup, 0o755); err != nil && !os.IsExist(err) {
		log.Printf("cgroup: mkdir supervisor gagal: %v → tanpa limit (butuh Delegate=yes)", err)
		return &cgroupMgr{}
	}
	if err := os.WriteFile(filepath.Join(sup, "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		log.Printf("cgroup: pindah proses ke supervisor gagal: %v → tanpa limit", err)
		return &cgroupMgr{}
	}
	// Aktifkan controller yang tersedia (delegated) untuk anak-anak base.
	ctrls, _ := os.ReadFile(filepath.Join(base, "cgroup.controllers"))
	avail := " " + strings.TrimSpace(string(ctrls)) + " "
	var enable []string
	for _, c := range []string{"memory", "cpu", "pids"} {
		if strings.Contains(avail, " "+c+" ") {
			enable = append(enable, "+"+c)
		}
	}
	if len(enable) == 0 {
		log.Printf("cgroup: controller memory/cpu/pids tak terdelegasi (avail=%q) → tanpa limit", strings.TrimSpace(string(ctrls)))
		return &cgroupMgr{}
	}
	if err := os.WriteFile(filepath.Join(base, "cgroup.subtree_control"), []byte(strings.Join(enable, " ")), 0o644); err != nil {
		log.Printf("cgroup: enable subtree_control %v gagal: %v → tanpa limit", enable, err)
		return &cgroupMgr{}
	}
	log.Printf("cgroup: siap — base=%s controllers=%v (limit resource per-app aktif)", base, enable)
	return &cgroupMgr{base: base, ok: true}
}

// create membuat/menyetel sub-cgroup app dan mengembalikan fd direktori-nya
// (untuk CLONE_INTO_CGROUP via UseCgroupFD) + path. Caller wajib menutup fd
// setelah cmd.Start.
func (m *cgroupMgr) create(name string, memMB, cpuPct, pids int) (*os.File, string, error) {
	dir := filepath.Join(m.base, "app-"+safe(name))
	if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
		return nil, "", err
	}
	if memMB > 0 {
		_ = os.WriteFile(filepath.Join(dir, "memory.max"), []byte(strconv.Itoa(memMB*1024*1024)), 0o644)
		// swap.max=0 → app yg bocor memori langsung OOM (fail-fast), tak seret VM.
		_ = os.WriteFile(filepath.Join(dir, "memory.swap.max"), []byte("0"), 0o644)
	}
	if cpuPct > 0 {
		// cpu.max: "<quota_us> <period_us>". period 100ms; quota = cpuPct% dari 1 core.
		_ = os.WriteFile(filepath.Join(dir, "cpu.max"), []byte(fmt.Sprintf("%d 100000", cpuPct*1000)), 0o644)
	}
	if pids > 0 {
		_ = os.WriteFile(filepath.Join(dir, "pids.max"), []byte(strconv.Itoa(pids)), 0o644)
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, "", err
	}
	return f, dir, nil
}

// remove menghapus cgroup app (best-effort; gagal bila masih ada proses).
func (m *cgroupMgr) remove(path string) {
	if path != "" {
		_ = os.Remove(path)
	}
}

// setCgroupFD menyuruh child lahir langsung di cgroup (clone3 CLONE_INTO_CGROUP)
// — race-free, tak perlu pindah PID setelah spawn.
func setCgroupFD(cmd *exec.Cmd, fd int) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.UseCgroupFD = true
	cmd.SysProcAttr.CgroupFD = fd
}
