package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

// cmdBackup membuat salinan konsisten (hot backup) database via VACUUM INTO,
// dengan mode 0600. Menggantikan backup manual operator yang tak konsisten
// (temuan audit: sejumlah backup 0644 world-readable). Aman dijalankan saat
// service hidup — VACUUM INTO memakai snapshot read, kompatibel dengan WAL.
func cmdBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	dbPath := fs.String("db", "/var/lib/lamund/lamund.db", "path database SQLite")
	out := fs.String("out", "", "path output (default: <db>.bak-YYYYMMDD-HHMMSS)")
	fs.Parse(args)

	target := *out
	if target == "" {
		target = fmt.Sprintf("%s.bak-%s", *dbPath, time.Now().Format("20060102-150405"))
	}
	if _, err := os.Stat(target); err == nil {
		fatal("output sudah ada: %s", target)
	}
	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		fatal("buka db: %v", err)
	}
	defer db.Close()
	// VACUUM INTO menulis DB utuh & terdefrag ke target (satu file, tanpa WAL).
	if _, err := db.Exec("VACUUM INTO ?", target); err != nil {
		fatal("VACUUM INTO: %v", err)
	}
	// Kunci ke owner-only: backup memuat hash sandi, secret MFA, hash API key.
	if err := os.Chmod(target, 0o600); err != nil {
		fatal("chmod backup: %v", err)
	}
	fmt.Println(target)
}
