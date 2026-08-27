// Package store adalah sumber kebenaran konfigurasi lamund (SQLite).
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct{ db *dbConn }

type Site struct {
	ID          int64
	Domain      string
	Type        string // static|proxy
	ProxyTarget string
	RootPath    string
	Status      string // active|disabled|pending_dns|error
	CreatedAt   string
	UserID      int64  // pembuat (0 = admin/warisan pra-F4)
	Config      string // dokumen config JSON (kosong = sintesis dari type/root/target)
	OwnerType   string // R4: 'user' | 'team'
	OwnerID     int64  // R4: id pemilik (user.id atau team.id)
	RepoURL   string // git-static: URL repo sumber (https://github.com/...)
	Branch    string // git-static: nama branch (default "main")
	BuildCmd  string // git-static: perintah build kustom (kosong = auto-detect)
	OutputDir string // git-static: subfolder output build (kosong = "dist" atau ".")
}

// siteCols urutan baku SELECT situs (== urutan scanSite).
const siteCols = `id, domain, type, proxy_target, root_path, status, created_at, user_id, config, owner_type, owner_id, repo_url, branch, build_cmd, output_dir`

func scanSite(row interface{ Scan(...any) error }) (*Site, error) {
	var st Site
	err := row.Scan(&st.ID, &st.Domain, &st.Type, &st.ProxyTarget, &st.RootPath, &st.Status,
		&st.CreatedAt, &st.UserID, &st.Config, &st.OwnerType, &st.OwnerID,
		&st.RepoURL, &st.Branch, &st.BuildCmd, &st.OutputDir)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *Store) scanSites(q string, args ...any) ([]Site, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Site
	for rows.Next() {
		var st Site
		if err := rows.Scan(&st.ID, &st.Domain, &st.Type, &st.ProxyTarget, &st.RootPath, &st.Status,
			&st.CreatedAt, &st.UserID, &st.Config, &st.OwnerType, &st.OwnerID,
			&st.RepoURL, &st.Branch, &st.BuildCmd, &st.OutputDir); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Open membuka database SQLite pada path dsn (dibuat bila belum ada) lalu
// menjalankan migrasi. SQLite = satu-satunya backend (single-binary, zero-config).
func Open(dsn string) (*Store, error) {
	c, err := openDB(dsn)
	if err != nil {
		return nil, err
	}
	// Pastikan dir ada sebelum migrasi (sql.Open lazy).
	if err := os.MkdirAll(filepath.Dir(dsn), 0o700); err != nil {
		c.Close()
		return nil, err
	}
	if err := migrate(c.sx.DB); err != nil {
		c.Close()
		return nil, fmt.Errorf("migrasi skema: %w", err)
	}
	// Kunci file DB ke owner-only setelah migrasi sukses. Abaikan error
	// bila filesystem tidak mendukung (mis. FAT). Termasuk sidecar WAL/SHM yang
	// memuat page terbaru (hash sandi/token) sampai checkpoint.
	_ = os.Chmod(dsn, 0o600)
	_ = os.Chmod(dsn+"-wal", 0o600)
	_ = os.Chmod(dsn+"-shm", 0o600)
	return &Store{db: c}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Ping memverifikasi database dapat diakses (readiness /healthz).
func (s *Store) Ping() error { return s.db.Ping() }

// Counts mengembalikan jumlah situs, user, dan app (untuk /metrics). Query
// COUNT(*) murah; tak menyentuh log (beda dari analitik yang di-agregasi).
func (s *Store) Counts() (sites, users, apps int, err error) {
	scan := func(q string) int {
		if err != nil {
			return 0
		}
		var n int
		err = s.db.QueryRow(q).Scan(&n)
		return n
	}
	sites = scan(`SELECT COUNT(*) FROM sites`)
	users = scan(`SELECT COUNT(*) FROM users`)
	apps = scan(`SELECT COUNT(*) FROM apps`)
	return sites, users, apps, err
}

func (s *Store) CreateSite(site Site) (int64, error) {
	if err := ValidateDomain(site.Domain); err != nil {
		return 0, err
	}
	// Uniqueness lintas-tabel: domain utama tak boleh bentrok dengan alias situs
	// lain (tabel site_domains) — bukan hanya dengan primary lain (dijaga UNIQUE
	// pada sites.domain). Tanpa cek ini, urutan "alias dulu, primary belakangan"
	// bisa lolos dan menimbulkan tabrakan routing (last-Upsert-wins).
	var dup int
	if err := s.db.QueryRow(`SELECT 1 FROM site_domains WHERE domain=?`, site.Domain).Scan(&dup); err == nil {
		return 0, fmt.Errorf("domain %s sudah terdaftar", site.Domain)
	} else if err != sql.ErrNoRows {
		return 0, err
	}
	if site.Status == "" {
		site.Status = "active"
	}
	// Default owner = pembuat (personal) bila belum diset eksplisit.
	if site.OwnerType == "" {
		site.OwnerType = "user"
		site.OwnerID = site.UserID
	}
	id, err := s.db.insertID(
		`INSERT INTO sites(domain, type, proxy_target, root_path, status, user_id, config, owner_type, owner_id, repo_url, branch, build_cmd, output_dir)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		site.Domain, site.Type, site.ProxyTarget, site.RootPath, site.Status, site.UserID, site.Config,
		site.OwnerType, site.OwnerID, site.RepoURL, site.Branch, site.BuildCmd, site.OutputDir)
	if err != nil {
		if isUniqueErr(err) {
			return 0, fmt.Errorf("domain %s sudah terdaftar", site.Domain)
		}
		return 0, err
	}
	return id, nil
}

func (s *Store) GetSiteByDomain(domain string) (*Site, error) {
	return scanSite(s.db.QueryRow(`SELECT `+siteCols+` FROM sites WHERE domain = ?`, domain))
}

// GetSiteWebhookSecret mengembalikan secret webhook auto-deploy situs (kosong =
// belum di-set). Query terpisah agar siteCols/scanSite tak berubah.
func (s *Store) GetSiteWebhookSecret(domain string) (string, error) {
	var secret string
	err := s.db.QueryRow(`SELECT webhook_secret FROM sites WHERE domain=?`, domain).Scan(&secret)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return secret, err
}

// SetSiteWebhookSecret menyetel secret webhook auto-deploy situs.
func (s *Store) SetSiteWebhookSecret(domain, secret string) error {
	_, err := s.db.Exec(`UPDATE sites SET webhook_secret=? WHERE domain=?`, secret, domain)
	return err
}

// Deploy adalah satu baris riwayat deploy git (tabel deploys).
type Deploy struct {
	ID         int64
	Domain     string
	Status     string // building|success|failed
	Trigger    string // manual|webhook|create
	Commit     string // short SHA (kosong bila fetch belum jalan)
	Message    string // subjek commit
	StartedAt  string
	FinishedAt string // kosong = masih building
}

// CreateDeploy menyisipkan baris deploy baru berstatus "building" dan
// mengembalikan id-nya (dipakai FinishDeploy saat selesai).
func (s *Store) CreateDeploy(domain, trigger string) (int64, error) {
	return s.db.insertID(
		`INSERT INTO deploys(domain, trigger, status) VALUES(?,?,'building')`,
		domain, trigger)
}

// FinishDeploy menyetel status akhir (success|failed) beserta commit/message dan
// stempel finished_at pada deploy id.
func (s *Store) FinishDeploy(id int64, status, commit, message string) error {
	_, err := s.db.Exec(
		`UPDATE deploys SET status=?, commit_sha=?, message=?, finished_at=`+s.db.nowExpr()+` WHERE id=?`,
		status, commit, message, id)
	return err
}

// ListDeploys mengembalikan hingga limit deploy terbaru untuk domain (id DESC).
func (s *Store) ListDeploys(domain string, limit int) ([]Deploy, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, domain, status, trigger, commit_sha, message, started_at, finished_at
		 FROM deploys WHERE domain=? ORDER BY id DESC LIMIT ?`, domain, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Deploy
	for rows.Next() {
		var d Deploy
		if err := rows.Scan(&d.ID, &d.Domain, &d.Status, &d.Trigger,
			&d.Commit, &d.Message, &d.StartedAt, &d.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) ListSites() ([]Site, error) {
	return s.scanSites(`SELECT ` + siteCols + ` FROM sites ORDER BY domain`)
}

// SetSiteConfig menyimpan dokumen config (routing multi-path) untuk situs.
func (s *Store) SetSiteConfig(domain, config string) error {
	res, err := s.db.Exec(`UPDATE sites SET config=? WHERE domain=?`, config, domain)
	return affected(res, err, domain)
}

func (s *Store) DeleteSite(domain string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	// Hapus alias terlebih dahulu (FK cascade tidak aktif).
	if _, err := tx.Exec(
		`DELETE FROM site_domains WHERE site_id = (SELECT id FROM sites WHERE domain = ?)`,
		domain,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	res, err := tx.Exec(`DELETE FROM sites WHERE domain = ?`, domain)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("domain %s tidak ditemukan", domain)
	}
	return tx.Commit()
}

// SetSiteProxy mengubah site menjadi tipe proxy dengan target baru.
func (s *Store) SetSiteProxy(domain, target string) error {
	res, err := s.db.Exec(`UPDATE sites SET type='proxy', proxy_target=?, root_path='' WHERE domain=?`, target, domain)
	return affected(res, err, domain)
}

// SetSiteStatic mengubah site menjadi tipe static dengan root baru.
func (s *Store) SetSiteStatic(domain, root string) error {
	res, err := s.db.Exec(`UPDATE sites SET type='static', root_path=?, proxy_target='' WHERE domain=?`, root, domain)
	return affected(res, err, domain)
}

// SetSiteStatus mengubah status site (active|disabled|...).
func (s *Store) SetSiteStatus(domain, status string) error {
	res, err := s.db.Exec(`UPDATE sites SET status=? WHERE domain=?`, status, domain)
	return affected(res, err, domain)
}

// SetSiteRoot mengubah root_path situs (dipakai setelah deploy git selesai).
func (s *Store) SetSiteRoot(domain, root string) error {
	res, err := s.db.Exec(`UPDATE sites SET root_path=? WHERE domain=?`, root, domain)
	return affected(res, err, domain)
}

// SetSiteGit menyetel (atau mengosongkan) field git sebuah situs. Semua kosong =
// putuskan koneksi git (situs kembali dikelola manual/zip).
func (s *Store) SetSiteGit(domain, repoURL, branch, buildCmd, outputDir string) error {
	res, err := s.db.Exec(
		`UPDATE sites SET repo_url=?, branch=?, build_cmd=?, output_dir=? WHERE domain=?`,
		repoURL, branch, buildCmd, outputDir, domain)
	return affected(res, err, domain)
}

func affected(res sql.Result, err error, domain string) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("domain %s tidak ditemukan", domain)
	}
	return nil
}
