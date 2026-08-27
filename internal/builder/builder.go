// Package builder mendeteksi jenis proyek aplikasi dan menjalankan langkah
// build (pasang dependensi, build) sebelum app dijalankan. Langkah build
// bersifat KOMPUTASI dari jenis proyek (konstanta seperti "npm ci"), bukan
// input request — jadi aman dijalankan via shell. Runtime versi dikelola oleh
// mise bila tersedia (opsional); jika tidak, memakai runtime sistem.
package builder

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Plan adalah rencana build hasil deteksi.
type Plan struct {
	Type  string   // node|python|go|static|unknown
	Steps []string // perintah build berurutan (konstanta, bukan input user)
}

// Detect menebak jenis proyek dari berkas di dir dan menyusun langkah build.
func Detect(dir string) Plan {
	has := func(f string) bool { _, err := os.Stat(filepath.Join(dir, f)); return err == nil }
	switch {
	case has("package.json"):
		install := "npm install"
		if has("package-lock.json") {
			install = "npm ci"
		}
		steps := []string{install}
		if nodeHasBuildScript(dir) {
			steps = append(steps, "npm run build")
		}
		return Plan{Type: "node", Steps: steps}
	case has("requirements.txt"):
		return Plan{Type: "python", Steps: []string{"pip install -r requirements.txt"}}
	case has("pyproject.toml"):
		return Plan{Type: "python", Steps: []string{"pip install ."}}
	case has("go.mod"):
		return Plan{Type: "go", Steps: []string{"go build -o app ./..."}}
	case has("index.html"):
		return Plan{Type: "static"} // tanpa langkah build
	default:
		return Plan{Type: "unknown"}
	}
}

func nodeHasBuildScript(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	var pj struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(b, &pj) != nil {
		return false
	}
	_, ok := pj.Scripts["build"]
	return ok
}

// MiseAvailable benar bila version-manager mise ada di PATH.
func MiseAvailable() bool { _, err := exec.LookPath("mise"); return err == nil }

// Run menjalankan langkah build di dir, menulis output ke logw. Bila mise ada,
// tiap langkah dijalankan lewat "mise exec" (memakai versi runtime yang di-pin
// proyek). Mengembalikan error pada langkah pertama yang gagal.
func Run(plan Plan, dir string, env []string, logw io.Writer) error {
	if len(plan.Steps) == 0 {
		fmt.Fprintf(logw, "Jenis proyek: %s — tidak ada langkah build.\n", plan.Type)
		return nil
	}
	base := append(os.Environ(), env...)
	if plan.Type == "node" {
		base = append(base, "NODE_ENV=production", "CI=1")
	}
	useMise := MiseAvailable()
	fmt.Fprintf(logw, "Jenis proyek: %s (mise: %v)\n", plan.Type, useMise)
	if useMise {
		_ = runStep(dir, base, logw, "mise install") // best-effort: pin dari .tool-versions
	}
	for _, step := range plan.Steps {
		fmt.Fprintf(logw, "\n$ %s\n", step)
		cmd := step
		if useMise {
			cmd = "mise exec -- " + step
		}
		if err := runStep(dir, base, logw, cmd); err != nil {
			return fmt.Errorf("langkah build %q gagal: %w", step, err)
		}
	}
	fmt.Fprintln(logw, "\n✔ build selesai.")
	return nil
}

func runStep(dir string, env []string, logw io.Writer, cmd string) error {
	c := exec.Command("sh", "-c", cmd)
	c.Dir = dir
	c.Env = env
	c.Stdout = logw
	c.Stderr = logw
	return c.Run()
}
