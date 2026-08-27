package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newSup(t *testing.T) *Supervisor {
	t.Helper()
	s := NewSupervisor(t.TempDir())
	// percepat untuk uji
	s.restartDelay = 10 * time.Millisecond
	s.stable = 200 * time.Millisecond
	s.maxRestarts = 3
	return s
}

func TestStartRunningAndStop(t *testing.T) {
	s := newSup(t)
	if err := s.Start(App{Name: "a", Command: "sleep 30"}); err != nil {
		t.Fatal(err)
	}
	if !s.waitState("a", StateRunning, time.Second) {
		t.Fatalf("harus running, dapat %s", s.Status("a").State)
	}
	if s.Status("a").PID == 0 {
		t.Fatal("PID harus terisi saat running")
	}
	if err := s.Stop("a"); err != nil {
		t.Fatal(err)
	}
	if !s.waitState("a", StateStopped, 3*time.Second) {
		t.Fatalf("harus stopped setelah Stop, dapat %s", s.Status("a").State)
	}
}

func TestCapturesLog(t *testing.T) {
	s := newSup(t)
	s.Start(App{Name: "logger", Command: "echo HALO-STDOUT; echo HALO-STDERR 1>&2; sleep 30"})
	s.waitState("logger", StateRunning, time.Second)
	time.Sleep(150 * time.Millisecond) // beri waktu tulis log
	b, err := os.ReadFile(filepath.Join(s.logDir, "app-logger.log"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if !contains(out, "HALO-STDOUT") || !contains(out, "HALO-STDERR") {
		t.Fatalf("log harus memuat stdout & stderr, dapat: %q", out)
	}
	s.Stop("logger")
}

func TestInjectsPortEnv(t *testing.T) {
	s := newSup(t)
	s.Start(App{Name: "port", Port: 45678, Command: `echo "port=$PORT"; sleep 30`})
	s.waitState("port", StateRunning, time.Second)
	time.Sleep(150 * time.Millisecond)
	b, _ := os.ReadFile(filepath.Join(s.logDir, "app-port.log"))
	if !contains(string(b), "port=45678") {
		t.Fatalf("PORT harus disuntikkan, log: %q", string(b))
	}
	s.Stop("port")
}

func TestCrashRestartsThenFails(t *testing.T) {
	s := newSup(t) // maxRestarts=3
	s.Start(App{Name: "boom", Command: "exit 1"})
	// crash langsung & terus, harus menyerah → failed
	if !s.waitState("boom", StateFailed, 5*time.Second) {
		t.Fatalf("harus failed setelah restart beruntun, dapat %s (restarts=%d)",
			s.Status("boom").State, s.Status("boom").Restarts)
	}
	if s.Status("boom").Restarts <= s.maxRestarts {
		t.Fatalf("restarts harus melewati maxRestarts, dapat %d", s.Status("boom").Restarts)
	}
}

func TestRestartRerunsCommand(t *testing.T) {
	s := newSup(t)
	s.Start(App{Name: "r", Command: "sleep 30"})
	s.waitState("r", StateRunning, time.Second)
	pid1 := s.Status("r").PID
	if err := s.Restart(App{Name: "r", Command: "sleep 30"}); err != nil {
		t.Fatal(err)
	}
	if !s.waitState("r", StateRunning, 3*time.Second) {
		t.Fatal("harus running lagi setelah restart")
	}
	if s.Status("r").PID == pid1 {
		t.Fatal("restart harus menghasilkan proses (PID) baru")
	}
	s.Stop("r")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestCommandMiseWrapping(t *testing.T) {
	s := NewSupervisor(t.TempDir())
	dir := t.TempDir()

	// mise tak ada → sh -c biasa
	s.miseOK = func() bool { return false }
	if got := s.command(App{Command: "node .", Dir: dir}).Args; got[0] != "sh" {
		t.Fatalf("tanpa mise harus sh, dapat %v", got)
	}
	// mise ada tapi tanpa .tool-versions → tetap sh -c (mise tak dipaksa)
	s.miseOK = func() bool { return true }
	if got := s.command(App{Command: "node .", Dir: dir}).Args; got[0] != "sh" {
		t.Fatalf("tanpa tool config harus sh, dapat %v", got)
	}
	// mise ada + .tool-versions → mise exec
	os.WriteFile(filepath.Join(dir, ".tool-versions"), []byte("nodejs 20.11.0\n"), 0o644)
	got := s.command(App{Command: "node .", Dir: dir}).Args
	if got[0] != "mise" || got[1] != "exec" {
		t.Fatalf("dengan mise+config harus 'mise exec', dapat %v", got)
	}
}

func TestRenameAndDelete(t *testing.T) {
	s := newSup(t)
	s.Start(App{Name: "a", Command: "sleep 30"})
	s.waitState("a", StateRunning, time.Second)
	s.Rename("a", "a#old")
	if s.Status("a").State != "" {
		t.Fatal("nama lama harus kosong setelah rename")
	}
	if s.Status("a#old").State != StateRunning {
		t.Fatal("nama baru harus running")
	}
	s.Stop("a#old")
	s.waitState("a#old", StateStopped, 3*time.Second)
	s.Delete("a#old")
	if s.Status("a#old").State != "" {
		t.Fatal("setelah Delete harus hilang dari peta")
	}
}
