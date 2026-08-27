package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// env disimpan sebagai JSON array agar nilai boleh berisi karakter apa pun
// (termasuk newline) tanpa merusak format.
func envMarshal(env []string) string {
	if len(env) == 0 {
		return ""
	}
	b, _ := json.Marshal(env)
	return string(b)
}

func envUnmarshal(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if json.Unmarshal([]byte(s), &out) == nil {
		return out
	}
	// kompat mundur: format lama newline-joined
	return strings.Split(s, "\n")
}

// portBase adalah awal rentang port yang di-assign ke app terkelola.
const portBase = 40000

// App adalah aplikasi terkelola (proses + port). Domain-nya juga punya satu
// site (type proxy) yang meneruskan ke port ini.
type App struct {
	ID            int64
	Domain        string
	UserID        int64
	Command       string
	WorkDir       string
	Env           []string // "K=V"
	Port          int
	Autostart     bool
	CreatedAt     string
	RepoURL       string
	Branch        string
	WebhookSecret string
	OwnerType     string // R4: 'user' | 'team'
	OwnerID       int64  // R4: user.id atau team.id
}

// CreateApp menyimpan app baru; port di-assign otomatis dari rentang bila 0.
func (s *Store) CreateApp(a App) (App, error) {
	if a.Port == 0 {
		var maxPort int
		if err := s.db.QueryRow(`SELECT COALESCE(MAX(port), ?) FROM apps`, portBase-1).Scan(&maxPort); err != nil {
			return a, err
		}
		a.Port = maxPort + 1
	}
	auto := 0
	if a.Autostart {
		auto = 1
	}
	if a.OwnerType == "" {
		a.OwnerType = "user"
		a.OwnerID = a.UserID
	}
	id, err := s.db.insertID(
		`INSERT INTO apps(domain, user_id, command, workdir, env, port, autostart, repo_url, branch, webhook_secret, owner_type, owner_id)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.Domain, a.UserID, a.Command, a.WorkDir, envMarshal(a.Env), a.Port, auto,
		a.RepoURL, a.Branch, a.WebhookSecret, a.OwnerType, a.OwnerID)
	if err != nil {
		if isUniqueErr(err) {
			return a, fmt.Errorf("app %s sudah ada", a.Domain)
		}
		return a, err
	}
	a.ID = id
	return a, nil
}

func scanApp(row interface{ Scan(...any) error }) (*App, error) {
	var a App
	var env string
	var auto int
	err := row.Scan(&a.ID, &a.Domain, &a.UserID, &a.Command, &a.WorkDir, &env, &a.Port, &auto, &a.CreatedAt,
		&a.RepoURL, &a.Branch, &a.WebhookSecret, &a.OwnerType, &a.OwnerID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Autostart = auto != 0
	a.Env = envUnmarshal(env)
	return &a, nil
}

const appCols = `id, domain, user_id, command, workdir, env, port, autostart, created_at, repo_url, branch, webhook_secret, owner_type, owner_id`

func (s *Store) GetAppByDomain(domain string) (*App, error) {
	return scanApp(s.db.QueryRow(`SELECT `+appCols+` FROM apps WHERE domain = ?`, domain))
}

func (s *Store) ListApps() ([]App, error) {
	return s.queryApps(`SELECT ` + appCols + ` FROM apps ORDER BY domain`)
}

func (s *Store) ListAppsByUser(userID int64) ([]App, error) {
	return s.queryApps(`SELECT `+appCols+` FROM apps WHERE user_id = ? ORDER BY domain`, userID)
}

func (s *Store) queryApps(q string, args ...any) ([]App, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []App
	for rows.Next() {
		var a App
		var env string
		var auto int
		if err := rows.Scan(&a.ID, &a.Domain, &a.UserID, &a.Command, &a.WorkDir, &env, &a.Port, &auto, &a.CreatedAt,
			&a.RepoURL, &a.Branch, &a.WebhookSecret, &a.OwnerType, &a.OwnerID); err != nil {
			return nil, err
		}
		a.Autostart = auto != 0
		a.Env = envUnmarshal(env)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteApp(domain string) error {
	res, err := s.db.Exec(`DELETE FROM apps WHERE domain = ?`, domain)
	return affected(res, err, domain)
}

// SetAppEnv mengganti seluruh environment variable app.
func (s *Store) SetAppEnv(domain string, env []string) error {
	res, err := s.db.Exec(`UPDATE apps SET env=? WHERE domain=?`, envMarshal(env), domain)
	return affected(res, err, domain)
}

// SetAppPort mengubah port live app (dipakai saat blue-green swap).
func (s *Store) SetAppPort(domain string, port int) error {
	res, err := s.db.Exec(`UPDATE apps SET port=? WHERE domain=?`, port, domain)
	return affected(res, err, domain)
}
