package store

import (
	"database/sql"
	"fmt"
	"strings"
)

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Role         string // superadmin|team_manager|member
	CreatedAt    string
	Disabled     bool
	Name         string // R3: nama tampilan
	Email        string // R3: email
	Theme        string // R3: preferensi tema panel ('' = default editorial)
	Locale       string // R3: preferensi bahasa ('' = default en)
	TokenVersion int64  // T4: versi token untuk revocation (bump saat ganti/reset sandi atau disable)
	EmailVerified bool  // hosted: pendaftaran mandiri wajib verifikasi email dulu (admin-create/lama = true)
	Approval     string // hosted: 'approved' (lama/admin) | 'pending' (nunggu admin) | 'rejected'
}

// userCols adalah daftar kolom baku untuk SELECT user (urutan == scanUser).
const userCols = `id, username, password_hash, role, created_at, disabled, name, email, theme, locale, token_version, email_verified, approval`

type Quota struct {
	UserID         int64
	MaxSites       int
	MaxStorageMB   int
	MaxBandwidthGB int
	MaxTeams       int
	MaxMemoryMB    int // RAM per app (0 = default)
	MaxCPUPercent  int // CPU per app, % dari 1 core (0 = default)
	MaxApps        int // jumlah app dimiliki (0 = default)
}

type APIKey struct {
	ID         int64
	UserID     int64
	Name       string
	KeyHash    string
	LastUsedAt string
}

// ---- users ----

// CanCreateTeams: setiap user terautentikasi (role apa pun) boleh membuat tim
// sendiri — pembuatnya otomatis jadi owner. Bikin AKUN tetap admin-only; ini cuma
// soal bikin TIM. Guard role kosong sebagai pengaman token cacat.
func (s *Store) CanCreateTeams(role string) bool {
	return role != ""
}

func (s *Store) SetUserRole(id int64, role string) error {
	res, err := s.db.Exec(`UPDATE users SET role=? WHERE id=?`, role, id)
	return affectedID(res, err, id)
}

// SetEmailVerified menandai status verifikasi email user (hosted registration).
func (s *Store) SetEmailVerified(id int64, verified bool) error {
	v := 0
	if verified {
		v = 1
	}
	res, err := s.db.Exec(`UPDATE users SET email_verified=? WHERE id=?`, v, id)
	return affectedID(res, err, id)
}

// CreateUser membuat user siap-pakai (jalur admin): terverifikasi + approved.
func (s *Store) CreateUser(u User) (int64, error) { return s.createUser(u, true, "approved") }

// CreateUserPending membuat user PENDING (pendaftaran mandiri): email dianggap
// terverifikasi (github yang verifikasi) tapi approval='pending' → login diblok
// sampai admin approve. Status diset ATOMIK di INSERT (secure-by-construction).
func (s *Store) CreateUserPending(u User) (int64, error) { return s.createUser(u, true, "pending") }

func (s *Store) createUser(u User, verified bool, approval string) (int64, error) {
	if u.Role == "" {
		u.Role = "member"
	}
	// Normalisasi username: lowercase + trim agar identitas konsisten.
	u.Username = strings.ToLower(strings.TrimSpace(u.Username))
	v := 0
	if verified {
		v = 1
	}
	// email_verified & approval diset EKSPLISIT di INSERT (secure-by-construction)
	// — tak bergantung DEFAULT kolom maupun UPDATE pasca-insert (cegah fail-open).
	id, err := s.db.insertID(`INSERT INTO users(username, password_hash, role, name, email, email_verified, approval) VALUES(?,?,?,?,?,?,?)`,
		u.Username, u.PasswordHash, u.Role, u.Name, u.Email, v, approval)
	if err != nil {
		if isUniqueErr(err) {
			return 0, fmt.Errorf("username %s sudah dipakai", u.Username)
		}
		return 0, err
	}
	return id, nil
}

// SetApproval menyetel status persetujuan user: 'approved'|'pending'|'rejected'.
// Approve juga membatalkan token lama agar status baru langsung berlaku.
func (s *Store) SetApproval(id int64, status string) error {
	res, err := s.db.Exec(`UPDATE users SET approval=? WHERE id=?`, status, id)
	return affectedID(res, err, id)
}

