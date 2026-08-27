package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// migrations di-apply berurutan; versi tersimpan di user_version SQLite.
// F2+ MENAMBAH entri baru di slice ini — jangan pernah mengubah yang lama.
var migrations = []string{
	`CREATE TABLE sites (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		domain       TEXT NOT NULL UNIQUE,
		type         TEXT NOT NULL CHECK(type IN ('static','proxy','redirect')),
		proxy_target TEXT NOT NULL DEFAULT '',
		root_path    TEXT NOT NULL DEFAULT '',
		status       TEXT NOT NULL DEFAULT 'active',
		created_at   TEXT NOT NULL DEFAULT (datetime('now'))
	);`,
	// F3: sertifikat per domain (cerminan status untuk panel/status endpoint;
	// certmagic yang menyimpan cert asli & memperpanjang).
	`CREATE TABLE certificates (
		domain             TEXT PRIMARY KEY,
		issuer             TEXT NOT NULL DEFAULT '',
		not_after          TEXT NOT NULL DEFAULT '',
		status             TEXT NOT NULL DEFAULT '',
		last_renew_attempt TEXT NOT NULL DEFAULT ''
	);`,
	// F4: multi-user, kuota, API key, usage. sites.user_id = pemilik (0 = admin/warisan).
	`CREATE TABLE users (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		username      TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role          TEXT NOT NULL DEFAULT 'user' CHECK(role IN ('admin','user')),
		created_at    TEXT NOT NULL DEFAULT (datetime('now')),
		disabled      INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE quotas (
		user_id          INTEGER PRIMARY KEY,
		max_sites        INTEGER NOT NULL DEFAULT 0,
		max_storage_mb   INTEGER NOT NULL DEFAULT 0,
		max_bandwidth_gb INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE api_keys (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id      INTEGER NOT NULL,
		name         TEXT NOT NULL DEFAULT '',
		key_hash     TEXT NOT NULL UNIQUE,
		last_used_at TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE usage_monthly (
		user_id         INTEGER NOT NULL,
		yyyymm          TEXT NOT NULL,
		bandwidth_bytes INTEGER NOT NULL DEFAULT 0,
		storage_bytes   INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(user_id, yyyymm)
	);
	ALTER TABLE sites ADD COLUMN user_id INTEGER NOT NULL DEFAULT 0;`,
	// S2: dokumen config per-situs (JSON) untuk routing multi-path. Kosong =
	// sintesis satu route dari type/root/target (situs lama jalan tanpa ubah).
	`ALTER TABLE sites ADD COLUMN config TEXT NOT NULL DEFAULT '';`,
	// run-app: aplikasi terkelola (proses + port). Tiap app juga punya satu
	// site (type proxy) yang meneruskan ke port-nya — routing/HTTPS reuse.
	`CREATE TABLE apps (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		domain     TEXT NOT NULL UNIQUE,
		user_id    INTEGER NOT NULL,
		command    TEXT NOT NULL,
		workdir    TEXT NOT NULL DEFAULT '',
		env        TEXT NOT NULL DEFAULT '',
		port       INTEGER NOT NULL,
		autostart  INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);`,
	// S4: deploy dari Git. repo_url/branch = sumber; webhook_secret utk verifikasi
	// HMAC push GitHub.
	`ALTER TABLE apps ADD COLUMN repo_url TEXT NOT NULL DEFAULT '';
	 ALTER TABLE apps ADD COLUMN branch TEXT NOT NULL DEFAULT '';
	 ALTER TABLE apps ADD COLUMN webhook_secret TEXT NOT NULL DEFAULT '';`,
	// R3 (Panel v2): profil user (name/email) + preferensi panel (theme/locale)
	// disimpan per-user agar sinkron lintas peramban.
	`ALTER TABLE users ADD COLUMN name TEXT NOT NULL DEFAULT '';
	 ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';
	 ALTER TABLE users ADD COLUMN theme TEXT NOT NULL DEFAULT '';
	 ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT '';`,
	// R4 (Panel v2): identitas tim. Resource (sites/apps) punya owner_type/owner_id
	// (default user = pemilik personal). team.org_id disisakan nullable untuk
	// milestone Org/Department kelak (tak dibangun sekarang).
	`CREATE TABLE teams (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL,
		slug       TEXT NOT NULL UNIQUE,
		org_id     INTEGER,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE team_members (
		team_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		role    TEXT NOT NULL DEFAULT 'member' CHECK(role IN ('owner','admin','member')),
		PRIMARY KEY(team_id, user_id)
	);
	ALTER TABLE sites ADD COLUMN owner_type TEXT NOT NULL DEFAULT 'user';
	ALTER TABLE sites ADD COLUMN owner_id INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE apps ADD COLUMN owner_type TEXT NOT NULL DEFAULT 'user';
	ALTER TABLE apps ADD COLUMN owner_id INTEGER NOT NULL DEFAULT 0;
	UPDATE sites SET owner_id = user_id;
	UPDATE apps SET owner_id = user_id;`,
	// R8 (Panel v2): akun terhubung per-user (GitHub token/PAT sekarang; token AI
	// provider menyusul). token disimpan di DB dir data (0700).
	`CREATE TABLE connections (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id    INTEGER NOT NULL,
		provider   TEXT NOT NULL,
		token      TEXT NOT NULL DEFAULT '',
		meta       TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		UNIQUE(user_id, provider)
	);`,
	// R-Tahap2: perluas enum peran instance jadi superadmin|team_manager|member.
	// SQLite tak bisa ubah CHECK → rebuild tabel users, map admin→superadmin, user→member.
	`CREATE TABLE users_new (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		username      TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role          TEXT NOT NULL DEFAULT 'member' CHECK(role IN ('superadmin','team_manager','member')),
		created_at    TEXT NOT NULL DEFAULT (datetime('now')),
		disabled      INTEGER NOT NULL DEFAULT 0,
		name          TEXT NOT NULL DEFAULT '',
		email         TEXT NOT NULL DEFAULT '',
		theme         TEXT NOT NULL DEFAULT '',
		locale        TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO users_new (id, username, password_hash, role, created_at, disabled, name, email, theme, locale)
		SELECT id, username, password_hash,
			CASE role WHEN 'admin' THEN 'superadmin' WHEN 'user' THEN 'member' ELSE 'member' END,
			created_at, disabled, name, email, theme, locale FROM users;
	DROP TABLE users;
	ALTER TABLE users_new RENAME TO users;`,
	// Tahap2: tabel undangan tim (link invite multi-pakai).
	`CREATE TABLE invites (
		token      TEXT PRIMARY KEY,
		team_id    INTEGER NOT NULL,
		role       TEXT NOT NULL DEFAULT 'member',
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);`,
	// Tahap3: setelan email instance + tabel reset kata sandi.
	`CREATE TABLE email_settings (
		id INTEGER PRIMARY KEY CHECK(id=1),
		backend TEXT NOT NULL DEFAULT 'off',
		host TEXT NOT NULL DEFAULT '', port INTEGER NOT NULL DEFAULT 0,
		username TEXT NOT NULL DEFAULT '', password TEXT NOT NULL DEFAULT '',
		from_addr TEXT NOT NULL DEFAULT '', tls INTEGER NOT NULL DEFAULT 1,
		api_base TEXT NOT NULL DEFAULT '', api_key TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO email_settings(id, backend) VALUES(1, 'off');
	CREATE TABLE password_resets (
		token TEXT PRIMARY KEY, user_id INTEGER NOT NULL, expires_at TEXT NOT NULL
	);`,
	// git-static: kolom sumber Git untuk situs statis/SPA di-deploy dari repositori.
	`ALTER TABLE sites ADD COLUMN repo_url TEXT NOT NULL DEFAULT '';
	 ALTER TABLE sites ADD COLUMN branch TEXT NOT NULL DEFAULT '';
	 ALTER TABLE sites ADD COLUMN build_cmd TEXT NOT NULL DEFAULT '';
	 ALTER TABLE sites ADD COLUMN output_dir TEXT NOT NULL DEFAULT '';`,
	// domain-aliases: satu situs bisa dilayani dari beberapa domain.
	// REFERENCES sites(id) adalah dokumentasi — FK enforcement tidak aktif (pragma
	// foreign_keys off). ON DELETE CASCADE tidak ditulis agar tidak silently no-op.
	`CREATE TABLE site_domains (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id    INTEGER NOT NULL REFERENCES sites(id),
		domain     TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX idx_site_domains_site ON site_domains(site_id);`,
	// dns-tahap1: zona authoritative, record, dan setelan nameserver per-instance.
	// FK tidak aktif — cascade harus eksplisit dalam transaksi.
	`CREATE TABLE dns_zones (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL UNIQUE,
		owner_type TEXT NOT NULL DEFAULT 'user',
		owner_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL,
		serial INTEGER NOT NULL DEFAULT 1,
		refresh INTEGER NOT NULL DEFAULT 7200,
		retry INTEGER NOT NULL DEFAULT 3600,
		expire INTEGER NOT NULL DEFAULT 1209600,
		minimum INTEGER NOT NULL DEFAULT 3600,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE TABLE dns_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		zone_id INTEGER NOT NULL REFERENCES dns_zones(id),
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		value TEXT NOT NULL,
		ttl INTEGER NOT NULL DEFAULT 3600,
		priority INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT (datetime('now'))
	);
	CREATE INDEX idx_dns_records_zone ON dns_records(zone_id);
	CREATE TABLE dns_settings (
		id INTEGER PRIMARY KEY CHECK(id=1),
		ns1 TEXT NOT NULL DEFAULT '',
		ns2 TEXT NOT NULL DEFAULT '',
		hostmaster TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO dns_settings(id) VALUES(1);`,
	// Security hardening T3: invite kadaluarsa + binding email (cegah escalation).
	// expires_at = RFC3339 UTC, '' = tidak kadaluarsa (lama). email = '' = link tanpa binding.
	`ALTER TABLE invites ADD COLUMN expires_at TEXT NOT NULL DEFAULT ''; ALTER TABLE invites ADD COLUMN email TEXT NOT NULL DEFAULT '';`,
	// Security hardening T4: revocation JWT via token_version.
	// Bump token_version saat ganti/reset sandi atau disable user → token lama (ver<token_version) ditolak.
	`ALTER TABLE users ADD COLUMN token_version INTEGER NOT NULL DEFAULT 0;`,
	// Security hardening: normalisasi username (lowercase+trim) agar konsisten
	// dgn lookup yang kini dinormalisasi — cegah lockout user mixed-case lama.
	// Hanya baris yang perlu; bila menimbulkan bentrok UNIQUE, migrasi gagal &
	// perlu resolusi manual (kasus langka dua user beda-casing).
	`UPDATE users SET username = lower(trim(username)) WHERE username <> lower(trim(username));`,
	// MFA TOTP: kolom secret/enabled/anti-replay pada tabel users (query terpisah,
	// scanUser TIDAK diubah) + tabel recovery codes (hashed, single-use).
	// Secret MFA disimpan plaintext at-rest — enkripsi direncanakan batch-2.
	`ALTER TABLE users ADD COLUMN mfa_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN mfa_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN mfa_last_step INTEGER NOT NULL DEFAULT 0;
CREATE TABLE mfa_recovery_codes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  code_hash TEXT NOT NULL,
  used INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_mfa_recovery_user ON mfa_recovery_codes(user_id);`,
	// Webhook auto-deploy untuk situs git-static (paritas dgn apps.webhook_secret):
	// push GitHub → /hooks/git/{domain} (HMAC) → clone/fetch + build + reload.
	`ALTER TABLE sites ADD COLUMN webhook_secret TEXT NOT NULL DEFAULT '';`,
	// Riwayat deploy git (ala Vercel): satu baris per deploy, dari manual/webhook/
	// create. commit_sha/message diisi setelah fetch berhasil; finished_at kosong =
	// masih building. Diindeks per-domain (id DESC) untuk daftar terbaru-dulu.
	`CREATE TABLE deploys (
	  id INTEGER PRIMARY KEY AUTOINCREMENT,
	  domain TEXT NOT NULL,
	  status TEXT NOT NULL DEFAULT 'building',
	  trigger TEXT NOT NULL DEFAULT 'manual',
	  commit_sha TEXT NOT NULL DEFAULT '',
	  message TEXT NOT NULL DEFAULT '',
	  started_at TEXT NOT NULL DEFAULT (datetime('now')),
	  finished_at TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX idx_deploys_domain ON deploys(domain, id DESC);`,
	// Pendaftaran mandiri hosted: user lama/admin-create = terverifikasi (DEFAULT 1);
	// signup mandiri di-set 0 lalu wajib verifikasi email. Token verifikasi sekali-pakai.
	`ALTER TABLE users ADD COLUMN email_verified INTEGER NOT NULL DEFAULT 1;
	CREATE TABLE email_verifications (
	  token TEXT PRIMARY KEY,
	  user_id INTEGER NOT NULL,
	  expires_at TEXT NOT NULL
	);`,
	// Workflow approval hosted: user lama/admin = 'approved' (DEFAULT); pendaftaran
	// mandiri = 'pending' → login diblok sampai admin approve; 'rejected' = ditolak.
	`ALTER TABLE users ADD COLUMN approval TEXT NOT NULL DEFAULT 'approved';`,
	// Login-with-GitHub: identitas dipetakan lewat ID numerik GitHub (immutable),
	// BUKAN username (renameable → account takeover) atau email. 0 = tak tertaut.
	`ALTER TABLE users ADD COLUMN github_id INTEGER NOT NULL DEFAULT 0;`,
	// Kuota tim per-user (0 = pakai default). Admin bisa setel dinamis. Bikin TIM
	// kini terbuka untuk semua user; kuota ini yang membatasi jumlah tim dimiliki.
	`ALTER TABLE quotas ADD COLUMN max_teams INTEGER NOT NULL DEFAULT 0;`,
	// Login-with-Google: identitas dipetakan lewat `sub` immutable (string), bukan
	// email. Akun bisa punya github_id DAN google_id (multi-provider). '' = tak tertaut.
	`ALTER TABLE users ADD COLUMN google_id TEXT NOT NULL DEFAULT '';`,
	// Limit resource per-app (ala Vercel tier): RAM & CPU per app + jumlah app.
	// 0 = pakai default free-tier. Admin bisa naikin per-user ("upgrade Pro").
	`ALTER TABLE quotas ADD COLUMN max_memory_mb INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE quotas ADD COLUMN max_cpu_percent INTEGER NOT NULL DEFAULT 0;
	 ALTER TABLE quotas ADD COLUMN max_apps INTEGER NOT NULL DEFAULT 0;`,
	// Tipe site 'redirect' (apex domain deployment → domain panel utama).
	// SQLite tak bisa ALTER CHECK → rebuild tabel dgn constraint baru.
	`CREATE TABLE sites_new (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		domain       TEXT NOT NULL UNIQUE,
		type         TEXT NOT NULL CHECK(type IN ('static','proxy','redirect')),
		proxy_target TEXT NOT NULL DEFAULT '',
		root_path    TEXT NOT NULL DEFAULT '',
		status       TEXT NOT NULL DEFAULT 'active',
		created_at   TEXT NOT NULL DEFAULT (datetime('now')),
		user_id      INTEGER NOT NULL DEFAULT 0,
		config       TEXT NOT NULL DEFAULT '',
		owner_type   TEXT NOT NULL DEFAULT 'user',
		owner_id     INTEGER NOT NULL DEFAULT 0,
		repo_url     TEXT NOT NULL DEFAULT '',
		branch       TEXT NOT NULL DEFAULT '',
		build_cmd    TEXT NOT NULL DEFAULT '',
		output_dir   TEXT NOT NULL DEFAULT '',
		webhook_secret TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO sites_new SELECT id,domain,type,proxy_target,root_path,status,created_at,user_id,config,owner_type,owner_id,repo_url,branch,build_cmd,output_dir,webhook_secret FROM sites;
	DROP TABLE sites;
	ALTER TABLE sites_new RENAME TO sites;`,
}

