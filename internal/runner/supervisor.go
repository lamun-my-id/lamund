// Package runner mengawasi proses aplikasi tepercaya (Node/Next-SSR/Go/Python):
// menjalankan perintah di direktori kerjanya dengan PORT ter-assign, menangkap
// log, dan me-restart otomatis saat crash (dengan batas). Model "jalur A":
// proses terkelola tanpa container — cocok karena Lamund self-hosted dan hanya
// menjalankan kode yang dipercaya operatornya.
package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// State proses aplikasi.
const (
	StateStopped = "stopped"
	StateRunning = "running"
	StateCrashed = "crashed"
	StateFailed  = "failed" // menyerah setelah terlalu banyak crash beruntun
)

// App mendeskripsikan aplikasi yang dijalankan.
type App struct {
	Name    string   // pengenal unik instance (mis. domain, atau domain#old saat swap)
	LogKey  string   // kunci berkas log (stabil lintas swap; kosong = pakai Name)
	Dir     string   // direktori kerja
	Command string   // perintah shell, mis. "npm start" / "node server.js"
	Env     []string // env tambahan "K=V"
	Port    int      // port yang di-assign, disuntikkan sebagai PORT

	// Batas resource per app (cgroups v2). 0 = tanpa batas untuk dimensi itu.
	MemoryMB   int // RAM cap
	CPUPercent int // CPU cap, % dari 1 core
	Pids       int // batas proses/thread
}

func (a App) logKey() string {
	if a.LogKey != "" {
		return a.LogKey
	}
	return a.Name
}

// Status ringkasan kondisi proses.
type Status struct {
	Name      string
	State     string
	PID       int
	Restarts  int
	StartedAt time.Time
}

type process struct {
	app       App
	cmd       *exec.Cmd
	state     string
	restarts  int
	startedAt time.Time
	stopping  bool
	logw      io.Writer
	cgPath    string // path cgroup app (kosong = tanpa cgroup)
}

// Supervisor mengelola sekumpulan proses aplikasi.
type Supervisor struct {
	mu     sync.Mutex
	procs  map[string]*process
	logDir string
	cg     *cgroupMgr // enforcement resource per app (nil-safe via enabled())

	// parameter restart (dapat diubah untuk uji).
	restartDelay time.Duration
	stable       time.Duration // uptime > ini → reset hitungan restart
	maxRestarts  int           // crash beruntun melebihi ini → failed
	now          func() time.Time
	openLog      func(name string) (io.Writer, error)
	miseOK       func() bool // dapat di-override untuk uji
}

// NewSupervisor membuat supervisor yang menulis log app ke logDir.
func NewSupervisor(logDir string) *Supervisor {
	s := &Supervisor{
		procs:        map[string]*process{},
		logDir:       logDir,
		restartDelay: time.Second,
		stable:       10 * time.Second,
		maxRestarts:  8,
		now:          time.Now,
	}
	s.openLog = s.defaultOpenLog
	s.miseOK = miseAvailable
	s.cg = newCgroupMgr() // deteksi cgroup v2 + siapkan subtree (best-effort)
	return s
}

// command menyusun perintah menjalankan app. Bila mise tersedia DAN folder app
// punya konfigurasi versi (.tool-versions / mise.toml), app dijalankan lewat
// "mise exec" agar memakai versi runtime yang di-pin — sama seperti saat build.
func (s *Supervisor) command(app App) *exec.Cmd {
	if s.miseOK() && hasToolConfig(app.Dir) {
		// mise install (best-effort, dibatasi waktu) memastikan versi terpasang.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		inst := exec.CommandContext(ctx, "mise", "install")
		inst.Dir = app.Dir
		_ = inst.Run()
		return exec.Command("mise", "exec", "--", "sh", "-c", app.Command)
	}
	return exec.Command("sh", "-c", app.Command)
}

func miseAvailable() bool { _, err := exec.LookPath("mise"); return err == nil }

