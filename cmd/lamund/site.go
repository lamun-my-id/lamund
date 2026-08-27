package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/lamun-my-id/lamund/internal/store"
)

// siteAdd memvalidasi lalu menyimpan site static baru.
func siteAdd(dbPath, domain, root string) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("root %q tidak ada atau bukan direktori", root)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	_, err = st.CreateSite(store.Site{Domain: domain, Type: "static", RootPath: root})
	return err
}

func cmdSite(args []string) {
	if len(args) < 1 {
		fatal("usage: lamund site <add|list|rm> ...")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("site "+sub, flag.ExitOnError)
	dbPath := fs.String("db", "/var/lib/lamund/lamund.db", "path database SQLite")
	domain := fs.String("domain", "", "nama domain")
	root := fs.String("root", "", "direktori file situs (tipe static)")
	siteType := fs.String("type", "static", "tipe site: static|proxy")
	target := fs.String("target", "", "target upstream (tipe proxy), mis. 127.0.0.1:3000")
	external := fs.Bool("allow-external", false, "izinkan proxy target non-loopback")
	disable := fs.Bool("disable", false, "nonaktifkan site (edit)")
	enable := fs.Bool("enable", false, "aktifkan site (edit)")
	fs.Parse(rest)

	switch sub {
	case "add":
		switch *siteType {
		case "static":
			must(siteAdd(*dbPath, *domain, *root))
		case "proxy":
			must(siteAddProxy(*dbPath, *domain, *target, *external))
		case "redirect":
			must(siteAddRedirect(*dbPath, *domain, *target))
		default:
			fatal("tipe %q tidak dikenal (static|proxy|redirect)", *siteType)
		}
		fmt.Printf("✔ site %s ditambahkan\n", *domain)
	case "edit":
		must(siteEdit(*dbPath, *domain, editOpts{
			target: *target, root: *root, disable: *disable, enable: *enable, allowExternal: *external,
		}))
		fmt.Printf("✔ site %s diperbarui\n", *domain)
	case "list":
		st, err := store.Open(*dbPath)
		must(err)
		defer st.Close()
		sites, err := st.ListSites()
		must(err)
		for _, s := range sites {
			target := s.RootPath
			if s.Type == "proxy" {
				target = s.ProxyTarget
			}
			fmt.Printf("%-30s %-7s %-10s %s\n", s.Domain, s.Type, s.Status, target)
		}
	case "rm":
		st, err := store.Open(*dbPath)
		must(err)
		defer st.Close()
		must(st.DeleteSite(*domain))
		fmt.Printf("✔ site %s dihapus\n", *domain)
	default:
		fatal("subperintah site tidak dikenal: %s", sub)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "lamund: "+format+"\n", a...)
	os.Exit(1)
}

func must(err error) {
	if err != nil {
		fatal("%v", err)
	}
}

// siteAddProxy menambah site proxy dengan target tervalidasi (anti-SSRF).
func siteAddProxy(dbPath, domain, target string, allowExternal bool) error {
	norm, err := store.ValidateProxyTarget(target, allowExternal)
	if err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	_, err = st.CreateSite(store.Site{Domain: domain, Type: "proxy", ProxyTarget: norm})
	return err
}

// siteAddRedirect membuat site tipe "redirect": host ini mengalihkan (302) ke
// target (URL absolut, mempertahankan path/query). Mis. domain deployment apex →
// domain panel utama. Tak ada koneksi server-side jadi tak perlu cek SSRF.
func siteAddRedirect(dbPath, domain, target string) error {
	target = strings.TrimSpace(target)
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		return fmt.Errorf("target redirect harus URL absolut (https://...)")
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	_, err = st.CreateSite(store.Site{Domain: domain, Type: "redirect", ProxyTarget: target})
	return err
}

type editOpts struct {
	target  string // set → jadi proxy dengan target ini
	root    string // set → jadi static dengan root ini
	disable bool
	enable  bool
	allowExternal bool
}

// siteEdit mengubah target/root/status sebuah site yang sudah ada.
func siteEdit(dbPath, domain string, o editOpts) error {
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if _, err := st.GetSiteByDomain(domain); err != nil {
		return err
	}
	switch {
	case o.target != "":
		norm, err := store.ValidateProxyTarget(o.target, o.allowExternal)
		if err != nil {
			return err
		}
		return st.SetSiteProxy(domain, norm)
	case o.root != "":
		info, err := os.Stat(o.root)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("root %q tidak ada atau bukan direktori", o.root)
		}
		return st.SetSiteStatic(domain, o.root)
	case o.disable:
		return st.SetSiteStatus(domain, "disabled")
	case o.enable:
		return st.SetSiteStatus(domain, "active")
	default:
		return fmt.Errorf("tidak ada perubahan (pakai --target/--root/--disable/--enable)")
	}
}
