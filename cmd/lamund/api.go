package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lamun-my-id/lamund/internal/api"
	"github.com/lamun-my-id/lamund/internal/auth"
	"github.com/lamun-my-id/lamund/internal/builder"
	"github.com/lamun-my-id/lamund/internal/email"
	"github.com/lamun-my-id/lamund/internal/runner"
	"github.com/lamun-my-id/lamund/internal/store"
	"github.com/lamun-my-id/lamund/internal/version"
	webpanel "github.com/lamun-my-id/lamund/web"
)

// cmdAPI menyajikan control-plane REST (/api/v1) di port admin terpisah
// (default loopback) — bukan port 80/443 lalu-lintas situs.
func cmdAPI(args []string) {
	fs := flag.NewFlagSet("api", flag.ExitOnError)
	dbPath := fs.String("db", "/var/lib/lamund/lamund.db", "path database SQLite")
	addr := fs.String("addr", "127.0.0.1:8080", "alamat listen API admin")
	secretFile := fs.String("secret-file", "/var/lib/lamund/secret.key", "file secret JWT (dibuat bila belum ada)")
	certDir := fs.String("acme-dir", "/var/lib/lamund/certs", "storage certmagic (untuk baca info sertifikat)")
	logDir := fs.String("log-dir", "/var/lib/lamund/logs", "direktori access log per-domain (untuk tail)")
	sitesDir := fs.String("sites-dir", "/var/lib/lamund/sites", "folder berkas situs per-tenant (upload/deploy)")
	reloadPidfile := fs.String("reload-pidfile", "/var/lib/lamund/lamund.pid", "pidfile data plane; disinyal SIGHUP saat situs berubah")
	ttl := fs.Duration("token-ttl", 24*time.Hour, "masa berlaku token akses")
	publicIP := fs.String("public-ip", "", "IP publik server (koma) untuk ditampilkan sbg glue nameserver DNS di panel")
	metricsAddr := fs.String("metrics-addr", "127.0.0.1:9091", "listener metrics Prometheus (loopback-only; kosong = nonaktif)")
	fs.Parse(args)

	githubClientID := os.Getenv("LAMUND_GITHUB_CLIENT_ID")

	st, err := store.Open(*dbPath)
	must(err)
	defer st.Close()

	secret, err := loadOrCreateSecret(*secretFile)
	must(err)

	sup := runner.NewSupervisor(filepath.Join(*logDir, "apps"))
	bld := builder.NewManager(filepath.Join(*logDir, "apps"))
	sites := store.NewSiteFS(*sitesDir)

	apiHandler := api.New(api.Deps{
		Store: st, Issuer: auth.NewIssuer(secret, *ttl),
		Sites:          sites,
		Runner:         sup,
		Builder:        bld,
		CertDir:        *certDir, LogDir: *logDir, DataDir: filepath.Dir(*dbPath),
		GitHubClientID: githubClientID,
		PublicIP:       *publicIP,
		OnSiteChange:   reloadSignaler(*reloadPidfile),
		Mailer:         email.NewManager(st.GetEmailSettings),
		BaseURL:        os.Getenv("LAMUND_BASE_URL"),
	})

	// Nyalakan app yang autostart. Data plane akan mem-proxy ke port-nya
	// (502 sementara sampai proses siap).
	startAutostartApps(st, sup)

	// Panel Vue di "/", REST di "/api/v1/". ServeMux tak memangkas prefix,
	// jadi sub-mux API tetap melihat path penuh.
	top := http.NewServeMux()
	top.Handle("/api/v1/", apiHandler)
	// Observability: /healthz readiness (ping DB), /metrics format Prometheus.
	top.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if err := st.Ping(); err != nil {
			http.Error(w, `{"status":"db unreachable"}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	top.Handle("/", webpanel.Handler())

	// /metrics disajikan di listener TERPISAH & loopback-only, BUKAN di mux panel.
	// Panel diproxy publik; bila /metrics ada di sana ia ikut terekspos. Listener
	// sendiri di 127.0.0.1 tak pernah diproxy → tak perlu access-control berbasis
	// header (yang rapuh). Prometheus scrape langsung ke alamat ini.
	apiStart := time.Now()
	if *metricsAddr != "" {
		mMux := http.NewServeMux()
		mMux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
			up := 1
			if st.Ping() != nil {
				up = 0
			}
			sites, users, apps, _ := st.Counts()
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			fmt.Fprintf(w, "# HELP lamund_up 1 jika control plane hidup & DB terjangkau\n# TYPE lamund_up gauge\nlamund_up %d\n", up)
			fmt.Fprintf(w, "# TYPE lamund_uptime_seconds gauge\nlamund_uptime_seconds %d\n", int(time.Since(apiStart).Seconds()))
			fmt.Fprintf(w, "# TYPE lamund_sites_total gauge\nlamund_sites_total %d\n", sites)
			fmt.Fprintf(w, "# TYPE lamund_users_total gauge\nlamund_users_total %d\n", users)
			fmt.Fprintf(w, "# TYPE lamund_apps_total gauge\nlamund_apps_total %d\n", apps)
			fmt.Fprintf(w, "# TYPE lamund_build_info gauge\nlamund_build_info{version=%q} 1\n", version.String())
		})
		mSrv := &http.Server{Addr: *metricsAddr, Handler: mMux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := mSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("WARN: metrics listener %s: %v", *metricsAddr, err)
			}
		}()
	}

	fmt.Printf("lamund panel+API di http://%s (db=%s)\n", *addr, *dbPath)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.PanelSecurityHeaders(top),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown: saat SIGTERM/SIGINT (mis. systemctl restart), matikan
	// semua proses app dulu agar tidak jadi yatim & port-nya bebas untuk boot
	// berikutnya.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		log.Printf("lamund api: shutdown — menghentikan semua app…")
		sup.StopAll()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		must(err)
	}
}

// startAutostartApps menyalakan semua app bertanda autostart saat boot.
func startAutostartApps(st *store.Store, sup *runner.Supervisor) {
	apps, err := st.ListApps()
	if err != nil {
		log.Printf("WARN: gagal memuat apps: %v", err)
		return
	}
	for _, a := range apps {
		if !a.Autostart {
			continue
		}
		sup.Start(runner.App{Name: a.Domain, Dir: a.WorkDir, Command: a.Command, Env: a.Env, Port: a.Port})
	}
}

// reloadSignaler mengembalikan fungsi yang mengirim SIGHUP ke proses data
// plane (dari pidfile) agar memuat ulang tabel routing. Aman bila pidfile
// tak ada / proses mati — perubahan tetap tersimpan di DB dan terpakai saat
// data plane berikutnya start.
func reloadSignaler(pidfile string) func() {
	return func() {
		raw, err := os.ReadFile(pidfile)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			return
		}
		if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
			log.Printf("WARN: gagal sinyal reload ke data plane (pid %d): %v", pid, err)
		}
	}
}

// loadOrCreateSecret membaca secret hex dari file, atau membuat 32 byte acak
// dan menyimpannya dengan izin 0600 bila belum ada.
func loadOrCreateSecret(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		secret, err := hex.DecodeString(strings.TrimSpace(string(b)))
		if err != nil || len(secret) < 16 {
			return nil, fmt.Errorf("secret di %s rusak", path)
		}
		return secret, nil
	}
	secret, err := auth.GenerateSecret()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(secret)), 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}