// GetUserIDByGitHubID memetakan ID numerik GitHub (immutable) → user id lokal.
// Identitas login-with-github HANYA lewat ini (bukan username/email) — cegah
// account takeover via rename akun GitHub.
func (s *Store) GetUserIDByGitHubID(ghID int64) (int64, bool) {
	if ghID == 0 {
		return 0, false
	}
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM users WHERE github_id=?`, ghID).Scan(&id); err != nil {
		return 0, false
	}
	return id, true
}

// SetGitHubID menautkan user lokal ke ID GitHub (dipanggil saat signup github).
func (s *Store) SetGitHubID(userID, ghID int64) error {
	res, err := s.db.Exec(`UPDATE users SET github_id=? WHERE id=?`, ghID, userID)
	return affectedID(res, err, userID)
}

// GetUserIDByGoogleID memetakan Google `sub` (immutable) → user id lokal.
// Identitas login-with-google HANYA lewat ini (bukan email — email bisa ganti).
func (s *Store) GetUserIDByGoogleID(sub string) (int64, bool) {
	if sub == "" {
		return 0, false
	}
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM users WHERE google_id=?`, sub).Scan(&id); err != nil {
		return 0, false
	}
	return id, true
}

// SetGoogleID menautkan user lokal ke Google `sub` (signup/connect google).
func (s *Store) SetGoogleID(userID int64, sub string) error {
	res, err := s.db.Exec(`UPDATE users SET google_id=? WHERE id=?`, sub, userID)
	return affectedID(res, err, userID)
}

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var disabled, emailVerified int
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt, &disabled,
		&u.Name, &u.Email, &u.Theme, &u.Locale, &u.TokenVersion, &emailVerified, &u.Approval)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Disabled = disabled != 0
	u.EmailVerified = emailVerified != 0
	return &u, nil
}

func (s *Store) GetUserByUsername(username string) (*User, error) {
	// Normalisasi argumen agar pencarian konsisten dengan CreateUser.
	username = strings.ToLower(strings.TrimSpace(username))
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE username = ?`, username))
}

func (s *Store) GetUserByID(id int64) (*User, error) {
	return scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

// UserLite ringkas untuk hasil pencarian invite (tanpa data sensitif).
type UserLite struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

// SearchUserCandidates mengambil kandidat user (approved & aktif) untuk pencarian
// invite ala GitHub: cocok substring username/name ATAU prefix 2 huruf pertama
// query (agar typo di ekor tetap terjaring). Ranking presisi (exact/prefix/
// levenshtein) dilakukan di lapisan API. Batasi `limit` kandidat.
func (s *Store) SearchUserCandidates(q string, limit int) ([]UserLite, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	like := "%" + q + "%"
	prefix := q + "%"
	if len(q) >= 2 {
		prefix = q[:2] + "%"
	}
	rows, err := s.db.Query(
		`SELECT username, name FROM users
		 WHERE disabled=0 AND approval='approved'
		   AND (LOWER(username) LIKE ? OR LOWER(name) LIKE ? OR LOWER(username) LIKE ?)
		 LIMIT ?`, like, like, prefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserLite
	for rows.Next() {
		var u UserLite
		if err := rows.Scan(&u.Username, &u.Name); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var disabled, emailVerified int
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.CreatedAt, &disabled,
			&u.Name, &u.Email, &u.Theme, &u.Locale, &u.TokenVersion, &emailVerified, &u.Approval); err != nil {
			return nil, err
		}
		u.Disabled = disabled != 0
		u.EmailVerified = emailVerified != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountUsers dipakai guard setup wizard (hanya jalan saat 0 user).
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// UpdateUserProfile menyetel nama & email (R3 edit akun).
func (s *Store) UpdateUserProfile(id int64, name, email string) error {
	res, err := s.db.Exec(`UPDATE users SET name=?, email=? WHERE id=?`, name, email, id)
	return affectedID(res, err, id)
}

// SetUserPrefs menyetel preferensi tema & locale (R3). Nilai kosong = default.
func (s *Store) SetUserPrefs(id int64, theme, locale string) error {
	res, err := s.db.Exec(`UPDATE users SET theme=?, locale=? WHERE id=?`, theme, locale, id)
	return affectedID(res, err, id)
}

func (s *Store) SetUserPassword(id int64, passwordHash string) error {
	res, err := s.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, passwordHash, id)
	return affectedID(res, err, id)
}

func (s *Store) SetUserDisabled(id int64, disabled bool) error {
	d := 0
	if disabled {
		d = 1
	}
	res, err := s.db.Exec(`UPDATE users SET disabled=? WHERE id=?`, d, id)
	return affectedID(res, err, id)
}

// BumpTokenVersion menaikkan token_version satu user sehingga semua token JWT
// lama (yang menyimpan ver < token_version) segera tidak valid.
// Panggil ini saat: ganti sandi, reset sandi, atau user di-disable.
func (s *Store) BumpTokenVersion(userID int64) error {
	res, err := s.db.Exec(`UPDATE users SET token_version=token_version+1 WHERE id=?`, userID)
	return affectedID(res, err, userID)
}

// ---- quota ----

func (s *Store) SetQuota(q Quota) error {
	_, err := s.db.Exec(
		`INSERT INTO quotas(user_id, max_sites, max_storage_mb, max_bandwidth_gb, max_teams, max_memory_mb, max_cpu_percent, max_apps)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   max_sites=excluded.max_sites, max_storage_mb=excluded.max_storage_mb,
		   max_bandwidth_gb=excluded.max_bandwidth_gb, max_teams=excluded.max_teams,
		   max_memory_mb=excluded.max_memory_mb, max_cpu_percent=excluded.max_cpu_percent,
		   max_apps=excluded.max_apps`,
		q.UserID, q.MaxSites, q.MaxStorageMB, q.MaxBandwidthGB, q.MaxTeams,
		q.MaxMemoryMB, q.MaxCPUPercent, q.MaxApps)
	return err
}

func (s *Store) GetQuota(userID int64) (*Quota, error) {
	row := s.db.QueryRow(`SELECT user_id, max_sites, max_storage_mb, max_bandwidth_gb, max_teams,
		max_memory_mb, max_cpu_percent, max_apps FROM quotas WHERE user_id = ?`, userID)
	var q Quota
	err := row.Scan(&q.UserID, &q.MaxSites, &q.MaxStorageMB, &q.MaxBandwidthGB, &q.MaxTeams,
		&q.MaxMemoryMB, &q.MaxCPUPercent, &q.MaxApps)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// CountTeamsOwnedByUser menghitung tim yang DIMILIKI user (role owner) — dasar
// enforcement kuota tim. Keanggotaan sebagai member/admin di tim orang lain tak dihitung.
func (s *Store) CountTeamsOwnedByUser(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM team_members WHERE user_id=? AND role='owner'`, userID).Scan(&n)
	return n, err
}

