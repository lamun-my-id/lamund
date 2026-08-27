package builder

import (
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Build status.
const (
	StatusIdle     = "idle"
	StatusBuilding = "building"
	StatusSuccess  = "success"
	StatusFailed   = "failed"
)

// State ringkasan status build terakhir sebuah app.
type State struct {
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	Type       string    `json:"type"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// Manager menjalankan build secara asinkron (npm ci bisa lama) dan melacak
// status per app, menulis log build ke berkas.
type Manager struct {
	mu     sync.Mutex
	states map[string]*State
	logDir string
	now    func() time.Time
}

func NewManager(logDir string) *Manager {
	return &Manager{states: map[string]*State{}, logDir: logDir, now: time.Now}
}

func (m *Manager) logPath(name string) string {
	return filepath.Join(m.logDir, "build-"+safe(name)+".log")
}

// Build memulai build asinkron untuk app di dir. onSuccess dipanggil setelah
// build sukses (mis. restart app). Idempoten: diabaikan bila sedang building.
func (m *Manager) Build(name, dir string, env []string, onSuccess func()) {
	m.Deploy(name, dir, nil, env, onSuccess)
}

// Deploy seperti Build tapi menjalankan pre (mis. git fetch) lebih dulu; bila
// pre gagal, build tidak dijalankan dan status = failed.
func (m *Manager) Deploy(name, dir string, pre func(io.Writer) error, env []string, onSuccess func()) {
	m.mu.Lock()
	if s := m.states[name]; s != nil && s.Status == StatusBuilding {
		m.mu.Unlock()
		return
	}
	m.states[name] = &State{Status: StatusBuilding, StartedAt: m.now()}
	m.mu.Unlock()

	go m.run(name, dir, pre, env, onSuccess)
}

func (m *Manager) run(name, dir string, pre func(io.Writer) error, env []string, onSuccess func()) {
	_ = os.MkdirAll(m.logDir, 0o750)
	logw, err := os.OpenFile(m.logPath(name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	var plan Plan
	var buildErr error
	switch {
	case err != nil:
		buildErr = err
	default:
		if pre != nil {
			buildErr = pre(logw)
		}
		if buildErr == nil {
			plan = Detect(dir)
			buildErr = Run(plan, dir, env, logw)
		}
		logw.Close()
	}

	m.mu.Lock()
	st := m.states[name]
	st.FinishedAt = m.now()
	st.Type = plan.Type
	if buildErr != nil {
		st.Status = StatusFailed
		st.Message = buildErr.Error()
	} else {
		st.Status = StatusSuccess
		st.Message = "build " + plan.Type + " sukses"
	}
	m.mu.Unlock()

	if buildErr == nil && onSuccess != nil {
		onSuccess()
	}
}

// Status mengembalikan status build app (idle bila belum pernah).
func (m *Manager) Status(name string) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.states[name]; ok {
		return *s
	}
	return State{Status: StatusIdle}
}

// Logs membaca hingga n baris terakhir log build.
func (m *Manager) Logs(name string, n int) ([]string, error) {
	return tail(m.logPath(name), n)
}
