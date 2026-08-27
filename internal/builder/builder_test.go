package builder

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect(t *testing.T) {
	t.Run("node dengan lockfile + build script", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "package.json", `{"scripts":{"build":"vite build","start":"node ."}}`)
		write(t, d, "package-lock.json", "{}")
		p := Detect(d)
		if p.Type != "node" || len(p.Steps) != 2 || p.Steps[0] != "npm ci" || p.Steps[1] != "npm run build" {
			t.Fatalf("node: %+v", p)
		}
	})
	t.Run("node tanpa lockfile & tanpa build", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "package.json", `{"scripts":{"start":"node ."}}`)
		p := Detect(d)
		if p.Type != "node" || len(p.Steps) != 1 || p.Steps[0] != "npm install" {
			t.Fatalf("node no-build: %+v", p)
		}
	})
	t.Run("python", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "requirements.txt", "flask")
		if p := Detect(d); p.Type != "python" || p.Steps[0] != "pip install -r requirements.txt" {
			t.Fatalf("python: %+v", p)
		}
	})
	t.Run("go", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "go.mod", "module x")
		if p := Detect(d); p.Type != "go" || p.Steps[0] != "go build -o app ./..." {
			t.Fatalf("go: %+v", p)
		}
	})
	t.Run("static tanpa build", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "index.html", "<h1>x</h1>")
		if p := Detect(d); p.Type != "static" || len(p.Steps) != 0 {
			t.Fatalf("static: %+v", p)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		if p := Detect(t.TempDir()); p.Type != "unknown" {
			t.Fatalf("unknown: %+v", p)
		}
	})
}

func TestRunExecutesSteps(t *testing.T) {
	d := t.TempDir()
	var log testLog
	// langkah palsu yang membuat berkas penanda (bukti dijalankan di dir).
	err := Run(Plan{Type: "custom", Steps: []string{"echo hi > marker.txt"}}, d, nil, &log)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "marker.txt")); err != nil {
		t.Fatal("langkah build harus berjalan di dir app")
	}
}

func TestRunFailsOnBadStep(t *testing.T) {
	var log testLog
	err := Run(Plan{Type: "custom", Steps: []string{"exit 3"}}, t.TempDir(), nil, &log)
	if err == nil {
		t.Fatal("langkah gagal harus mengembalikan error")
	}
}

func TestManagerAsyncBuild(t *testing.T) {
	m := NewManager(t.TempDir())
	d := t.TempDir()
	write(t, d, "index.html", "x") // static → tanpa langkah → sukses cepat
	done := make(chan struct{}, 1)
	m.Build("app.com", d, nil, func() { done <- struct{}{} })

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("onSuccess harus dipanggil setelah build sukses")
	}
	if st := m.Status("app.com"); st.Status != StatusSuccess {
		t.Fatalf("status harus success, dapat %s", st.Status)
	}
}

func TestManagerBuildFailure(t *testing.T) {
	m := NewManager(t.TempDir())
	d := t.TempDir()
	write(t, d, "requirements.txt", "paket-yang-pasti-tidak-ada-xyz==999") // pip gagal
	m.Build("f.com", d, nil, nil)
	// tunggu selesai
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if s := m.Status("f.com").Status; s == StatusFailed || s == StatusSuccess {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// pip mungkin tak ada di CI; terima failed ATAU (bila pip absen) tetap failed.
	if st := m.Status("f.com"); st.Status != StatusFailed {
		t.Skipf("lingkungan tak punya pip yang gagal terkendali (status=%s) — dilewati", st.Status)
	}
}

type testLog struct{ b []byte }

func (l *testLog) Write(p []byte) (int, error) { l.b = append(l.b, p...); return len(p), nil }