// ---- api keys ----

func (s *Store) CreateAPIKey(userID int64, name, keyHash string) (int64, error) {
	return s.db.insertID(`INSERT INTO api_keys(user_id, name, key_hash) VALUES(?,?,?)`, userID, name, keyHash)
}

func (s *Store) GetAPIKeyByHash(keyHash string) (*APIKey, error) {
	row := s.db.QueryRow(`SELECT id, user_id, name, key_hash, last_used_at FROM api_keys WHERE key_hash = ?`, keyHash)
	var k APIKey
	err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.LastUsedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func (s *Store) ListAPIKeys(userID int64) ([]APIKey, error) {
	rows, err := s.db.Query(`SELECT id, user_id, name, key_hash, last_used_at FROM api_keys WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIKey(id, userID int64) error {
	res, err := s.db.Exec(`DELETE FROM api_keys WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("api key %d tidak ditemukan", id)
	}
	return nil
}

// CountUsersByRole menghitung jumlah user dengan peran tertentu.
func (s *Store) CountUsersByRole(role string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role=?`, role).Scan(&n)
	return n, err
}

// CountOwnedResources menghitung total sites, apps, dan dns_zones milik user.
// Dipakai guard hapus user: jika >0, pindahkan atau hapus resource dulu.
func (s *Store) CountOwnedResources(userID int64) (int, error) {
	query := `
		SELECT (
			SELECT COUNT(*) FROM sites
			WHERE (owner_type='user' AND owner_id=?) OR user_id=?
		) + (
			SELECT COUNT(*) FROM apps
			WHERE (owner_type='user' AND owner_id=?) OR user_id=?
		) + (
			SELECT COUNT(*) FROM dns_zones
			WHERE owner_id=? AND owner_type='user'
		)`
	var n int
	err := s.db.QueryRow(query, userID, userID, userID, userID, userID).Scan(&n)
	return n, err
}

// DeleteUser menghapus user beserta seluruh data afiliasinya dalam satu transaksi.
// Guard resource (CountOwnedResources) harus dilakukan sebelum memanggil ini.
func (s *Store) DeleteUser(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	tables := []string{"team_members", "quotas", "api_keys", "connections", "mfa_recovery_codes"}
	for _, tbl := range tables {
		if _, err := tx.Exec(`DELETE FROM `+tbl+` WHERE user_id=?`, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	res, err := tx.Exec(`DELETE FROM users WHERE id=?`, id)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return fmt.Errorf("user %d tidak ditemukan", id)
	}
	return tx.Commit()
}

// ---- sites scoping (untuk isolasi tenant & kuota) ----

func (s *Store) CountUserSites(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sites WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

func (s *Store) CountUserApps(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM apps WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

func (s *Store) ListSitesByUser(userID int64) ([]Site, error) {
	return s.scanSites(`SELECT `+siteCols+` FROM sites WHERE user_id = ? ORDER BY domain`, userID)
}

func affectedID(res sql.Result, err error, id int64) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("id %d tidak ditemukan", id)
	}
	return nil
}