// migrate menerapkan migrasi yang tertunda secara ATOMIK & TERSERIALISASI.
// `BEGIN IMMEDIATE` mengambil kunci tulis di awal, jadi bila dua proses
// (data plane + panel) membuka DB belum-termigrasi bersamaan, hanya satu
// yang bermigrasi; yang lain menunggu (busy_timeout) lalu melihat versi baru
// dan melewati — mencegah error "table already exists".
func migrate(db *sql.DB) error {
	ctx := context.Background()
	// Baik akuisisi koneksi (pragma journal_mode(WAL) butuh exclusive-lock
	// sesaat saat connect) MAUPUN `BEGIN IMMEDIATE` bisa balik SQLITE_BUSY
	// seketika bila proses lain sedang bermigrasi. Karena migrasi jarang &
	// pendek, retry-with-backoff aman: yang kalah lomba menunggu, lalu melihat
	// versi baru & melewati (idempoten).
	deadline := time.Now().Add(10 * time.Second)
	for attempt := 0; ; attempt++ {
		err := migrateOnce(ctx, db)
		if err == nil || !isBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Duration(5*(attempt+1)) * time.Millisecond)
	}
}

func migrateOnce(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	if err := applyMigrations(ctx, conn); err != nil {
		_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		return err
	}
	_, err = conn.ExecContext(ctx, `COMMIT`)
	return err
}

func isBusy(err error) bool {
	s := err.Error()
	return strings.Contains(s, "database is locked") || strings.Contains(s, "SQLITE_BUSY")
}

func applyMigrations(ctx context.Context, conn *sql.Conn) error {
	var v int
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		if _, err := conn.ExecContext(ctx, migrations[i]); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, `PRAGMA user_version = `+strconv.Itoa(i+1)); err != nil {
			return err
		}
	}
	return nil
}
