package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lamun-my-id/lamund/internal/acme"
	"github.com/lamun-my-id/lamund/internal/dns"
	"github.com/lamun-my-id/lamund/internal/logging"
	"github.com/lamun-my-id/lamund/internal/server"
	"github.com/lamun-my-id/lamund/internal/store"
)

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dbPath := fs.String("db", "/var/lib/lamund/lamund.db", "path database SQLite")
	httpAddr := fs.String("http", ":80", "alamat listener HTTP")
	httpsAddr := fs.String("https", "", "alamat listener HTTPS (kosong = HTTP saja, tanpa SSL)")
	acmeCA := fs.String("acme-ca", "staging", "CA ACME: staging|production|<url>")
	acmeEmail := fs.String("acme-email", "", "email akun ACME")
	acmeDir := fs.String("acme-dir", "/var/lib/lamund/certs", "penyimpanan sertifikat")
	publicIP := fs.String("public-ip", "", "IP publik server (koma) untuk cek DNS sebelum terbit cert")
	pidPath := fs.String("pidfile", "/run/lamund.pid", "path pidfile (untuk lamund reload)")
	logDir := fs.String("log-dir", "/var/lib/lamund/logs", "direktori access log per-domain")
	dnsAddr := fs.String("dns", "", "alamat bind DNS authoritative (mis. 1.2.3.4:53); kosong = mati")
	fs.Parse(args)

	st, err := store.Open(*dbPath)
	must(err)
	defer st.Close()

	rl, err := server.NewReloadable(st)
	must(err)

	// DNS authoritative (opsional, aktif hanya bila flag --dns diberikan).
	var dnsZones *dns.Zones
	if *dnsAddr != "" {
		z, zerr := dns.NewReloadableZones(st)
		if zerr != nil {
			log.Printf("WARN: DNS zone build gagal (server tetap jalan): %v", zerr)
			// Buat Zones kosong agar Serve tetap bisa berjalan.
			z = dns.NewZones()
		}
		dnsZones = z
		stop, serr := dns.Serve(*dnsAddr, dnsZones)
		if serr != nil {
			log.Printf("WARN: DNS listener gagal bind %s: %v", *dnsAddr, serr)
			dnsZones = nil // jangan reload zona bila server gagal start
		} else {
			log.Printf("lamund: DNS authoritative di %s", *dnsAddr)
			defer stop()
		}
	}

	// Access log per-domain (rotasi 20 MB). Bila gagal dibuka, jalan tanpa log.
	logged := func(h http.Handler) http.Handler { return h }
	if lm, err := logging.NewManager(*logDir, 20<<20, nil, nil); err != nil {
		log.Printf("WARN: access log nonaktif (%s): %v", *logDir, err)
	} else {
		defer lm.Close()
		logged = lm.Handler
	}

	if err := os.WriteFile(*pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		log.Printf("WARN: gagal tulis pidfile %s: %v", *pidPath, err)
	} else {
		defer os.Remove(*pidPath)
	}

	// mode HTTP saja (tanpa SSL) — sederhana, satu listener
	if *httpsAddr == "" {
		log.Printf("lamund: %d site aktif, listen %s (HTTP saja)", rl.Len(), *httpAddr)
		srv := &http.Server{Addr: *httpAddr, Handler: logged(rl.Handler()), ReadHeaderTimeout: 10 * time.Second}
		installReload(rl, nil, nil, dnsZones, st)
		runShutdown(srv)
		serveOne(srv)
		return
	}

	// mode HTTPS — ACME otomatis lewat certmagic
	acmeCfg := acme.Config{Email: *acmeEmail, StorageDir: *acmeDir, CA: *acmeCA}
	if dnsZones != nil {
		acmeCfg.DNSProvider = &dns.ACMEProvider{Store: st, Reload: func() { dnsZones.Reload(st) }}
	}
	mgr := acme.NewManager(acmeCfg)
	ips := parseIPs(*publicIP)
	log.Printf("lamund: %d site aktif, HTTP %s + HTTPS %s (ACME ca=%s)", rl.Len(), *httpAddr, *httpsAddr, *acmeCA)

	// :80 = responder challenge ACME → host dikenal redirect ke https; host tak
	// dikenal dapat 404 sopan (jangan redirect ke HTTPS yang lalu gagal di TLS).
	httpFallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rl.HasHost(r.Host) {
			redirectHTTPS(w, r)
			return
		}
		server.WriteNotFound(w)
	})
	httpSrv := &http.Server{
		Addr:              *httpAddr,
		Handler:           mgr.HTTPChallengeHandler(httpFallback),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// :443 = TLS dari cert manager → vhost handler
	httpsSrv := &http.Server{
		Addr:              *httpsAddr,
		Handler:           logged(rl.Handler()),
		TLSConfig:         mgr.TLSConfig(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	installReload(rl, mgr, ips, dnsZones, st)
	runShutdown(httpSrv, httpsSrv)

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listener :80: %v", err)
		}
	}()
	// setelah :80 siap melayani challenge, minta/pastikan cert domain aktif
	go syncACME(context.Background(), rl, mgr, ips, st, dnsZones != nil)

	if err := httpsSrv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listener :443: %v", err)
	}
}

