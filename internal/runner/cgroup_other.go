//go:build !linux

package runner

import (
	"os"
	"os/exec"
)

// Stub non-Linux: cgroup tak tersedia → runner jalan tanpa limit. Memungkinkan
// build & test di macOS/Windows saat pengembangan.
type cgroupMgr struct{}

func newCgroupMgr() *cgroupMgr { return &cgroupMgr{} }

func (m *cgroupMgr) enabled() bool { return false }

func (m *cgroupMgr) create(name string, memMB, cpuPct, pids int) (*os.File, string, error) {
	return nil, "", nil
}

func (m *cgroupMgr) remove(path string) {}

func setCgroupFD(cmd *exec.Cmd, fd int) {}