func hasToolConfig(dir string) bool {
	for _, f := range []string{".tool-versions", "mise.toml", ".mise.toml"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

func (s *Supervisor) defaultOpenLog(name string) (io.Writer, error) {
	if err := os.MkdirAll(s.logDir, 0o750); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(s.logDir, "app-"+safe(name)+".log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
}

// Start menjalankan app (idempoten bila sudah running).
func (s *Supervisor) Start(app App) error {
	s.mu.Lock()
	if p, ok := s.procs[app.Name]; ok && p.state == StateRunning {
		s.mu.Unlock()
		return nil
	}
	w, err := s.openLog(app.logKey())
	if err != nil {
		s.mu.Unlock()
		return err
	}
	p := &process{app: app, logw: w, state: StateStopped}
	s.procs[app.Name] = p
	s.mu.Unlock()
	return s.spawn(p)
}

func (s *Supervisor) spawn(p *process) error {
	// CATATAN KEAMANAN: Command dijalankan lewat "sh -c" SECARA SENGAJA —
	// ini start-command yang ditetapkan operator (seperti Procfile / systemd
	// ExecStart), bukan input dari request HTTP. Batas kepercayaan ditegakkan
	// di lapisan API: hanya user berwenang (admin/pemilik app) yang boleh
	// menyetel Command. Jangan pernah mengisi Command dari data request mentah.
	cmd := s.command(p.app)
	cmd.Dir = p.app.Dir
	cmd.Env = append(os.Environ(), p.app.Env...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("PORT=%d", p.app.Port))
	cmd.Stdout = p.logw
	cmd.Stderr = p.logw
	// Grup proses sendiri agar seluruh pohon (npm→node) bisa dimatikan sekaligus.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Batasi resource via cgroups v2 (bila tersedia & ada limit). Child lahir
	// langsung di cgroup (CLONE_INTO_CGROUP) → seluruh pohon proses terbatasi.
	a := p.app
	if s.cg.enabled() && (a.MemoryMB > 0 || a.CPUPercent > 0 || a.Pids > 0) {
		if f, path, err := s.cg.create(a.Name, a.MemoryMB, a.CPUPercent, a.Pids); err == nil {
			setCgroupFD(cmd, int(f.Fd()))
			p.cgPath = path
			defer f.Close()
		} else {
			fmt.Fprintf(p.logw, "[lamund] gagal siapkan cgroup limit: %v (jalan tanpa limit)\n", err)
		}
	}

	if err := cmd.Start(); err != nil {
		s.mu.Lock()
		p.state = StateCrashed
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	p.cmd = cmd
	p.state = StateRunning
	p.startedAt = s.now()
	p.stopping = false
	s.mu.Unlock()

	go s.wait(p)
	return nil
}

func (s *Supervisor) wait(p *process) {
	_ = p.cmd.Wait()

	s.mu.Lock()
	if p.stopping {
		p.state = StateStopped
		cgPath := p.cgPath
		s.mu.Unlock()
		s.cg.remove(cgPath) // proses berhenti final → bersihkan cgroup
		return
	}
	if s.now().Sub(p.startedAt) > s.stable {
		p.restarts = 0 // sudah stabil cukup lama; anggap sehat, reset
	}
	p.restarts++
	p.state = StateCrashed
	if p.restarts > s.maxRestarts {
		p.state = StateFailed
		cgPath := p.cgPath
		s.mu.Unlock()
		s.cg.remove(cgPath) // menyerah restart → bersihkan cgroup
		return
	}
	delay := s.restartDelay
	s.mu.Unlock()

	time.Sleep(delay)

	s.mu.Lock()
	stopping := p.stopping
	s.mu.Unlock()
	if stopping {
		return
	}
	_ = s.spawn(p)
}

// Stop menghentikan app (mengirim SIGTERM ke seluruh grup proses).
func (s *Supervisor) Stop(name string) error {
	s.mu.Lock()
	p, ok := s.procs[name]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	p.stopping = true
	cmd := p.cmd
	s.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		// negatif PID = seluruh grup proses.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	return nil
}

// Restart menghentikan lalu menjalankan ulang app.
func (s *Supervisor) Restart(app App) error {
	_ = s.Stop(app.Name)
	// beri waktu proses lama benar-benar mati sebelum bind port lagi.
	s.waitState(app.Name, StateStopped, 5*time.Second)
	s.mu.Lock()
	delete(s.procs, app.Name)
	s.mu.Unlock()
	return s.Start(app)
}

// Rename memindahkan instance dari kunci from ke to (dipakai saat blue-green
// swap: instance lama di-rename agar instance baru bisa memakai nama kanonik).
func (s *Supervisor) Rename(from, to string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.procs[from]
	if !ok || from == to {
		return
	}
	delete(s.procs, from)
	p.app.Name = to
	s.procs[to] = p
}

// Delete menghapus entri instance dari peta (setelah Stop). Aman bila tak ada.
func (s *Supervisor) Delete(name string) {
	s.mu.Lock()
	delete(s.procs, name)
	s.mu.Unlock()
}

// Status mengembalikan kondisi app (State kosong bila tak dikenal).
func (s *Supervisor) Status(name string) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.procs[name]
	if !ok {
		return Status{Name: name}
	}
	st := Status{Name: name, State: p.state, Restarts: p.restarts, StartedAt: p.startedAt}
	if p.cmd != nil && p.cmd.Process != nil && p.state == StateRunning {
		st.PID = p.cmd.Process.Pid
	}
	return st
}

// StopAll menghentikan semua app (dipakai saat shutdown).
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	names := make([]string, 0, len(s.procs))
	for n := range s.procs {
		names = append(names, n)
	}
	s.mu.Unlock()
	for _, n := range names {
		_ = s.Stop(n)
	}
}

// waitState menunggu app mencapai state tertentu hingga timeout (untuk internal & uji).
func (s *Supervisor) waitState(name, want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.Status(name).State == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s.Status(name).State == want
}

// LogPath mengembalikan path berkas log untuk app.
func (s *Supervisor) LogPath(name string) string {
	return filepath.Join(s.logDir, "app-"+safe(name)+".log")
}

// Logs mengembalikan hingga n baris terakhir log app (kosong bila belum ada).
func (s *Supervisor) Logs(name string, n int) ([]string, error) {
	f, err := os.Open(s.LogPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	defer f.Close()
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return ring, sc.Err()
}

func safe(name string) string {
	out := make([]byte, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.', r == '_':
			out = append(out, byte(r))
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "app"
	}
	return string(out)
}