// redirectHTTPS mengarahkan trafik :80 (yang bukan challenge) ke https.
func redirectHTTPS(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusMovedPermanently)
}

// syncACME meminta/mengelola sertifikat untuk domain aktif yang DNS-nya sudah
// mengarah ke server (bila public-ip diberikan) — cegah rate-limit LE.
// Domain zona terkelola (dnsOn=true) diperluas menjadi wildcard+apex via DNS-01;
// wildcard (awalan "*.") dilewati pre-check A-record karena tak bisa di-LookupIP.
func syncACME(ctx context.Context, rl *server.Reloadable, mgr *acme.Manager, ips []net.IP, st *store.Store, dnsOn bool) {
	domains := certDomains(st, rl.ActiveDomains(), dnsOn)
	var ready []string
	for _, d := range domains {
		// Wildcard tidak bisa dicek via A-record; DNS-01 yang akan memvalidasi.
		if strings.HasPrefix(d, "*.") {
			ready = append(ready, d)
			continue
		}
		// Domain di zona terkelola (dnsOn) bisa diterbitkan via DNS-01 walau tak
		// punya A record — jangan gugurkan lewat pre-check A. (Apex zona dsb.)
		if dnsOn {
			if _, _, ok := st.FindDNSZoneForDomain(d); ok {
				ready = append(ready, d)
				continue
			}
		}
		if len(ips) == 0 {
			ready = append(ready, d)
			continue
		}
		ok, err := acme.DomainPointsHere(ctx, net.DefaultResolver, d, ips)
		if err != nil {
			log.Printf("acme: %s dilewati (DNS: %v)", d, err)
			continue
		}
		if !ok {
			log.Printf("acme: %s dilewati (DNS belum mengarah ke server)", d)
			continue
		}
		ready = append(ready, d)
	}
	if len(ready) == 0 {
		return
	}
	if err := mgr.Manage(ctx, ready); err != nil {
		log.Printf("acme: sebagian cert gagal: %v", err)
	} else {
		log.Printf("acme: %d domain dikelola & bersertifikat", len(ready))
	}
}

// certDomains memperluas daftar domain aktif untuk keperluan ACME:
// - bila dnsOn dan domain termasuk zona terkelola Lamund → ganti dengan "*.apex" dan "apex" (dedup)
// - selain itu → tambahkan domain apa adanya.
// Output unik, urutan stabil (urutan iterasi dengan dedup map).
func certDomains(st *store.Store, active []string, dnsOn bool) []string {
	seen := make(map[string]bool)
	var out []string
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, d := range active {
		if dnsOn {
			if z, label, ok := st.FindDNSZoneForDomain(d); ok {
				// Wildcard single-label + apex menutup apex & subdomain 1-level.
				add("*." + z.Domain)
				add(z.Domain)
				// Subdomain berlapis (mis. a.b.zona) TIDAK ditutup wildcard
				// single-label — pertahankan host eksaknya (terbit via DNS-01).
				if label != "@" && strings.Contains(label, ".") {
					add(d)
				}
				continue
			}
		}
		add(d)
	}
	return out
}

// installReload memasang handler SIGHUP: reload konfigurasi + (jika ACME) sync cert
// + (jika DNS aktif) reload zona DNS.
func installReload(rl *server.Reloadable, mgr *acme.Manager, ips []net.IP, zones *dns.Zones, st *store.Store) {
	go func() {
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		for range hup {
			if err := rl.Reload(); err != nil {
				log.Printf("lamund: reload GAGAL (pakai konfigurasi lama): %v", err)
				continue
			}
			log.Printf("lamund: reload OK — %d site aktif", rl.Len())
			if mgr != nil {
				go syncACME(context.Background(), rl, mgr, ips, st, zones != nil)
			}
			if zones != nil {
				if err := zones.Reload(st); err != nil {
					log.Printf("lamund: DNS reload GAGAL (zona lama dipertahankan): %v", err)
				} else {
					log.Printf("lamund: DNS reload OK")
				}
			}
		}
	}()
}

func runShutdown(srvs ...*http.Server) {
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("lamund: shutdown ...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, s := range srvs {
			s.Shutdown(ctx)
		}
	}()
}

func serveOne(srv *http.Server) {
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func parseIPs(s string) []net.IP {
	var out []net.IP
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if ip := net.ParseIP(part); ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

// cmdReload mengirim SIGHUP ke daemon lamund yang sedang jalan (baca pidfile).
func cmdReload(args []string) {
	fs := flag.NewFlagSet("reload", flag.ExitOnError)
	pidPath := fs.String("pidfile", "/run/lamund.pid", "path pidfile daemon")
	fs.Parse(args)

	raw, err := os.ReadFile(*pidPath)
	if err != nil {
		fatal("tidak bisa baca pidfile %s (daemon jalan?): %v", *pidPath, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		fatal("pidfile %s rusak: %v", *pidPath, err)
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		fatal("proses %d tidak ditemukan: %v", pid, err)
	}
	must(p.Signal(syscall.SIGHUP))
	fmt.Printf("✔ sinyal reload dikirim ke lamund (pid %d)\n", pid)
}
